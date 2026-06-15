// Package consensus : moteur PoS "Aurora" v1 — rotation déterministe du
// proposeur pondérée par le stake, finalité immédiate (pas de fork choice
// en devnet ; les votes BFT multi-validateurs arrivent en Phase 2).
package consensus

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"chaingo/internal/crypto"
	"chaingo/internal/mempool"
	"chaingo/internal/state"
	"chaingo/internal/store"
	"chaingo/internal/types"
)

// La récompense de bloc n'est pas une constante : elle est calculée par
// types.RewardPerBlock à partir du stake total et du taux d'inflation
// annuel défini dans les Params de la chaîne (genèse).

// ErrGap : bloc reçu trop en avance — il faut d'abord synchroniser.
var ErrGap = errors.New("block height gap, sync required")

// MaxRounds : nombre de proposeurs de secours acceptés par hauteur. Si le
// proposeur du round 0 est hors-ligne, le round suivant (un autre tirage
// pondéré par le stake) prend la main après un intervalle de bloc. La
// probabilité que 64 tirages consécutifs tombent tous sur des validateurs
// morts est négligeable dès qu'un validateur honnête est actif.
const MaxRounds = 64

type Engine struct {
	mu        sync.Mutex
	st        *state.State
	pool      *mempool.Mempool
	db        *store.Store // nil = pas de persistance (bench)
	key       *crypto.KeyPair
	addr      string
	interval  time.Duration
	maxTxs    int
	heartbeat int // produit un bloc vide tous les N ticks (liveness)
	tick      int
	// lastProgress : dernier commit local (production ou bloc accepté).
	// Sert d'horloge pour le round de secours courant.
	lastProgress time.Time
	OnBlock      func(*types.Block) // hook broadcast p2p

	// Finalité BFT (Phase 2) : pool de précommits. La hauteur finalisée vit
	// dans l'état (persistée), avancée quand un bloc porte un commit ≥ 2/3.
	votes   *votePool
	chainID string
	OnVote  func(*types.Vote) // hook broadcast p2p des votes
	// voted : hauteur -> kind -> hash déjà signé PAR CE NŒUD. Invariant de
	// sûreté BFT : on ne signe JAMAIS deux votes de même kind à la même
	// hauteur (sinon on déclenche notre propre règle d'équivocation →
	// auto-slash). Tranche 2 ajoutera la règle de verrouillage POL.
	voted map[uint64]map[string]string
	// Slashing : preuves de double-signature en attente d'inclusion dans un
	// bloc (clé voter@height), protégées par e.mu.
	evidence map[string]*types.DoubleSignEvidence
	stopCh   chan struct{}
}

func evidenceKey(voter string, height uint64) string {
	return fmt.Sprintf("%s@%d", voter, height)
}

func New(st *state.State, pool *mempool.Mempool, db *store.Store, key *crypto.KeyPair, interval time.Duration, maxTxs int) *Engine {
	e := &Engine{
		st:           st,
		pool:         pool,
		db:           db,
		key:          key,
		interval:     interval,
		maxTxs:       maxTxs,
		heartbeat:    20,
		lastProgress: time.Now(),
		votes:        newVotePool(),
		voted:        map[uint64]map[string]string{},
		evidence:     map[string]*types.DoubleSignEvidence{},
		stopCh:       make(chan struct{}),
	}
	if key != nil {
		e.addr = key.Address()
	}
	return e
}

// SetChainID renseigne le chain_id utilisé pour signer/vérifier les votes.
func (e *Engine) SetChainID(id string) { e.chainID = id }

// FinalizedHeight : dernière hauteur finalisée (depuis l'état, persistée).
func (e *Engine) FinalizedHeight() uint64 { return e.st.GetFinalized() }

func (e *Engine) Start() {
	if e.key == nil {
		log.Println("[consensus] no validator key — running as full node (sync + API only)")
		return
	}
	go func() {
		t := time.NewTicker(e.interval)
		defer t.Stop()
		for {
			select {
			case <-e.stopCh:
				return
			case <-t.C:
				e.tick++
				force := e.tick%e.heartbeat == 0
				if b := e.ProduceOnce(force); b != nil {
					log.Printf("[consensus] block #%d produced: %d tx(s), hash %s…", b.Header.Height, len(b.Txs), b.Hash[:12])
				}
			}
		}
	}()
}

func (e *Engine) Stop() { close(e.stopCh) }

