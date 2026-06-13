package consensus

import (
	"sync"

	"chaingo/internal/types"
)

// votePool accumule les précommits par (hauteur, hash de bloc) et calcule
// le pouvoir de vote cumulé. Pondération et seuil 2/3 sont décidés par
// l'appelant (l'Engine), qui connaît le pouvoir de chaque validateur.
type votePool struct {
	mu sync.Mutex
	// height -> blockHash -> voterAddr -> power
	votes map[uint64]map[string]map[string]uint64
	seen  map[string]bool // hash de vote -> déjà vu (déduplication)
}

func newVotePool() *votePool {
	return &votePool{
		votes: map[uint64]map[string]map[string]uint64{},
		seen:  map[string]bool{},
	}
}

// add enregistre un vote avec le pouvoir du votant. Renvoie (nouveau,
// pouvoir cumulé sur ce (hauteur,hash)). nouveau=false si déjà vu.
func (p *votePool) add(v *types.Vote, power uint64) (bool, uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	h := v.Hash()
	if p.seen[h] {
		return false, p.powerLocked(v.Height, v.BlockHash)
	}
	p.seen[h] = true
	if p.votes[v.Height] == nil {
		p.votes[v.Height] = map[string]map[string]uint64{}
	}
	if p.votes[v.Height][v.BlockHash] == nil {
		p.votes[v.Height][v.BlockHash] = map[string]uint64{}
	}
	p.votes[v.Height][v.BlockHash][v.Voter] = power
	return true, p.powerLocked(v.Height, v.BlockHash)
}

func (p *votePool) powerLocked(height uint64, hash string) uint64 {
	var total uint64
	for _, power := range p.votes[height][hash] {
		total += power
	}
	return total
}

func (p *votePool) power(height uint64, hash string) uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.powerLocked(height, hash)
}

// prune supprime les votes des hauteurs déjà finalisées.
func (p *votePool) prune(upTo uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for h := range p.votes {
		if h <= upTo {
			delete(p.votes, h)
		}
	}
}

// hasQuorum : le pouvoir cumulé dépasse-t-il strictement 2/3 du total ?
func hasQuorum(power, total uint64) bool {
	return total > 0 && power*3 > total*2
}
