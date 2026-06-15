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

// stepWithOffline : seuls les nœuds dont l'index N'EST PAS dans `offline`
// produisent. Le round de secours est forcé en mettant `lastProgress` loin
// dans le passé pour TOUS les moteurs (ils accepteront donc un round > 0).
func (s *simNet) stepWithOffline(offline map[int]bool) {
	long := time.Now().Add(-24 * time.Hour)
	for _, e := range s.engines {
		e.mu.Lock()
		e.lastProgress = long
		e.mu.Unlock()
	}
	for i, e := range s.engines {
		if offline[i] {
			continue
		}
		e.ProduceOnce(true)
	}
	s.deliver()
}

// indexOf : index du nœud dont la clé correspond à l'adresse.
func (s *simNet) indexOf(addr string) int {
	for i, k := range s.keys {
		if k.Address() == addr {
			return i
		}
	}
	return -1
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

// TestFallbackRoundOnOfflineProposer : le proposeur élu au round 0 est
// offline → un autre validateur prend la main au round suivant, la chaîne
// avance malgré tout. Bloc valide chez tous les nœuds en ligne.
func TestFallbackRoundOnOfflineProposer(t *testing.T) {
	net := newSimNet(t, 4)
	// Avance la chaîne d'un bloc pour s'établir.
	net.step()
	if h := net.states[0].GetHeight(); h != 1 {
		t.Fatalf("setup : hauteur 1 attendue, got %d", h)
	}

	// Qui serait le proposeur du round 0 pour la prochaine hauteur ?
	prev := net.states[0].GetLastHash()
	r0 := net.states[0].SelectProposer(2, prev, 0)
	off := net.indexOf(r0)
	if off < 0 {
		t.Fatalf("proposeur round 0 introuvable parmi les nœuds : %s", r0)
	}

	// On le coupe et on déclenche un round de secours.
	net.stepWithOffline(map[int]bool{off: true})

	// Tous les nœuds en ligne sont à la hauteur 2 et d'accord.
	h0 := net.states[0].GetHeight()
	if h0 != 2 {
		t.Fatalf("après round de secours : hauteur 2 attendue, got %d", h0)
	}
	hash0 := net.states[0].GetLastHash()
	for i, st := range net.states {
		if i == off {
			continue
		}
		if st.GetHeight() != h0 || st.GetLastHash() != hash0 {
			t.Fatalf("nœud %d divergent : h=%d hash=%s", i, st.GetHeight(), st.GetLastHash())
		}
	}
	// Le bloc produit a un round > 0.
	if net.chain[len(net.chain)-1].Header.Round == 0 {
		t.Fatal("le bloc de secours devrait avoir un round > 0")
	}
}

// TestEquivocationGetsSlashedThroughNetwork : un validateur émet deux votes
// conflictuels (équivocation manuelle hors du castVote du moteur) reçus par
// le réseau → un autre nœud les transforme en preuve, l'inclut dans son bloc,
// et le slashing s'applique chez tous les nœuds qui appliquent ce bloc.
func TestEquivocationGetsSlashedThroughNetwork(t *testing.T) {
	net := newSimNet(t, 4)
	const offender = 1

	// Avance d'un bloc pour avoir un prev valide.
	net.step()
	prev := net.states[0].GetLastHash()
	stakeBefore := net.states[0].PowerOf(net.keys[offender].Address())

	// Deux précommits conflictuels signés par offender à la hauteur 2.
	mkVote := func(hash string) *types.Vote {
		v := &types.Vote{ChainID: "sim", Height: 2, Kind: types.PrecommitKind, BlockHash: hash}
		v.SignWith(net.keys[offender])
		return v
	}
	vA := mkVote("FAKE_HASH_A_for_h2_" + prev[:8])
	vB := mkVote("FAKE_HASH_B_for_h2_" + prev[:8])

	// Tous les autres nœuds reçoivent A puis B → ils détectent l'équivocation
	// et préparent la preuve pour leur prochain bloc proposé.
	for i, e := range net.engines {
		if i == offender {
			continue
		}
		e.AddVote(vA)
		e.AddVote(vB)
	}

	// Produire des blocs jusqu'à ce que la preuve soit incluse (le proposeur
	// élu doit ne PAS être l'offender pour qu'elle soit drainée dans son bloc).
	included := false
	for try := 0; try < 10 && !included; try++ {
		net.step()
		for _, b := range net.chain {
			for _, ev := range b.Evidence {
				if ev.Voter == net.keys[offender].Address() && ev.Height == 2 {
					included = true
					break
				}
			}
			if included {
				break
			}
		}
	}
	if !included {
		t.Fatal("la preuve d'équivocation n'a jamais été incluse dans un bloc")
	}

	// Slash appliqué : stake de l'offender réduit (5 % par défaut) chez tous
	// les nœuds qui ont appliqué le bloc avec la preuve.
	stakeAfter := net.states[0].PowerOf(net.keys[offender].Address())
	if stakeAfter >= stakeBefore {
		t.Fatalf("le slash devrait avoir réduit le stake de l'offender : avant=%d après=%d",
			stakeBefore, stakeAfter)
	}
	// IsSlashed = true partout.
	for i, st := range net.states {
		if !st.IsSlashed(net.keys[offender].Address(), 2) {
			t.Fatalf("nœud %d : équivocation devrait être marquée slashée", i)
		}
	}
}