// ProduceOnce builds, signs, applies and persists one block if this node
// is the elected proposer for the next height. force=true produces even
// an empty block (heartbeat).
func (e *Engine) ProduceOnce(force bool) *types.Block {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.key == nil {
		return nil
	}
	height := e.st.GetHeight() + 1
	prev := e.st.GetLastHash()
	// Round courant : un round de secours s'ouvre à chaque intervalle
	// écoulé sans bloc. On ne produit que si on est LE proposeur du round
	// en cours (pas d'un round passé ni futur).
	round := uint32(time.Since(e.lastProgress) / e.interval)
	if round >= MaxRounds {
		round = MaxRounds - 1
	}
	if e.st.SelectProposer(height, prev, round) != e.addr {
		return nil
	}
	txs := e.pool.Take(e.maxTxs, e.st.NonceOf)
	evid := e.drainEvidenceLocked()
	if len(txs) == 0 && len(evid) == 0 && !force {
		return nil
	}
	// Le timestamp est fixé AVANT l'exécution : il fait partie des règles
	// (déblocage des unbondings) et doit être identique chez les suiveurs.
	blockTime := time.Now().UnixMilli()
	// Commit du bloc parent : les précommits ≥ 2/3 connus pour (height-1, prev).
	lastCommit := e.buildLastCommit(height-1, prev)
	absent := e.absentProposers(height, prev, round, e.addr)
	applied, failed, root, _ := e.st.Execute(txs, evid, absent, e.addr, blockTime, false)
	for h, err := range failed {
		log.Printf("[consensus] tx %s… rejected: %v", h[:12], err)
		e.pool.Drop(h)
	}
	for _, tx := range applied {
		e.pool.Drop(tx.Hash())
	}
	if len(evid) > 0 {
		log.Printf("[consensus] bloc #%d inclut %d preuve(s) de double-signature (slash appliqué)", height, len(evid))
	}
	b := &types.Block{
		Header: types.BlockHeader{
			Height:         height,
			PrevHash:       prev,
			Timestamp:      blockTime,
			Proposer:       e.addr,
			Round:          round,
			TxRoot:         types.TxRoot(applied),
			EvidenceRoot:   types.EvidenceRoot(evid),
			LastCommitRoot: types.CommitRoot(lastCommit),
			StateRoot:      root,
		},
		Txs:        applied,
		Evidence:   evid,
		LastCommit: lastCommit,
	}
	b.Hash = b.ComputeHash()
	b.ProposerPubKey = e.key.PubBytes()
	b.ProposerSignature = e.key.Sign(b.SigningBytes())
	e.st.Commit(height, b.Hash)
	if len(lastCommit) > 0 {
		e.st.SetFinalized(height - 1)
		e.votes.prune(height - 1)
		e.pruneVotedLocked(height - 1)
	}
	e.lastProgress = time.Now()
	e.persist(b)
	if e.OnBlock != nil {
		e.OnBlock(b)
	}
	e.castVote(height, b.Hash)
	return b
}

