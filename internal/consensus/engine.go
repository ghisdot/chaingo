// Package consensus : moteur PoS "Aurora" v1 — rotation déterministe du
// proposeur pondérée par le stake, finalité immédiate (pas de fork choice
// en devnet ; les votes BFT multi-validateurs arrivent en Phase 2).
package consensus

import (
	"errors"
	"fmt"
	"log"
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
	stopCh       chan struct{}
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
		stopCh:       make(chan struct{}),
	}
	if key != nil {
		e.addr = key.Address()
	}
	return e
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
	if len(txs) == 0 && !force {
		return nil
	}
	// Le timestamp est fixé AVANT l'exécution : il fait partie des règles
	// (déblocage des unbondings) et doit être identique chez les suiveurs.
	blockTime := time.Now().UnixMilli()
	applied, failed, root, _ := e.st.Execute(txs, e.addr, blockTime, false)
	for h, err := range failed {
		log.Printf("[consensus] tx %s… rejected: %v", h[:12], err)
		e.pool.Drop(h)
	}
	for _, tx := range applied {
		e.pool.Drop(tx.Hash())
	}
	b := &types.Block{
		Header: types.BlockHeader{
			Height:    height,
			PrevHash:  prev,
			Timestamp: blockTime,
			Proposer:  e.addr,
			TxRoot:    types.TxRoot(applied),
			StateRoot: root,
		},
		Txs: applied,
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
	if err := types.VerifyAll(b.Txs); err != nil {
		return err
	}
	snapshot := e.st.Bytes()
	_, _, root, err := e.st.Execute(b.Txs, b.Header.Proposer, b.Header.Timestamp, true)
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
	log.Printf("[consensus] block #%d accepted from peer: %d tx(s)", b.Header.Height, len(b.Txs))
	return nil
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
