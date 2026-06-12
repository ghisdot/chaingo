// Package mempool gère la file d'attente des transactions. Le tip est un
// marché : les transactions les mieux-disantes sont servies en premier
// (l'option --fast du CLI n'est qu'un préréglage de tip élevé).
package mempool

import (
	"errors"
	"sort"
	"sync"

	"chaingo/internal/types"
)

type Mempool struct {
	mu  sync.Mutex
	txs map[string]*types.Transaction
	max int
}

func New(max int) *Mempool {
	return &Mempool{txs: map[string]*types.Transaction{}, max: max}
}

var (
	ErrDuplicate = errors.New("tx already in mempool")
	ErrFull      = errors.New("mempool full")
)

// Add validates the transaction (including its post-quantum signature —
// call from request goroutines, the heavy work happens outside the lock)
// and queues it. Returns true if the tx is new.
func (m *Mempool) Add(tx *types.Transaction) (bool, error) {
	if err := tx.ValidateBasic(); err != nil {
		return false, err
	}
	h := tx.Hash()
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.txs[h]; ok {
		return false, ErrDuplicate
	}
	if len(m.txs) >= m.max {
		return false, ErrFull
	}
	m.txs[h] = tx
	return true, nil
}

func (m *Mempool) Size() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.txs)
}

func (m *Mempool) Drop(hash string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.txs, hash)
}

// Take selects up to max transactions for the next block. Txs are
// grouped per account (nonce order is mandatory within an account), and
// accounts are served by the tip of their next pending tx — best bidders
// first. Txs whose nonce is not yet reachable stay for a later block.
func (m *Mempool) Take(max int, nonceOf func(addr string) uint64) []*types.Transaction {
	m.mu.Lock()
	groups := map[string][]*types.Transaction{}
	for _, tx := range m.txs {
		groups[tx.From] = append(groups[tx.From], tx)
	}
	m.mu.Unlock()

	type chain struct {
		txs []*types.Transaction // chaîne de nonces consécutifs, prête à inclure
		tip uint64               // tip de la tx de tête (l'enchère du compte)
	}
	var chains []chain
	for from, txs := range groups {
		sort.Slice(txs, func(i, j int) bool { return txs[i].Nonce < txs[j].Nonce })
		exp := nonceOf(from)
		var ready []*types.Transaction
		for _, tx := range txs {
			if tx.Nonce != exp {
				continue // trou de nonce (ou doublon) : attendra un prochain bloc
			}
			ready = append(ready, tx)
			exp++
		}
		if len(ready) > 0 {
			chains = append(chains, chain{txs: ready, tip: ready[0].Tip})
		}
	}
	sort.Slice(chains, func(i, j int) bool {
		if chains[i].tip != chains[j].tip {
			return chains[i].tip > chains[j].tip
		}
		return chains[i].txs[0].From < chains[j].txs[0].From
	})

	var out []*types.Transaction
	for _, c := range chains {
		for _, tx := range c.txs {
			if len(out) >= max {
				return out
			}
			out = append(out, tx)
		}
	}
	return out
}
