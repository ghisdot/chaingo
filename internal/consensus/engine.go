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

	// Finalité BFT (Phase 2) : pool de précommits + hauteur finalisée.
	votes     *votePool
	finalized uint64
	chainID   string
	OnVote    func(*types.Vote) // hook broadcast p2p des votes
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

// FinalizedHeight : dernière hauteur finalisée par ≥ 2/3 du stake.
func (e *Engine) FinalizedHeight() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.finalized
}

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
	applied, failed, root, _ := e.st.Execute(txs, evid, e.addr, blockTime, false)
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
			Height:       height,
			PrevHash:     prev,
			Timestamp:    blockTime,
			Proposer:     e.addr,
			TxRoot:       types.TxRoot(applied),
			EvidenceRoot: types.EvidenceRoot(evid),
			StateRoot:    root,
		},
		Txs:      applied,
		Evidence: evid,
	}
	b.Hash = b.ComputeHash()
	b.ProposerPubKey = e.key.PubBytes()
	b.ProposerSignature = e.key.Sign(b.SigningBytes())
	e.st.Commit(height, b.Hash)
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
	// Le proposeur doit être l'élu d'un des rounds de la hauteur (le
	// round 0 en temps normal, un round de secours si des validateurs
	// étaient hors-ligne).
	validProposer := false
	for r := uint32(0); r < MaxRounds; r++ {
		if e.st.SelectProposer(b.Header.Height, prev, r) == b.Header.Proposer {
			validProposer = true
			break
		}
	}
	if !validProposer {
		return fmt.Errorf("proposer %s not elected for any round at height %d", b.Header.Proposer, b.Header.Height)
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
	if err := types.VerifyAll(b.Txs); err != nil {
		return err
	}
	snapshot := e.st.Bytes()
	_, _, root, err := e.st.Execute(b.Txs, b.Evidence, b.Header.Proposer, b.Header.Timestamp, true)
	if err != nil || root != b.Header.StateRoot {
		e.st.Restore(snapshot)
		if err == nil {
			err = errors.New("state root mismatch")
		}
		return err
	}
	e.st.Commit(b.Header.Height, b.Hash)
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

// castVote : ce nœud, s'il est validateur, précommit le bloc qu'il vient
// de committer, l'ajoute au pool et le diffuse. Suppose e.mu détenu.
func (e *Engine) castVote(height uint64, hash string) {
	if e.key == nil {
		return
	}
	v := &types.Vote{ChainID: e.chainID, Height: height, BlockHash: hash}
	v.SignWith(e.key)
	e.votes.add(v, e.st.PowerOf(e.addr))
	if e.OnVote != nil {
		e.OnVote(v)
	}
	e.checkFinalityLocked(height, hash)
}

// recordEquivocation : transforme un vote conflictuel en preuve à inclure
// dans un futur bloc (sauf si la faute est déjà punie ou déjà connue).
func (e *Engine) recordEquivocation(prev, cur *types.Vote) {
	key := evidenceKey(cur.Voter, cur.Height)
	if _, exists := e.evidence[key]; exists || e.st.IsSlashed(cur.Voter, cur.Height) {
		return
	}
	e.evidence[key] = &types.DoubleSignEvidence{
		Height: cur.Height, Voter: cur.Voter, VoteA: prev, VoteB: cur,
	}
	log.Printf("[consensus] ÉQUIVOCATION détectée : %s a précommit 2 blocs à la hauteur #%d — preuve en attente de slash", cur.Voter, cur.Height)
}

// AddVote : précommit reçu d'un pair. Renvoie true s'il est nouveau (à
// re-diffuser). Valide la signature et le statut de validateur du votant.
func (e *Engine) AddVote(v *types.Vote) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.chainID != "" && v.ChainID != e.chainID {
		return false, errWrongVoteChain
	}
	if v.Height <= e.finalized {
		return false, nil // hauteur déjà finalisée
	}
	power := e.st.PowerOf(v.Voter)
	if power == 0 {
		return false, errors.New("voter is not an active validator")
	}
	if err := v.Verify(); err != nil {
		return false, err
	}
	isNew, _, equiv := e.votes.add(v, power)
	if !isNew {
		return false, nil
	}
	if equiv != nil {
		e.recordEquivocation(equiv, v)
	}
	e.checkFinalityLocked(v.Height, e.localHash(v.Height))
	return true, nil
}

// checkFinalityLocked : si le pouvoir cumulé des précommits sur NOTRE bloc
// à `height` dépasse 2/3 du stake, la hauteur est finalisée. Suppose e.mu détenu.
func (e *Engine) checkFinalityLocked(height uint64, localHash string) {
	if height <= e.finalized || localHash == "" {
		return
	}
	power := e.votes.power(height, localHash)
	total := e.st.TotalPower()
	if hasQuorum(power, total) {
		e.finalized = height
		e.votes.prune(height)
		log.Printf("[consensus] hauteur #%d FINALISÉE (%d%% du stake a précommit)", height, power*100/total)
	}
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

// localHash : hash du bloc local à une hauteur (db, ou état courant).
func (e *Engine) localHash(height uint64) string {
	if e.db != nil {
		if b, _ := e.db.GetBlock(height); b != nil {
			return b.Hash
		}
	}
	if height == e.st.GetHeight() {
		return e.st.GetLastHash()
	}
	return ""
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
