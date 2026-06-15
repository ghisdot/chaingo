package consensus

import (
	"testing"
	"time"

	"chaingo/internal/types"
)

// forceRound positionne lastProgress de tous les moteurs pour que ProduceOnce
// calcule le round `r` (interval = 1h dans les tests).
func (s *simNet) forceRound(r uint32) {
	dur := time.Duration(r)*time.Hour + 100*time.Millisecond
	for _, e := range s.engines {
		e.mu.Lock()
		e.lastProgress = time.Now().Add(-dur)
		e.mu.Unlock()
	}
}

// TestForkChoiceConvergesViaHigherRoundPolka : LE test de partition (#7).
//
// Scénario : à la hauteur H, le nœud minoritaire M (le proposeur du round 0)
// produit et commit B0 (round 0), mais B0 ne parvient pas aux 3 autres. Ceux-ci
// basculent au round 1 et commit B1 (round 1) → 3/4 de prevotes = POLKA pour B1.
// On livre alors B1 + les prevotes à M : M est verrouillé sur B0 (round 0) mais,
// voyant une polka pour B1 à un round SUPÉRIEUR, il doit REORG vers B1.
// Résultat attendu : convergence des 4 nœuds sur B1, sans double-finalité.
func TestForkChoiceConvergesViaHigherRoundPolka(t *testing.T) {
	net := newSimNet(t, 4)

	// Établit la chaîne et trouve une hauteur où les proposeurs round 0 et
	// round 1 DIFFÈRENT (sinon pas de blocs concurrents distincts).
	var H uint64
	var prev string
	for {
		net.step()
		H = net.states[0].GetHeight() + 1
		prev = net.states[0].GetLastHash()
		p0 := net.states[0].SelectProposer(H, prev, 0)
		p1 := net.states[0].SelectProposer(H, prev, 1)
		if p0 != p1 && net.indexOf(p0) >= 0 && net.indexOf(p1) >= 0 {
			break
		}
		if H > 30 {
			t.Skip("pas de hauteur avec proposeurs round0 != round1 (tirage de clés)")
		}
	}
	iMin := net.indexOf(net.states[0].SelectProposer(H, prev, 0)) // minoritaire (round 0)
	iP1 := net.indexOf(net.states[0].SelectProposer(H, prev, 1))  // proposeur round 1

	// 1) M produit B0 (round 0) et le commit localement. On ne le diffuse PAS.
	net.blocks, net.votes = nil, nil
	net.forceRound(0)
	b0 := net.engines[iMin].ProduceOnce(true)
	if b0 == nil || b0.Header.Round != 0 {
		t.Fatalf("B0 round 0 attendu, got %v", b0)
	}
	if net.states[iMin].GetLastHash() != b0.Hash {
		t.Fatal("M devrait être sur B0")
	}
	net.blocks, net.votes = nil, nil // jette les messages de B0 (non diffusé)

	// 2) Le proposeur du round 1 produit B1 (round 1) et le commit.
	net.forceRound(1)
	b1 := net.engines[iP1].ProduceOnce(true)
	if b1 == nil || b1.Header.Round != 1 {
		t.Fatalf("B1 round 1 attendu, got %v", b1)
	}
	if b1.Hash == b0.Hash {
		t.Fatal("B0 et B1 devraient être des blocs distincts")
	}

	// 3) Les 2 autres nœuds majoritaires (ni M ni P1) appliquent B1.
	for j := range net.engines {
		if j == iMin || j == iP1 {
			continue
		}
		if err := net.engines[j].ApplyExternalBlock(b1); err != nil {
			t.Fatalf("nœud %d applique B1 : %v", j, err)
		}
	}

	// 4) Collecte les PREVOTES pour B1 (la polka : 3 validateurs majoritaires).
	var b1Prevotes []*types.Vote
	for _, v := range net.votes {
		if v.Kind == types.PrevoteKind && v.BlockHash == b1.Hash && v.Height == H {
			b1Prevotes = append(b1Prevotes, v)
		}
	}
	if len(b1Prevotes) < 3 {
		t.Fatalf("polka attendue (≥3 prevotes pour B1), got %d", len(b1Prevotes))
	}

	// 5) M reçoit B1 (bufferisé, pas encore de polka chez lui) puis les prevotes.
	if err := net.engines[iMin].ApplyExternalBlock(b1); err != nil {
		t.Fatalf("M applique B1 (bufferise) : %v", err)
	}
	if net.states[iMin].GetLastHash() != b0.Hash {
		t.Fatal("M ne doit pas encore avoir basculé (pas de polka livrée)")
	}
	for _, pv := range b1Prevotes {
		net.engines[iMin].AddVote(pv) // le 3e complète la polka → reorg
	}

	// 6) M a basculé vers B1.
	if net.states[iMin].GetLastHash() != b1.Hash {
		t.Fatalf("M aurait dû reorg vers B1 : lasthash=%s, B1=%s", net.states[iMin].GetLastHash(), b1.Hash)
	}
	if net.states[iMin].GetHeight() != H {
		t.Fatalf("M devrait être à la hauteur %d, got %d", H, net.states[iMin].GetHeight())
	}

	// 7) Les 4 nœuds convergent sur B1 (même hash, même racine d'état).
	root := net.states[iMin].Root()
	for j, st := range net.states {
		if j == iP1 {
			continue // P1 a produit B1, déjà dessus (cohérent)
		}
		if j != iMin {
			// nœuds majoritaires
			if st.GetLastHash() != b1.Hash {
				t.Fatalf("nœud majoritaire %d pas sur B1", j)
			}
		}
		if st.GetLastHash() == b1.Hash && st.Root() != root {
			t.Fatalf("nœud %d : racine d'état divergente après convergence", j)
		}
	}
}