// ApplyExternalBlock validates and applies a block received from a peer.
func (e *Engine) ApplyExternalBlock(b *types.Block) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	cur := e.st.GetHeight()
	switch {
	case b.Header.Height <= cur:
		return nil // déjà connu
	case b.Header.Height > cur+1:
		return ErrGap
	}
	prev := e.st.GetLastHash()
	if b.Header.PrevHash != prev {
		return fmt.Errorf("prev hash mismatch at height %d", b.Header.Height)
	}
	// Le proposeur doit être EXACTEMENT l'élu du round inscrit dans l'en-tête
	// (round 0 nominal, sinon un round de secours). Le round explicite lève
	// toute ambiguïté et rend le comptage d'inactivité déterministe.
	if b.Header.Round >= MaxRounds {
		return fmt.Errorf("round %d out of range", b.Header.Round)
	}
	if want := e.st.SelectProposer(b.Header.Height, prev, b.Header.Round); want != b.Header.Proposer {
		return fmt.Errorf("wrong proposer at height %d round %d: got %s, want %s", b.Header.Height, b.Header.Round, b.Header.Proposer, want)
	}
	if b.ComputeHash() != b.Hash {
		return errors.New("invalid block hash")
	}
	if err := b.VerifyProposerSig(); err != nil {
		return fmt.Errorf("proposer signature: %w", err)
	}
	if types.TxRoot(b.Txs) != b.Header.TxRoot {
		return errors.New("tx root mismatch")
	}
	if types.EvidenceRoot(b.Evidence) != b.Header.EvidenceRoot {
		return errors.New("evidence root mismatch")
	}
	for _, ev := range b.Evidence {
		if err := ev.Verify(e.chainID); err != nil {
			return fmt.Errorf("invalid double-sign evidence: %w", err)
		}
	}
	// Commit du parent : si présent, il doit représenter ≥ 2/3 du stake actif
	// sur le bloc parent (height-1, prevHash). C'est ce qui finalise.
	if types.CommitRoot(b.LastCommit) != b.Header.LastCommitRoot {
		return errors.New("last commit root mismatch")
	}
	finalizesParent := false
	if len(b.LastCommit) > 0 {
		power, cerr := e.verifyCommit(b.LastCommit, b.Header.Height-1, b.Header.PrevHash)
		if cerr != nil {
			return fmt.Errorf("invalid last commit: %w", cerr)
		}
		if !hasQuorum(power, e.st.TotalPower()) {
			return errors.New("last commit below 2/3 of active stake")
		}
		finalizesParent = true
	}
	if err := types.VerifyAll(b.Txs); err != nil {
		return err
	}
	absent := e.absentProposers(b.Header.Height, prev, b.Header.Round, b.Header.Proposer)
	snapshot := e.st.Bytes()
	_, _, root, err := e.st.Execute(b.Txs, b.Evidence, absent, b.Header.Proposer, b.Header.Timestamp, true)
	if err != nil || root != b.Header.StateRoot {
		e.st.Restore(snapshot)
		if err == nil {
			err = errors.New("state root mismatch")
		}
		return err
	}
	e.st.Commit(b.Header.Height, b.Hash)
	if finalizesParent {
		e.st.SetFinalized(b.Header.Height - 1)
		e.votes.prune(b.Header.Height - 1)
		e.pruneVotedLocked(b.Header.Height - 1)
	}
	e.lastProgress = time.Now()
	e.persist(b)
	for _, tx := range b.Txs {
		e.pool.Drop(tx.Hash())
	}
	for _, ev := range b.Evidence {
		delete(e.evidence, evidenceKey(ev.Voter, ev.Height))
	}
	log.Printf("[consensus] block #%d accepted from peer: %d tx(s), %d preuve(s)", b.Header.Height, len(b.Txs), len(b.Evidence))
	e.castVote(b.Header.Height, b.Hash)
	return nil
}

// ---- finalité BFT (Phase 2) ----

var errWrongVoteChain = errors.New("vote on wrong chain")

// castVote : émet prevote puis précommit pour le bloc qu'on vient de
// committer. Le précommit reste celui qui décide la finalité ; le prevote
// est posé en préparation de la tranche 2 (locking). Suppose e.mu détenu.
func (e *Engine) castVote(height uint64, hash string) {
	e.castVoteKind(height, types.PrevoteKind, hash)
	e.castVoteKind(height, types.PrecommitKind, hash)
}

// castVoteKind : émet un vote d'un kind donné, l'ajoute au pool et le diffuse.
//
// INVARIANT DE SÛRETÉ BFT : un validateur ne signe jamais deux votes du même
// kind à la même hauteur. Sinon il produit lui-même une preuve d'équivocation
// et se fait slasher. Ce garde-fou est aussi le prérequis d'un futur
// fork-choice (la règle de verrouillage POL viendra en tranche 2).
func (e *Engine) castVoteKind(height uint64, kind, hash string) {
	if e.key == nil {
		return
	}
	if e.voted[height] == nil {
		e.voted[height] = map[string]string{}
	}
	if prev, ok := e.voted[height][kind]; ok {
		if prev != hash {
			log.Printf("[consensus] SÛRETÉ : refus de signer un 2e %s (%s…) à la hauteur #%d — déjà voté %s… (anti-auto-équivocation)", kind, short(hash), height, short(prev))
		}
		return
	}
	e.voted[height][kind] = hash
	v := &types.Vote{ChainID: e.chainID, Height: height, Kind: kind, BlockHash: hash}
	v.SignWith(e.key)
	e.votes.add(v)
	if e.OnVote != nil {
		e.OnVote(v)
	}
}

