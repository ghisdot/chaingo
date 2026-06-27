package consensus

import (
	"testing"

	"chaingo/internal/types"
)

// TestBuriedForkReorgMultiBlock : REORG MULTI-BLOCS (fork enterré).
//
// Scénario : un nœud minoritaire M, isolé, construit DEUX blocs sur sa branche
// (A@H round 0 puis A@H+1) — son sommet est donc H+1. Pendant ce temps la majorité
// produit un bloc CONCURRENT B@H au round 1 avec une POLKA (≥ 2/3 de prevotes). On
// livre B@H (ENTERRÉ sous le sommet de M) puis la polka à M : voyant une polka pour
// B@H à un round strictement supérieur, M doit REORG ENTERRÉ — rembobiner son
// sommet de H+1 jusqu'au point de fork H-1, abandonner A@H ET A@H+1, et basculer
// sur B@H. Son sommet retombe donc à H.
//
// On recherche un alignement de proposeurs (M proposeur de H ET de H+1 au round 0,
// un autre proposeur au round 1) en repartant de réseaux frais (clés aléatoires).
func TestBuriedForkReorgMultiBlock(t *testing.T) {
	for attempt := 0; attempt < 80; attempt++ {
		net := newSimNet(t, 4)
		net.step()
		net.step() // établit la chaîne (sommet 2, finalité 1)

		H := net.states[0].GetHeight() + 1
		prev := net.states[0].GetLastHash()
		p0 := net.states[0].SelectProposer(H, prev, 0)
		p1 := net.states[0].SelectProposer(H, prev, 1)
		iMin, iP1 := net.indexOf(p0), net.indexOf(p1)
		if iMin < 0 || iP1 < 0 || p0 == p1 {
			continue
		}

		// M produit A@H (round 0), NON diffusé.
		net.blocks, net.votes = nil, nil
		net.forceRound(0)
		aH := net.engines[iMin].ProduceOnce(true)
		if aH == nil || aH.Header.Round != 0 {
			continue
		}
		// M doit aussi être proposeur de H+1 (sur A@H) pour étendre sa branche isolée.
		if net.states[iMin].SelectProposer(H+1, aH.Hash, 0) != p0 {
			continue // pas d'alignement → on retente avec un réseau frais
		}
		net.blocks, net.votes = nil, nil
		net.forceRound(0)
		aH1 := net.engines[iMin].ProduceOnce(true) // A@H+1 sur A@H
		if aH1 == nil || net.states[iMin].GetHeight() != H+1 {
			continue
		}
		net.blocks, net.votes = nil, nil

		// La majorité : P1 produit B@H (round 1), les 2 autres l'appliquent → polka.
		net.forceRound(1)
		bH := net.engines[iP1].ProduceOnce(true)
		if bH == nil || bH.Header.Round != 1 || bH.Hash == aH.Hash {
			continue
		}
		for j := range net.engines {
			if j == iMin || j == iP1 {
				continue
			}
			if err := net.engines[j].ApplyExternalBlock(bH); err != nil {
				continue
			}
		}
		var polka []*types.Vote
		for _, v := range net.votes {
			if v.Kind == types.PrevoteKind && v.BlockHash == bH.Hash && v.Height == H {
				polka = append(polka, v)
			}
		}
		if len(polka) < 3 {
			continue
		}

		// --- Le cœur du test : M (sommet H+1) reçoit B@H ENTERRÉ + la polka. ---
		finalBefore := net.engines[iMin].FinalizedHeight()
		if net.states[iMin].GetHeight() != H+1 {
			t.Fatalf("M devrait être au sommet %d avant reorg", H+1)
		}
		// B@H bufferisé (pas encore de polka chez M) → pas de bascule.
		if err := net.engines[iMin].ApplyExternalBlock(bH); err != nil {
			t.Fatalf("M applique B@H (bufferise) : %v", err)
		}
		if net.states[iMin].GetLastHash() != aH1.Hash {
			t.Fatal("M ne doit pas avoir bougé sans polka livrée")
		}
		// Livraison de la polka → REORG ENTERRÉ attendu.
		for _, pv := range polka {
			net.engines[iMin].AddVote(pv)
		}

		// Vérifications : sommet retombé à H, sur B@H, finalité INCHANGÉE.
		if got := net.states[iMin].GetHeight(); got != H {
			t.Fatalf("reorg enterré attendu : sommet %d → %d, got %d", H+1, H, got)
		}
		if net.states[iMin].GetLastHash() != bH.Hash {
			t.Fatalf("M devrait être sur B@H après le reorg enterré")
		}
		if got := net.engines[iMin].FinalizedHeight(); got != finalBefore {
			t.Fatalf("la finalité ne doit pas bouger sur un reorg (got %d, attendu %d)", got, finalBefore)
		}
		// La méta de la branche abandonnée (H+1) a été purgée.
		eM := net.engines[iMin]
		eM.mu.Lock()
		_, lockedH1 := eM.locked[H+1]
		_, snapH1 := eM.snapshots[H+1]
		eM.mu.Unlock()
		if lockedH1 || snapH1 {
			t.Fatal("la méta de la hauteur abandonnée H+1 (verrou/snapshot) aurait dû être purgée")
		}
		return // succès
	}
	t.Skip("aucun alignement de proposeurs pour le scénario de fork enterré (tirage de clés)")
}

// TestReorgNeverBelowFinality : SÛRETÉ ABSOLUE — un bloc concurrent à une hauteur
// DÉJÀ FINALISÉE est ignoré sans condition (jamais de réécriture de l'histoire
// finalisée), même prétendant un round élevé. C'est la garde la plus critique du
// fork-choice/reorg : la finalité est irréversible.
func TestReorgNeverBelowFinality(t *testing.T) {
	net := newSimNet(t, 4)
	for i := 0; i < 5; i++ {
		net.step()
	}
	e := net.engines[0]
	fin := e.FinalizedHeight()
	if fin < 1 {
		t.Skip("finalité pas encore établie")
	}

	hBefore := net.states[0].GetHeight()
	hashBefore := net.states[0].GetLastHash()
	rootBefore := net.states[0].Root()

	// Bloc concurrent BIDON à une hauteur finalisée, round volontairement élevé.
	bogus := &types.Block{
		Header: types.BlockHeader{Height: fin, Round: 9},
		Hash:   "fork-sous-finalite-doit-etre-ignore",
	}
	if err := e.ApplyExternalBlock(bogus); err != nil {
		t.Fatalf("un bloc à une hauteur finalisée doit être ignoré SANS erreur, got %v", err)
	}

	// L'état (sommet, hash, racine, finalité) doit être STRICTEMENT inchangé.
	if net.states[0].GetHeight() != hBefore ||
		net.states[0].GetLastHash() != hashBefore ||
		net.states[0].Root() != rootBefore {
		t.Fatal("SÛRETÉ : un fork sous la finalité ne doit JAMAIS modifier l'état")
	}
	if e.FinalizedHeight() != fin {
		t.Fatalf("la finalité ne doit pas bouger (got %d, attendu %d)", e.FinalizedHeight(), fin)
	}
}
