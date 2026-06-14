package consensus

import (
	"testing"
	"time"

	"chaingo/internal/crypto"
	"chaingo/internal/mempool"
	"chaingo/internal/state"
	"chaingo/internal/types"
)

// Harnais d'intégration : N moteurs câblés en mémoire. Le « gossip » est
// simulé par un bus de messages (blocs/votes mis en file dans OnBlock/OnVote,
// puis distribués hors des verrous) — donc déterministe, rapide et sans
// sockets, tout en exerçant le VRAI chemin de consensus (production,
// validation, votes, commit, finalité).
type simNet struct {
	t       *testing.T
	keys    []*crypto.KeyPair
	engines []*Engine
	states  []*state.State
	blocks  []*types.Block // file de blocs à distribuer
	votes   []*types.Vote  // file de votes à distribuer
	chain   []*types.Block // journal des blocs produits (pour la synchro tardive)
}

func newSimNet(t *testing.T, n int) *simNet {
	s := &simNet{t: t}
	for i := 0; i < n; i++ {
		k, _ := crypto.GenerateKeyPair()
		s.keys = append(s.keys, k)
	}
	for i := 0; i < n; i++ {
		st := state.New()
		st.SetParams(types.DefaultParams())
		for _, k := range s.keys {
			st.BootstrapStake(k.Address(), 1_000_000*types.Unit)
		}
		eng := New(st, mempool.New(100), nil, s.keys[i], time.Hour, 100)
		eng.SetChainID("sim")
		eng.OnBlock = func(b *types.Block) { s.blocks = append(s.blocks, b); s.chain = append(s.chain, b) }
		eng.OnVote = func(v *types.Vote) { s.votes = append(s.votes, v) }
		s.engines = append(s.engines, eng)
		s.states = append(s.states, st)
	}
	return s
}

// deliver distribue tous les messages en file (blocs puis votes) à tous les
// moteurs, jusqu'à épuisement (la distribution d'un bloc génère de nouveaux
// votes). ApplyExternalBlock/AddVote sont idempotents.
func (s *simNet) deliver() {
	for len(s.blocks) > 0 || len(s.votes) > 0 {
		bs := s.blocks
		s.blocks = nil
		for _, b := range bs {
			for _, e := range s.engines {
				e.ApplyExternalBlock(b)
			}
		}
		vs := s.votes
		s.votes = nil
		for _, v := range vs {
			for _, e := range s.engines {
				e.AddVote(v)
			}
		}
	}
}

// step produit une hauteur (le proposeur élu produit, les autres suivent) et
// distribue tout.
func (s *simNet) step() {
	for _, e := range s.engines {
		e.ProduceOnce(true)
	}
	s.deliver()
}

// TestMultiValidatorConsensusAndFinality : 4 validateurs convergent (même
// hauteur, même hash, même racine d'état) et finalisent en continu.
func TestMultiValidatorConsensusAndFinality(t *testing.T) {
	net := newSimNet(t, 4)
	const H = 8
	for h := 1; h <= H; h++ {
		net.step()
		// Tous les nœuds à la même hauteur, même dernier hash, même racine.
		h0 := net.states[0].GetHeight()
		hash0 := net.states[0].GetLastHash()
		root0 := net.states[0].Root()
		for i, st := range net.states {
			if st.GetHeight() != h0 {
				t.Fatalf("hauteur #%d : nœud %d à %d, nœud 0 à %d", h, i, st.GetHeight(), h0)
			}
			if st.GetLastHash() != hash0 {
				t.Fatalf("hauteur #%d : hash divergent au nœud %d", h, i)
			}
			if st.Root() != root0 {
				t.Fatalf("hauteur #%d : racine d'état divergente au nœud %d", h, i)
			}
		}
	}
	// Finalité : tous d'accord, et finalized = hauteur-1 (4 validateurs en
	// ligne ⇒ 3/4 = 75 % > 2/3 ⇒ chaque bloc finalise son parent).
	for i, e := range net.engines {
		if got := e.FinalizedHeight(); got != H-1 {
			t.Fatalf("nœud %d : finalized=%d, attendu %d", i, got, H-1)
		}
	}
}

// TestLateNodeSyncsFinality : un nœud qui rejoint en rejouant les blocs dans
// l'ordre rattrape la hauteur ET la finalité — la finalité provient des blocs
// eux-mêmes (LastCommit), pas d'un état de votes local.
func TestLateNodeSyncsFinality(t *testing.T) {
	net := newSimNet(t, 4)
	const H = 6
	for h := 1; h <= H; h++ {
		net.step()
	}

	// Nœud tardif : même set de validateurs, mais découvre la chaîne après coup.
	late := state.New()
	late.SetParams(types.DefaultParams())
	for _, k := range net.keys {
		late.BootstrapStake(k.Address(), 1_000_000*types.Unit)
	}
	lateKey, _ := crypto.GenerateKeyPair() // observateur (non-validateur)
	eng := New(late, mempool.New(100), nil, lateKey, time.Hour, 100)
	eng.SetChainID("sim")

	for _, b := range net.chain {
		if err := eng.ApplyExternalBlock(b); err != nil {
			t.Fatalf("synchro bloc #%d : %v", b.Header.Height, err)
		}
	}
	if late.GetHeight() != H {
		t.Fatalf("nœud tardif à la hauteur %d, attendu %d", late.GetHeight(), H)
	}
	if eng.FinalizedHeight() != H-1 {
		t.Fatalf("nœud tardif finalized=%d, attendu %d (finalité issue des blocs)", eng.FinalizedHeight(), H-1)
	}
	if late.GetLastHash() != net.states[0].GetLastHash() {
		t.Fatal("nœud tardif : hash final divergent")
	}
}
