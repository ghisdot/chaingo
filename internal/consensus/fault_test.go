package consensus

import (
	"testing"
	"time"
)

// assertConvergedAmong : les nœuds d'indices `idx` partagent hauteur, dernier
// hash et racine d'état.
func assertConvergedAmong(t *testing.T, s *simNet, idx []int, ctx string) {
	t.Helper()
	h0 := s.states[idx[0]].GetHeight()
	hash0 := s.states[idx[0]].GetLastHash()
	root0 := s.states[idx[0]].Root()
	for _, i := range idx {
		if s.states[i].GetHeight() != h0 {
			t.Fatalf("%s : nœud %d à la hauteur %d, attendu %d", ctx, i, s.states[i].GetHeight(), h0)
		}
		if s.states[i].GetLastHash() != hash0 {
			t.Fatalf("%s : hash divergent au nœud %d", ctx, i)
		}
		if s.states[i].Root() != root0 {
			t.Fatalf("%s : racine d'état divergente au nœud %d", ctx, i)
		}
	}
}

// deliverOnlineOnly : comme deliver(), mais ne distribue blocs+votes qu'aux nœuds
// EN LIGNE. Un nœud offline ne reçoit rien (il ne vote donc pas) — modèle d'une
// vraie panne / partition, plus fort que stepWithOffline (qui ne fait que sauter
// le rôle de proposeur).
func (s *simNet) deliverOnlineOnly(offline map[int]bool) {
	for len(s.blocks) > 0 || len(s.votes) > 0 {
		bs := s.blocks
		s.blocks = nil
		for _, b := range bs {
			for i, e := range s.engines {
				if offline[i] {
					continue
				}
				e.ApplyExternalBlock(b)
			}
		}
		vs := s.votes
		s.votes = nil
		for _, v := range vs {
			for i, e := range s.engines {
				if offline[i] {
					continue
				}
				e.AddVote(v)
			}
		}
	}
}

// stepExcept : produit une hauteur via le 1er proposeur EN LIGNE (round de
// secours déterministe) et ne distribue qu'aux nœuds en ligne. Renvoie false si
// aucun proposeur en ligne n'existe dans [1, MaxRounds).
func (s *simNet) stepExcept(offline map[int]bool) bool {
	// Référence = premier nœud EN LIGNE : states[0] peut être offline (donc figé à
	// une hauteur passée), ce qui fausserait le calcul de la hauteur cible.
	ref := -1
	for i := range s.engines {
		if !offline[i] {
			ref = i
			break
		}
	}
	if ref < 0 {
		return false
	}
	height := s.states[ref].GetHeight() + 1
	prev := s.states[ref].GetLastHash()
	for r := uint32(1); r < MaxRounds; r++ {
		a := s.states[ref].SelectProposer(height, prev, r)
		idx := s.indexOf(a)
		if idx < 0 || offline[idx] {
			continue
		}
		dur := time.Duration(r)*time.Hour + 100*time.Millisecond
		for i, e := range s.engines {
			if offline[i] {
				continue
			}
			e.mu.Lock()
			e.lastProgress = time.Now().Add(-dur)
			e.mu.Unlock()
		}
		s.engines[idx].ProduceOnce(true)
		s.deliverOnlineOnly(offline)
		return true
	}
	return false
}

// TestSustainedFallbackLivenessAndFinality : un proposeur récurrent hors-ligne
// (son créneau est sauté à CHAQUE hauteur) ne casse ni la liveness ni la finalité
// — les 4 nœuds (qui votent tous) convergent et finalisent sur plusieurs hauteurs
// d'affilée via des rounds de secours. Vérifie l'absence de dérive d'état sur des
// rounds de secours RÉPÉTÉS.
func TestSustainedFallbackLivenessAndFinality(t *testing.T) {
	net := newSimNet(t, 4)
	net.step() // hauteur 1 établie (round 0)

	offline := map[int]bool{0: true} // le nœud 0 ne PROPOSE jamais (mais vote)
	all := []int{0, 1, 2, 3}
	const rounds = 5
	startH := net.states[0].GetHeight()

	for k := 0; k < rounds; k++ {
		r, ok := net.stepWithOffline(offline)
		if !ok {
			t.Fatalf("itération %d : aucun round de secours disponible", k)
		}
		if r == 0 {
			t.Fatalf("itération %d : round de secours attendu (>0)", k)
		}
		assertConvergedAmong(t, net, all, "sustained-fallback")
	}

	if got := net.states[0].GetHeight(); got != startH+rounds {
		t.Fatalf("hauteur attendue %d, got %d", startH+rounds, got)
	}
	// Les 4 nœuds votent → 4/4 > 2/3 ⇒ la finalité avance (finalized = hauteur-1).
	wantFinal := net.states[0].GetHeight() - 1
	for i, e := range net.engines {
		if got := e.FinalizedHeight(); got != wantFinal {
			t.Fatalf("nœud %d : finalized=%d, attendu %d (finalité doit suivre)", i, got, wantFinal)
		}
	}
}

// TestNoFinalityWithoutQuorum : avec 2 validateurs sur 4 RÉELLEMENT hors-ligne
// (ne reçoivent ni ne votent), les 2 nœuds en ligne ne réunissent que 50 % du
// pouvoir (< 2/3). SÛRETÉ : la hauteur peut avancer entre eux, mais la FINALITÉ
// N'AVANCE PAS (aucun bloc n'est finalisé sans quorum), et les 2 en ligne ne
// divergent jamais (pas de fork).
func TestNoFinalityWithoutQuorum(t *testing.T) {
	net := newSimNet(t, 4)
	net.step()
	net.step() // établit + finalise quelques hauteurs (4/4 votent)

	offline := map[int]bool{0: true, 1: true} // 2/4 hors-ligne ⇒ 50 % < 2/3
	online := []int{2, 3}

	// 2 hauteurs en partition : draine la finalité résiduelle des votes PRÉ-partition
	// (le 1er bloc de partition peut encore finaliser la dernière hauteur à quorum).
	for k := 0; k < 2; k++ {
		if !net.stepExcept(offline) {
			t.Fatalf("setup partition %d : aucun proposeur en ligne", k)
		}
	}
	finalMid := net.engines[2].FinalizedHeight()
	heightMid := net.states[2].GetHeight()

	// 3 hauteurs SUPPLÉMENTAIRES, toutes produites/votées par 2/4 seulement.
	for k := 0; k < 3; k++ {
		if !net.stepExcept(offline) {
			t.Fatalf("partition %d : aucun proposeur en ligne", k)
		}
		assertConvergedAmong(t, net, online, "no-quorum")
	}

	// SÛRETÉ/LIVENESS : la hauteur avance (les 2 en ligne produisent)…
	if net.states[2].GetHeight() <= heightMid {
		t.Fatalf("la hauteur aurait dû avancer (got %d, mid %d)", net.states[2].GetHeight(), heightMid)
	}
	// …mais la FINALITÉ N'AVANCE PLUS (commits à 2/4 = 50 % < 2/3).
	for _, i := range online {
		if got := net.engines[i].FinalizedHeight(); got != finalMid {
			t.Fatalf("nœud %d : finalized=%d a avancé sans quorum (attendu %d, stable)", i, got, finalMid)
		}
	}
}
