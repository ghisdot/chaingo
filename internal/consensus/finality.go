package consensus

import (
	"sort"
	"sync"

	"chaingo/internal/types"
)

// votePool accumule les votes BFT (prevotes ET precommits) par (hauteur, kind,
// hash de bloc). Il stocke les votes pour pouvoir reconstruire le COMMIT
// (precommits ≥ 2/3) inclus dans le bloc suivant — c'est lui qui rend la
// finalité persistante et vérifiable. Les prevotes y vivent aussi (tranche 1
// du verrouillage) : la tranche 2 s'en servira pour le lock/POL.
type votePool struct {
	mu sync.Mutex
	// height -> kind -> blockHash -> voterAddr -> vote
	votes map[uint64]map[string]map[string]map[string]*types.Vote
	seen  map[string]bool // hash de vote -> déjà vu (déduplication)
	// height -> kind -> voterAddr -> premier Vote vu (détection d'équivocation,
	// par kind : équivoque un précommit ≠ équivoque un prevote).
	first map[uint64]map[string]map[string]*types.Vote
}

func newVotePool() *votePool {
	return &votePool{
		votes: map[uint64]map[string]map[string]map[string]*types.Vote{},
		seen:  map[string]bool{},
		first: map[uint64]map[string]map[string]*types.Vote{},
	}
}

// add enregistre un vote. Renvoie (nouveau, équivocation). Si le votant avait
// déjà signé un autre hash POUR LE MÊME KIND à cette hauteur, `equiv` contient
// le vote conflictuel précédent (preuve de double-signature).
func (p *votePool) add(v *types.Vote) (isNew bool, equiv *types.Vote) {
	p.mu.Lock()
	defer p.mu.Unlock()
	h := v.Hash()
	if p.seen[h] {
		return false, nil
	}
	p.seen[h] = true

	if p.first[v.Height] == nil {
		p.first[v.Height] = map[string]map[string]*types.Vote{}
	}
	if p.first[v.Height][v.Kind] == nil {
		p.first[v.Height][v.Kind] = map[string]*types.Vote{}
	}
	if prev, ok := p.first[v.Height][v.Kind][v.Voter]; ok {
		if prev.BlockHash != v.BlockHash {
			equiv = prev
		}
	} else {
		p.first[v.Height][v.Kind][v.Voter] = v
	}

	if p.votes[v.Height] == nil {
		p.votes[v.Height] = map[string]map[string]map[string]*types.Vote{}
	}
	if p.votes[v.Height][v.Kind] == nil {
		p.votes[v.Height][v.Kind] = map[string]map[string]*types.Vote{}
	}
	if p.votes[v.Height][v.Kind][v.BlockHash] == nil {
		p.votes[v.Height][v.Kind][v.BlockHash] = map[string]*types.Vote{}
	}
	p.votes[v.Height][v.Kind][v.BlockHash][v.Voter] = v
	return true, equiv
}

// prevoters renvoie les PREVOTES connus pour (hauteur, round, hash) — la base
// de la détection de polka (POL). Filtre par round : seuls les prevotes émis
// au round demandé comptent pour la polka de ce round.
func (p *votePool) prevoters(height uint64, round uint32, hash string) []*types.Vote {
	p.mu.Lock()
	defer p.mu.Unlock()
	m := p.votes[height][types.PrevoteKind][hash]
	if len(m) == 0 {
		return nil
	}
	out := make([]*types.Vote, 0, len(m))
	for _, v := range m {
		if v.Round == round {
			out = append(out, v)
		}
	}
	return out
}

// commitVotes renvoie les PRECOMMITS connus pour (hauteur, hash), triés par
// votant (ordre déterministe pour la racine de commit).
func (p *votePool) commitVotes(height uint64, hash string) []*types.Vote {
	p.mu.Lock()
	defer p.mu.Unlock()
	m := p.votes[height][types.PrecommitKind][hash]
	if len(m) == 0 {
		return nil
	}
	addrs := make([]string, 0, len(m))
	for a := range m {
		addrs = append(addrs, a)
	}
	sort.Strings(addrs)
	out := make([]*types.Vote, 0, len(m))
	for _, a := range addrs {
		out = append(out, m[a])
	}
	return out
}

// prune supprime les votes des hauteurs déjà finalisées.
func (p *votePool) prune(upTo uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for h := range p.votes {
		if h <= upTo {
			delete(p.votes, h)
			delete(p.first, h)
		}
	}
}

// hasQuorum : le pouvoir cumulé dépasse-t-il strictement 2/3 du total ?
func hasQuorum(power, total uint64) bool {
	return total > 0 && power*3 > total*2
}