func short(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

// pruneVotedLocked oublie les précommits émis aux hauteurs déjà finalisées.
func (e *Engine) pruneVotedLocked(upTo uint64) {
	for h := range e.voted {
		if h <= upTo {
			delete(e.voted, h)
		}
	}
}

// recordEquivocation : transforme un précommit conflictuel en preuve à
// inclure dans un futur bloc (sauf si la faute est déjà punie ou déjà
// connue). Pour cette tranche, seuls les précommits déclenchent un slash.
func (e *Engine) recordEquivocation(prev, cur *types.Vote) {
	if cur.Kind != types.PrecommitKind {
		return
	}
	key := evidenceKey(cur.Voter, cur.Height)
	if _, exists := e.evidence[key]; exists || e.st.IsSlashed(cur.Voter, cur.Height) {
		return
	}
	e.evidence[key] = &types.DoubleSignEvidence{
		Height: cur.Height, Voter: cur.Voter, VoteA: prev, VoteB: cur,
	}
	log.Printf("[consensus] ÉQUIVOCATION détectée : %s a précommit 2 blocs à la hauteur #%d — preuve en attente de slash", cur.Voter, cur.Height)
}

// AddVote : vote reçu d'un pair (prevote ou précommit). Renvoie true s'il est
// nouveau (à re-diffuser). Valide signature, kind et statut de validateur.
func (e *Engine) AddVote(v *types.Vote) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.chainID != "" && v.ChainID != e.chainID {
		return false, errWrongVoteChain
	}
	if v.Kind != types.PrecommitKind && v.Kind != types.PrevoteKind {
		return false, fmt.Errorf("unknown vote kind %q", v.Kind)
	}
	if v.Height <= e.st.GetFinalized() {
		return false, nil // hauteur déjà finalisée
	}
	if e.st.PowerOf(v.Voter) == 0 {
		return false, errors.New("voter is not an active validator")
	}
	if err := v.Verify(); err != nil {
		return false, err
	}
	isNew, equiv := e.votes.add(v)
	if !isNew {
		return false, nil
	}
	if equiv != nil {
		e.recordEquivocation(equiv, v)
	}
	return true, nil
}

// buildLastCommit : reconstruit le commit du bloc (height, hash) à partir des
// précommits connus, s'il atteint ≥ 2/3 du stake actif ; sinon nil (le bloc
// ne finalisera pas son parent — la finalité rattrapera plus tard).
func (e *Engine) buildLastCommit(height uint64, hash string) []*types.Vote {
	if height == 0 {
		return nil // la genèse n'a pas de précommits
	}
	cand := e.votes.commitVotes(height, hash)
	if len(cand) == 0 {
		return nil
	}
	power, err := e.verifyCommit(cand, height, hash)
	if err != nil || !hasQuorum(power, e.st.TotalPower()) {
		return nil
	}
	return cand
}

// verifyCommit valide un ensemble de précommits sur (height, hash) et renvoie
// le pouvoir actif cumulé des votants distincts. Sert au proposeur (build) et
// aux suiveurs (validation du LastCommit reçu).
func (e *Engine) verifyCommit(votes []*types.Vote, height uint64, hash string) (uint64, error) {
	seen := map[string]bool{}
	var power uint64
	for _, v := range votes {
		if e.chainID != "" && v.ChainID != e.chainID {
			return 0, errors.New("commit vote on wrong chain")
		}
		if v.Kind != types.PrecommitKind {
			return 0, errors.New("commit must contain precommits only")
		}
		if v.Height != height || v.BlockHash != hash {
			return 0, errors.New("commit vote height/hash mismatch")
		}
		if seen[v.Voter] {
			return 0, errors.New("duplicate voter in commit")
		}
		seen[v.Voter] = true
		p := e.st.PowerOf(v.Voter)
		if p == 0 {
			return 0, errors.New("commit voter is not an active validator")
		}
		if err := v.Verify(); err != nil {
			return 0, err
		}
		power += p
	}
	return power, nil
}

// absentProposers : les validateurs élus des rounds de secours sautés
// (0..round-1) qui n'ont donc pas produit ce bloc — déterministe à partir de
// (height, prev, round), identique chez le proposeur et chez les suiveurs.
// On exclut le proposeur effectif (il a produit) et on déduplique.
func (e *Engine) absentProposers(height uint64, prev string, round uint32, proposer string) []string {
	if round == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for r := uint32(0); r < round; r++ {
		a := e.st.SelectProposer(height, prev, r)
		if a == "" || a == proposer || seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out
}

// drainEvidenceLocked : retire et renvoie les preuves en attente (ordre
// trié = déterministe), en écartant celles déjà punies. Suppose e.mu détenu.
func (e *Engine) drainEvidenceLocked() []*types.DoubleSignEvidence {
	if len(e.evidence) == 0 {
		return nil
	}
	keys := make([]string, 0, len(e.evidence))
	for k := range e.evidence {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []*types.DoubleSignEvidence
	for _, k := range keys {
		ev := e.evidence[k]
		delete(e.evidence, k)
		if e.st.IsSlashed(ev.Voter, ev.Height) {
			continue
		}
		out = append(out, ev)
	}
	return out
}

func (e *Engine) persist(b *types.Block) {
	if e.db == nil {
		return
	}
	if err := e.db.SaveBlock(b); err != nil {
		log.Printf("[consensus] save block: %v", err)
	}
	if err := e.db.SaveState(e.st.Bytes()); err != nil {
		log.Printf("[consensus] save state: %v", err)
	}
}
