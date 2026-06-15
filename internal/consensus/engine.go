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
	// setByHeight : set de validateurs FIGÉ qui gouverne les votes de chaque
	// hauteur (#5). setByHeight[H] = pouvoir actif post-(H-1), figé à l'entrée
	// du traitement du bloc H. Le quorum 2/3 d'une hauteur se mesure contre ce
	// set, pas contre l'état vivant — voir docs/design/phase2-validator-set-freeze.md.
	setByHeight map[uint64]*state.ValidatorSet
	// locked : verrou POL par hauteur (#6 tranche 3). Quand ce nœud précommit un
	// bloc, il se VERROUILLE dessus (round + hash). Il ne précommettra un hash
	// différent à cette hauteur QUE sur preuve d'une polka à un round STRICTEMENT
	// supérieur (POL). Garantit qu'on ne change d'avis que sur quorum plus récent
	// — base d'un reorg sûr (fork-choice = #7).
	locked map[uint64]lock
	// snapshots : photo de l'état (bytes) APRÈS application de chaque hauteur,
	// pour la fenêtre NON finalisée [finalized, current] (#7, fondation du reorg).
	// Un reorg = restaurer le snapshot du point de fork (toujours ≥ finalized,
	// immuable) puis rejouer la branche concurrente. Purgé sous `finalized`.
	snapshots map[uint64][]byte
	// forks : blocs CONCURRENTS reçus à une hauteur déjà committée (mais non
	// finalisée), en attente d'une éventuelle bascule (#7). On bascule vers l'un
	// d'eux s'il porte une polka à un round STRICTEMENT supérieur à notre bloc
	// courant (preuve qu'une super-majorité a vu mieux). Scope de cette tranche :
	// reorg du SOMMET (1 bloc) — un fork enterré plus profond est hors périmètre
	// (le système reste fail-safe). Purgé sous `finalized`.
	forks map[uint64][]*types.Block
	// Slashing : preuves de double-signature en attente d'inclusion dans un
	// bloc (clé voter@height), protégées par e.mu.
	evidence map[string]*types.DoubleSignEvidence
	stopCh   chan struct{}
}

func evidenceKey(voter string, height uint64) string {
	return fmt.Sprintf("%s@%d", voter, height)
}