// TestForkChoiceIgnoresLowerRound : un bloc concurrent à un round INFÉRIEUR ou
// égal (ou sans polka) ne déclenche PAS de reorg — on reste fidèle à notre bloc.
func TestForkChoiceIgnoresLowerRound(t *testing.T) {
	net := newSimNet(t, 4)
	for {
		net.step()
		H := net.states[0].GetHeight() + 1
		prev := net.states[0].GetLastHash()
		if net.states[0].SelectProposer(H, prev, 0) != net.states[0].SelectProposer(H, prev, 1) {
			break
		}
		if net.states[0].GetHeight() > 30 {
			t.Skip("tirage de clés défavorable")
		}
	}
	H := net.states[0].GetHeight() + 1
	prev := net.states[0].GetLastHash()
	iMin := net.indexOf(net.states[0].SelectProposer(H, prev, 0))
	iP1 := net.indexOf(net.states[0].SelectProposer(H, prev, 1))

	// Cette fois c'est P1 (round 1) qui est minoritaire et produit B1 ; M (round
	// 0) et les autres sont sur B0. Un bloc round 1 SANS polka livrée à eux ne
	// doit pas les faire basculer.
	net.blocks, net.votes = nil, nil
	net.forceRound(0)
	b0 := net.engines[iMin].ProduceOnce(true)
	// Les 2 autres (ni M ni P1) appliquent B0 → branche majoritaire B0.
	for j := range net.engines {
		if j == iMin || j == iP1 {
			continue
		}
		net.engines[j].ApplyExternalBlock(b0)
	}
	net.votes = nil

	net.forceRound(1)
	b1 := net.engines[iP1].ProduceOnce(true) // P1 sur B1 (round 1), isolé

	// On livre B1 à un nœud majoritaire (sur B0) SANS aucune polka pour B1.
	var maj int = -1
	for j := range net.engines {
		if j != iMin && j != iP1 {
			maj = j
			break
		}
	}
	before := net.states[maj].GetLastHash()
	net.engines[maj].ApplyExternalBlock(b1)
	if net.states[maj].GetLastHash() != before {
		t.Fatal("sans polka, un bloc round 1 ne doit PAS déclencher de reorg")
	}
	_ = b1
}
