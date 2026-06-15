// Package mempool gère la file d'attente des transactions. Le tip est un
// marché : les transactions les mieux-disantes sont servies en premier
// (l'option --fast du CLI n'est qu'un préréglage de tip élevé).
package mempool

import (
	"errors"
	"sort"
	"sync"
	"time"

	"chaingo/internal/types"
)

// entry : on garde l'horodatage d'arrivée pour deux raisons : permettre
// la purge des tx coincées (trou de nonce qui ne se résoudra jamais) et
// alimenter le diagnostic via /v1/mempool.
type entry struct {
	tx      *types.Transaction
	addedAt time.Time
}

type Mempool struct {
	mu  sync.Mutex
	txs map[string]*entry
	max int
}

func New(max int) *Mempool {
	return &Mempool{txs: map[string]*entry{}, max: max}
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
	m.txs[h] = &entry{tx: tx, addedAt: time.Now()}
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

// PurgeExpired drops les tx qui croupissent depuis plus de maxAge —
// typiquement des trous de nonce qui ne se résoudront jamais (le compte
// a abandonné la séquence). Renvoie le nombre purgé. À appeler depuis le
// moteur (PAS depuis le state path : time.Now() y est interdit, mais ici
// on est en coordination locale, pas en consensus).
func (m *Mempool) PurgeExpired(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	var n int
	for h, e := range m.txs {
		if e.addedAt.Before(cutoff) {
			delete(m.txs, h)
			n++
		}
	}
	return n
}

// PendingInfo : snapshot léger de l'état du mempool pour le diagnostic
// (/v1/mempool). N'expose ni signature ni mémo — juste de quoi voir
// qui a quoi en attente, et depuis combien de temps.
type PendingInfo struct {
	Hash    string `json:"hash"`
	From    string `json:"from"`
	Type    string `json:"type"`
	Nonce   uint64 `json:"nonce"`
	Tip     uint64 `json:"tip"`
	AgeSecs int64  `json:"age_secs"`
}

// Snapshot retourne les tx en attente, triées par âge décroissant.
// Limite : au plus `max` entrées (0 = pas de limite). Utile pour repérer
// les trous de nonce : si tu vois plusieurs entrées du même `from` avec
// des nonces non consécutifs, c'est ton diagnostic.
func (m *Mempool) Snapshot(max int) []PendingInfo {
	m.mu.Lock()
	out := make([]PendingInfo, 0, len(m.txs))
	now := time.Now()
	for h, e := range m.txs {
		out = append(out, PendingInfo{
			Hash:    h,
			From:    e.tx.From,
			Type:    string(e.tx.Type),
			Nonce:   e.tx.Nonce,
			Tip:     e.tx.Tip,
			AgeSecs: int64(now.Sub(e.addedAt).Seconds()),
		})
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].AgeSecs > out[j].AgeSecs })
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

// Take selects up to max transactions for the next block. Txs are
// grouped per account (nonce order is mandatory within an account), and
// accounts are served by the tip of their next pending tx — best bidders
// first. Txs whose nonce is not yet reachable stay for a later block.
func (m *Mempool) Take(max int, nonceOf func(addr string) uint64) []*types.Transaction {
	m.mu.Lock()
	groups := map[string][]*types.Transaction{}
	for _, e := range m.txs {
		groups[e.tx.From] = append(groups[e.tx.From], e.tx)
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