// lock : verrou POL d'un nœud à une hauteur (round + hash du bloc verrouillé).
type lock struct {
	round uint32
	hash  string
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
		setByHeight:  map[uint64]*state.ValidatorSet{},
		locked:       map[uint64]lock{},
		snapshots:    map[uint64][]byte{},
		forks:        map[uint64][]*types.Block{},
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
	// Fige le set votant de cette hauteur : l'état est ici post-(height-1),
	// donc le set actif est par définition celui qui gouverne height (#5).
	e.freezeSetLocked(height)
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
	// Purge les tx croupies (trou de nonce qui ne se résoudra jamais, ou
	// auteur qui a abandonné). 10 min : assez court pour ne pas accumuler,
	// assez long pour absorber des creux d'inclusion liés à un mempool
	// saturé. Le purge n'est pas dans le state path (pas dans Execute) :
	// la non-déterminisme de time.Now() ne casse rien ici.
	if n := e.pool.PurgeExpired(10 * time.Minute); n > 0 {
		log.Printf("[mempool] %d tx purgée(s) (TTL 10 min — trous de nonce probables)", n)
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
	e.snapshotStateLocked(height) // point de restauration pour un éventuel reorg (#7)
	if len(lastCommit) > 0 {
		e.st.SetFinalized(height - 1)
		e.votes.prune(height - 1)
		e.pruneVotedLocked(height - 1)
		e.pruneSetsLocked(height - 1)
		e.pruneSnapshotsBelow(height - 1) // garde ≥ finalité (point de fork minimal)
		e.pruneForksBelow(height - 1)
	}
	e.lastProgress = time.Now()
	e.persist(b)
	if e.OnBlock != nil {
		e.OnBlock(b)
	}
	e.castVote(height, round, b.Hash)
	return b
}

// validateBlockLocked : valide un bloc `b` censé étendre `prev` (hash du
// parent), SANS muter l'état. Renvoie finalizesParent (le bloc porte-t-il un
// commit ≥ 2/3 du parent ?) et une erreur si quoi que ce soit cloche. Le set
// votant de la hauteur est figé au passage (#5). Suppose e.mu détenu et l'état
// positionné à la hauteur b.Header.Height-1. Partagé par le chemin nominal et
// le fork-choice (#7).
func (e *Engine) validateBlockLocked(b *types.Block, prev string) (finalizesParent bool, err error) {
	if b.Header.PrevHash != prev {
		return false, fmt.Errorf("prev hash mismatch at height %d", b.Header.Height)
	}
	// Fige le set votant de cette hauteur : l'état est ici post-(Height-1) (#5).
	e.freezeSetLocked(b.Header.Height)
	// Le proposeur doit être EXACTEMENT l'élu du round inscrit dans l'en-tête
	// (round 0 nominal, sinon un round de secours). Le round explicite lève
	// toute ambiguïté et rend le comptage d'inactivité déterministe.
	if b.Header.Round >= MaxRounds {
		return false, fmt.Errorf("round %d out of range", b.Header.Round)
	}
	if want := e.st.SelectProposer(b.Header.Height, prev, b.Header.Round); want != b.Header.Proposer {
		return false, fmt.Errorf("wrong proposer at height %d round %d: got %s, want %s", b.Header.Height, b.Header.Round, b.Header.Proposer, want)
	}
	if b.ComputeHash() != b.Hash {
		return false, errors.New("invalid block hash")
	}
	if err := b.VerifyProposerSig(); err != nil {
		return false, fmt.Errorf("proposer signature: %w", err)
	}
	if types.TxRoot(b.Txs) != b.Header.TxRoot {
		return false, errors.New("tx root mismatch")
	}
	if types.EvidenceRoot(b.Evidence) != b.Header.EvidenceRoot {
		return false, errors.New("evidence root mismatch")
	}
	for _, ev := range b.Evidence {
		if err := ev.Verify(e.chainID); err != nil {
			return false, fmt.Errorf("invalid double-sign evidence: %w", err)
		}
	}
	// Commit du parent : si présent, il doit représenter ≥ 2/3 du stake actif
	// sur le bloc parent (height-1, prevHash). C'est ce qui finalise.
	if types.CommitRoot(b.LastCommit) != b.Header.LastCommitRoot {
		return false, errors.New("last commit root mismatch")
	}
	if len(b.LastCommit) > 0 {
		power, cerr := e.verifyCommit(b.LastCommit, b.Header.Height-1, b.Header.PrevHash)
		if cerr != nil {
			return false, fmt.Errorf("invalid last commit: %w", cerr)
		}
		if !hasQuorum(power, e.setForHeight(b.Header.Height-1).Total) {
			return false, errors.New("last commit below 2/3 of the height's validator set")
		}
		finalizesParent = true
	}
	if err := types.VerifyAll(b.Txs); err != nil {
		return false, err
	}
	return finalizesParent, nil
}

// ApplyExternalBlock validates and applies a block received from a peer.
func (e *Engine) ApplyExternalBlock(b *types.Block) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	cur := e.st.GetHeight()
	switch {
	case b.Header.Height <= cur:
		return e.considerFork(b, cur) // déjà committé : bloc connu OU concurrent (fork-choice)
	case b.Header.Height > cur+1:
		return ErrGap
	}
	prev := e.st.GetLastHash()
	finalizesParent, err := e.validateBlockLocked(b, prev)
	if err != nil {
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
	e.snapshotStateLocked(b.Header.Height) // point de restauration reorg (#7)
	if finalizesParent {
		e.st.SetFinalized(b.Header.Height - 1)
		e.votes.prune(b.Header.Height - 1)
		e.pruneVotedLocked(b.Header.Height - 1)
		e.pruneSetsLocked(b.Header.Height - 1)
		e.pruneSnapshotsBelow(b.Header.Height - 1)
		e.pruneForksBelow(b.Header.Height - 1)
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
	e.castVote(b.Header.Height, b.Header.Round, b.Hash)
	return nil
}

// ---- finalité BFT (Phase 2) ----

var errWrongVoteChain = errors.New("vote on wrong chain")

// castVote : émet prevote puis précommit pour le bloc qu'on vient de
// committer. Le précommit reste celui qui décide la finalité ; le prevote
// est posé en préparation de la tranche 2 (locking). Suppose e.mu détenu.
func (e *Engine) castVote(height uint64, round uint32, hash string) {
	e.castVoteKind(height, round, types.PrevoteKind, hash)
	e.castVoteKind(height, round, types.PrecommitKind, hash)
}

// castVoteKind : émet un vote d'un kind donné, l'ajoute au pool et le diffuse.
//
// INVARIANT DE SÛRETÉ BFT : un validateur ne signe jamais deux votes du même
// kind à la même hauteur. Sinon il produit lui-même une preuve d'équivocation
// et se fait slasher. Ce garde-fou est aussi le prérequis d'un futur
// fork-choice (la règle de verrouillage POL viendra en tranche 2).
func (e *Engine) castVoteKind(height uint64, round uint32, kind, hash string) {
	if e.key == nil {
		return
	}
	// Anti-auto-équivocation, désormais consciente du round : on ne signe jamais
	// deux votes du même kind à la même hauteur ET au même round pour des hash
	// différents (= notre propre preuve d'équivocation). Signer à un round
	// DIFFÉRENT est en revanche autorisé (changement légitime, voir le verrou).
	rk := rkKey(round, kind)
	if e.voted[height] == nil {
		e.voted[height] = map[string]string{}
	}
	if prev, ok := e.voted[height][rk]; ok {
		if prev != hash {
			log.Printf("[consensus] SÛRETÉ : refus de signer un 2e %s (%s…) à la hauteur #%d round %d — déjà voté %s… (anti-auto-équivocation)", kind, short(hash), height, round, short(prev))
		}
		return
	}

	// Règle de VERROUILLAGE (POL), uniquement pour les précommits : si ce nœud
	// est verrouillé sur un autre bloc à cette hauteur, il ne précommet le
	// nouveau QUE s'il existe une polka pour ce nouveau bloc à un round
	// STRICTEMENT supérieur au verrou (preuve d'un quorum plus récent). Sinon il
	// reste fidèle à son verrou. Au PREMIER précommit d'une hauteur (cas nominal
	// sans reorg), aucun verrou n'existe → comportement identique à l'historique.
	if kind == types.PrecommitKind {
		if lk, ok := e.locked[height]; ok && lk.hash != hash {
			if !(round > lk.round && e.hasPolka(height, round, hash)) {
				log.Printf("[consensus] VERROU : reste verrouillé sur %s… (round %d) à la hauteur #%d — refus de précommettre %s… sans polka de round supérieur", short(lk.hash), lk.round, height, short(hash))
				return
			}
		}
		e.locked[height] = lock{round: round, hash: hash} // (re)verrouillage
	}

	e.voted[height][rk] = hash
	v := &types.Vote{ChainID: e.chainID, Height: height, Round: round, Kind: kind, BlockHash: hash}
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

// pruneVotedLocked oublie les votes émis ET les verrous aux hauteurs déjà
// finalisées (une hauteur finalisée ne peut plus changer — verrou inutile).
func (e *Engine) pruneVotedLocked(upTo uint64) {
	for h := range e.voted {
		if h <= upTo {
			delete(e.voted, h)
		}
	}
	for h := range e.locked {
		if h <= upTo {
			delete(e.locked, h)
		}
	}
}

// recordEquivocation : transforme un précommit conflictuel en preuve à
// inclure dans un futur bloc (sauf si la faute est déjà punie ou déjà connue).
//
// #8 : les PREVOTES équivoques sont désormais punis comme les précommits.
// Signer deux votes (du même kind) pour des blocs différents à la même hauteur
// ET au même round est une faute byzantine prouvable — que ce soit un prevote
// ou un précommit. (`prev` et `cur` partagent kind et round : le votePool les
// détecte par (hauteur, round, kind, voter), donc un changement cross-round
// légitime n'arrive jamais ici.) Le chemin de slash en aval est kind-agnostique.
func (e *Engine) recordEquivocation(prev, cur *types.Vote) {
	if cur.Kind != types.PrecommitKind && cur.Kind != types.PrevoteKind {
		return
	}
	key := evidenceKey(cur.Voter, cur.Height)
	if _, exists := e.evidence[key]; exists || e.st.IsSlashed(cur.Voter, cur.Height) {
		return
	}
	e.evidence[key] = &types.DoubleSignEvidence{
		Height: cur.Height, Voter: cur.Voter, VoteA: prev, VoteB: cur,
	}
	log.Printf("[consensus] ÉQUIVOCATION détectée : %s a signé 2 %ss en conflit à la hauteur #%d round %d — preuve en attente de slash", cur.Voter, cur.Kind, cur.Height, cur.Round)
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
	if e.setForHeight(v.Height).PowerOf(v.Voter) == 0 {
		return false, errors.New("voter not in the height's validator set")
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
	// Un nouveau PREVOTE peut compléter une polka pour un bloc concurrent
	// bufferisé → réévaluer le fork-choice (#7). No-op si rien ne qualifie.
	if v.Kind == types.PrevoteKind {
		if err := e.tryReorgLocked(v.Height); err != nil {
			log.Printf("[consensus] reorg déclenché par vote : %v", err)
		}
	}
	return true, nil
}

// hasPolka : existe-t-il une POLKA (Proof-of-Lock) pour (height, round, hash) —
// c.-à-d. ≥ 2/3 du pouvoir du set FIGÉ de la hauteur en PREVOTES sur ce bloc à
// ce round ? La polka est la preuve qu'une super-majorité a vu ce bloc à ce
// round ; c'est elle qui autorise un validateur à se (re)verrouiller dessus
// (#6, chemin B). Les prevotes sont vérifiés (signature, votant dans le set,
// pas de doublon). Suppose e.mu détenu.
func (e *Engine) hasPolka(height uint64, round uint32, hash string) bool {
	cand := e.votes.prevoters(height, round, hash)
	if len(cand) == 0 {
		return false
	}
	set := e.setForHeight(height)
	seen := map[string]bool{}
	var power uint64
	for _, v := range cand {
		if v.Kind != types.PrevoteKind || v.Height != height || v.Round != round || v.BlockHash != hash {
			continue
		}
		if seen[v.Voter] {
			continue
		}
		p := set.PowerOf(v.Voter)
		if p == 0 {
			continue // pas dans le set figé de cette hauteur
		}
		if v.Verify() != nil {
			continue
		}
		seen[v.Voter] = true
		power += p
	}
	return hasQuorum(power, set.Total)
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
	if err != nil || !hasQuorum(power, e.setForHeight(height).Total) {
		return nil
	}
	return cand
}

// freezeSetLocked fige le set de validateurs qui gouverne la hauteur `height`,
// s'il ne l'est pas déjà. Appelé À L'ENTRÉE du traitement du bloc `height`,
// quand l'état est exactement post-(height-1) : le set actif à cet instant est
// par définition le set votant de `height`. Suppose e.mu détenu.
func (e *Engine) freezeSetLocked(height uint64) {
	if _, ok := e.setByHeight[height]; !ok {
		e.setByHeight[height] = e.st.SnapshotActiveSet()
	}
}

// setForHeight renvoie le set figé qui gouverne la hauteur `height`. Repli sur
// une photo de l'état courant si la hauteur n'est pas (encore) figée — cas
// limité : (a) vote reçu sur la hauteur suivante avant traitement de son bloc
// (l'état courant EST alors le bon set), (b) unique hauteur non finalisée juste
// après un redémarrage. Le repli n'est jamais pire que l'ancien comportement
// (qui lisait toujours l'état vivant). Suppose e.mu détenu.
func (e *Engine) setForHeight(height uint64) *state.ValidatorSet {
	if vs, ok := e.setByHeight[height]; ok {
		return vs
	}
	return e.st.SnapshotActiveSet()
}

// pruneSetsLocked oublie les sets figés des hauteurs déjà finalisées.
func (e *Engine) pruneSetsLocked(upTo uint64) {
	for h := range e.setByHeight {
		if h <= upTo {
			delete(e.setByHeight, h)
		}
	}
}

// snapshotStateLocked mémorise l'état (bytes) APRÈS la hauteur `height` — point
// de restauration possible pour un reorg (#7). Suppose e.mu détenu.
func (e *Engine) snapshotStateLocked(height uint64) {
	e.snapshots[height] = e.st.Bytes()
}

// pruneSnapshotsBelow purge les snapshots strictement SOUS `keep` : une fois une
// hauteur finalisée (immuable), aucun reorg ne peut descendre en-dessous, donc
// son point de restauration n'est plus utile. On GARDE `keep` lui-même (point
// de fork minimal possible). Suppose e.mu détenu.
func (e *Engine) pruneSnapshotsBelow(keep uint64) {
	for h := range e.snapshots {
		if h < keep {
			delete(e.snapshots, h)
		}
	}
}

// RewindTo restaure l'état au point de fin de la hauteur `height` à partir du
// snapshot conservé, et repositionne la hauteur du moteur. Échoue si aucun
// snapshot n'est disponible (hauteur trop ancienne — déjà purgée car finalisée,
// ou jamais traitée dans cette session). C'est la brique de bas niveau du
// fork-choice : l'appelant rejoue ensuite la branche concurrente depuis
// height+1. NE descend JAMAIS sous la dernière hauteur finalisée.
func (e *Engine) RewindTo(height uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if height < e.st.GetFinalized() {
		return fmt.Errorf("rewind refusé : hauteur %d sous la finalité %d (immuable)", height, e.st.GetFinalized())
	}
	snap, ok := e.snapshots[height]
	if !ok {
		return fmt.Errorf("rewind impossible : pas de snapshot pour la hauteur %d", height)
	}
	if err := e.st.Restore(snap); err != nil {
		return fmt.Errorf("rewind : restauration échouée : %w", err)
	}
	// Les votes/verrous/sets des hauteurs > height appartiennent à la branche
	// abandonnée : on les oublie pour repartir proprement.
	for h := range e.voted {
		if h > height {
			delete(e.voted, h)
		}
	}
	for h := range e.locked {
		if h > height {
			delete(e.locked, h)
		}
	}
	for h := range e.snapshots {
		if h > height {
			delete(e.snapshots, h)
		}
	}
	// NB : les votes accumulés pour les hauteurs > height (branche abandonnée)
	// sont laissés au votePool ; ils sont dédupliqués par hash, réévalués contre
	// le set figé, et purgés à la finalisation.
	return nil
}

// pruneForksBelow oublie les blocs concurrents bufferisés sous `keep` (hauteurs
// finalisées : plus aucun reorg possible). Suppose e.mu détenu.
func (e *Engine) pruneForksBelow(keep uint64) {
	for h := range e.forks {
		if h < keep {
			delete(e.forks, h)
		}
	}
}

// considerFork gère un bloc reçu à une hauteur DÉJÀ committée (≤ sommet). Soit
// c'est le bloc qu'on a déjà (no-op), soit c'est un bloc concurrent → on
// l'évalue pour un éventuel reorg (#7). Scope de cette tranche : reorg du SOMMET
// uniquement (b à la hauteur courante). Un fork enterré plus profond est hors
// périmètre — le système reste fail-safe (jamais de double-finalité). Suppose
// e.mu détenu.
func (e *Engine) considerFork(b *types.Block, cur uint64) error {
	H := b.Header.Height
	if H <= e.st.GetFinalized() {
		return nil // finalisée : immuable
	}
	if H != cur {
		return nil // fork enterré : hors scope
	}
	if b.Hash == e.st.GetLastHash() {
		return nil // exactement notre bloc courant
	}
	e.bufferForkLocked(b)
	return e.tryReorgLocked(H)
}

func (e *Engine) bufferForkLocked(b *types.Block) {
	for _, f := range e.forks[b.Header.Height] {
		if f.Hash == b.Hash {
			return // déjà bufferisé
		}
	}
	e.forks[b.Header.Height] = append(e.forks[b.Header.Height], b)
}

// tryReorgLocked bascule vers un bloc concurrent bufferisé à la hauteur `H` (=
// sommet courant) s'il porte une POLKA à un round STRICTEMENT supérieur à notre
// bloc courant (preuve qu'une super-majorité a vu mieux à un round plus récent —
// règle POL). No-op si aucun candidat ne qualifie. Suppose e.mu détenu.
func (e *Engine) tryReorgLocked(H uint64) error {
	if H != e.st.GetHeight() {
		return nil // plus le sommet
	}
	ourRound := uint32(0)
	if lk, ok := e.locked[H]; ok {
		ourRound = lk.round
	}
	for _, b := range e.forks[H] {
		if b.Hash == e.st.GetLastHash() || b.Header.Round <= ourRound {
			continue
		}
		if !e.hasPolka(H, b.Header.Round, b.Hash) {
			continue // pas de quorum de prevotes plus récent → on reste fidèle
		}
		return e.reorgToTipLocked(b)
	}
	return nil
}

// reorgToTipLocked exécute la bascule vers `b` (à la hauteur courante) : restaure
// l'état au point de fork (H-1), valide + exécute b, re-vote. En cas d'échec,
// restaure NOTRE tip courant — jamais de perte. Suppose e.mu détenu et
// b.Header.Height == e.st.GetHeight().
func (e *Engine) reorgToTipLocked(b *types.Block) error {
	H := b.Header.Height
	if H-1 < e.st.GetFinalized() {
		return nil // ne jamais rembobiner sous la finalité
	}
	forkSnap, ok := e.snapshots[H-1]
	if !ok {
		return fmt.Errorf("reorg : pas de snapshot au point de fork %d", H-1)
	}
	oldHash := e.st.GetLastHash()
	backup := e.st.Bytes() // filet : notre tip courant
	if err := e.st.Restore(forkSnap); err != nil {
		return err
	}
	prev := e.st.GetLastHash()
	finalizesParent, err := e.validateBlockLocked(b, prev)
	if err != nil {
		e.st.Restore(backup)
		return err
	}
	absent := e.absentProposers(H, prev, b.Header.Round, b.Header.Proposer)
	_, _, root, err := e.st.Execute(b.Txs, b.Evidence, absent, b.Header.Proposer, b.Header.Timestamp, true)
	if err != nil || root != b.Header.StateRoot {
		e.st.Restore(backup) // on garde notre bloc d'origine
		if err == nil {
			err = errors.New("state root mismatch on reorg")
		}
		return err
	}
	e.st.Commit(H, b.Hash)
	e.snapshotStateLocked(H)
	log.Printf("[consensus] REORG #%d : %s… → %s… (round %d, justifié par polka)", H, short(oldHash), short(b.Hash), b.Header.Round)
	if finalizesParent {
		e.st.SetFinalized(H - 1)
		e.votes.prune(H - 1)
		e.pruneVotedLocked(H - 1)
		e.pruneSetsLocked(H - 1)
		e.pruneSnapshotsBelow(H - 1)
		e.pruneForksBelow(H - 1)
	}
	e.lastProgress = time.Now()
	e.persist(b)
	for _, tx := range b.Txs {
		e.pool.Drop(tx.Hash())
	}
	// On abandonne notre vote/verrou de l'ancien bloc à H pour re-voter b. Le
	// changement est à un round SUPÉRIEUR → légitime (pas d'auto-équivocation,
	// cf castVoteKind + evidence round-aware).
	delete(e.voted, H)
	delete(e.locked, H)
	e.castVote(H, b.Header.Round, b.Hash)
	return nil
}

// verifyCommit valide un ensemble de précommits sur (height, hash) et renvoie
// le pouvoir actif cumulé des votants distincts, MESURÉ CONTRE LE SET FIGÉ de
// `height` (#5). Sert au proposeur (build) et aux suiveurs (validation du
// LastCommit reçu).
func (e *Engine) verifyCommit(votes []*types.Vote, height uint64, hash string) (uint64, error) {
	set := e.setForHeight(height)
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
		p := set.PowerOf(v.Voter) // pouvoir FIGÉ de la hauteur, pas l'état vivant
		if p == 0 {
			return 0, errors.New("commit voter not in the height's validator set")
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
