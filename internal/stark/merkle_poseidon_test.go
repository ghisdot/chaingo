// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Tests de l'arbre de Merkle algébrique Poseidon (merkle_poseidon.go) :
//   - ROUND-TRIP : pour plusieurs tailles et tous les indices, une ouverture
//     produite par PoseidonOpen est acceptée par PoseidonVerifyPath.
//   - SOUNDNESS / tests NÉGATIFS : feuille falsifiée, frère falsifié, indice
//     erroné, racine falsifiée, chemin tronqué/allongé, mauvaise direction =>
//     REJET (false).
//   - PADDING : une taille non puissance de 2 est correctement engagée (dernière
//     feuille dupliquée) et toutes les feuilles réelles s'ouvrent.
//   - DÉTERMINISME : deux engagements des mêmes feuilles donnent la même racine.
//
// Déterministe : aucun time/math/rand ; PRNG explicite à graine fixe (newPRNG,
// défini dans field_test.go).
package stark

import "testing"

// randDigest fabrique un digest [4]Felt pseudo-aléatoire (PRNG déterministe).
func randDigest(rng *prng) [poseidonDigestLen]Felt {
	var d [poseidonDigestLen]Felt
	for i := range d {
		d[i] = rng.felt()
	}
	return d
}

// randLeaves fabrique n feuilles (digests) pseudo-aléatoires.
func randLeaves(rng *prng, n int) [][poseidonDigestLen]Felt {
	leaves := make([][poseidonDigestLen]Felt, n)
	for i := range leaves {
		leaves[i] = randDigest(rng)
	}
	return leaves
}

// ---------------------------------------------------------------------------
// Round-trip : ouverture/vérification sur plusieurs tailles et tous les indices.
// ---------------------------------------------------------------------------

func TestPoseidonMerkleRoundTrip(t *testing.T) {
	rng := newPRNG(0x1234ABCD)
	// Tailles puissances de 2 ET tailles « biscornues » (padding requis).
	sizes := []int{1, 2, 3, 4, 5, 7, 8, 9, 16, 17, 31, 32, 33}

	for _, n := range sizes {
		leaves := randLeaves(rng, n)
		root, tree := PoseidonCommit(leaves)

		if tree.NumLeaves() != n {
			t.Fatalf("n=%d : NumLeaves=%d attendu %d", n, tree.NumLeaves(), n)
		}
		if tree.Root() != root {
			t.Fatalf("n=%d : Root() != racine renvoyée par Commit", n)
		}

		for idx := 0; idx < n; idx++ {
			path := PoseidonOpen(tree, idx)
			if !PoseidonVerifyPath(root, idx, leaves[idx], path) {
				t.Fatalf("n=%d idx=%d : ouverture valide rejetée (round-trip cassé)", n, idx)
			}
		}
	}
}

// TestPoseidonMerkleArbreUneFeuille : cas dégénéré N=1 (arbre = racine =
// feuille, chemin vide).
func TestPoseidonMerkleArbreUneFeuille(t *testing.T) {
	rng := newPRNG(0xFEED)
	leaves := randLeaves(rng, 1)
	root, tree := PoseidonCommit(leaves)

	path := PoseidonOpen(tree, 0)
	if len(path) != 0 {
		t.Fatalf("N=1 : chemin attendu vide, got len=%d", len(path))
	}
	if root != leaves[0] {
		t.Fatal("N=1 : la racine doit être la feuille elle-même")
	}
	if !PoseidonVerifyPath(root, 0, leaves[0], path) {
		t.Fatal("N=1 : ouverture de l'unique feuille rejetée")
	}
}

// ---------------------------------------------------------------------------
// Déterminisme de l'engagement.
// ---------------------------------------------------------------------------

func TestPoseidonMerkleDeterministe(t *testing.T) {
	rng := newPRNG(0x0DDBEEF)
	leaves := randLeaves(rng, 13)

	r1, _ := PoseidonCommit(leaves)
	r2, _ := PoseidonCommit(leaves)
	if r1 != r2 {
		t.Fatal("PoseidonCommit non déterministe : deux racines différentes pour les mêmes feuilles")
	}

	// La racine doit dépendre du CONTENU : modifier une feuille change la racine.
	mod := make([][poseidonDigestLen]Felt, len(leaves))
	copy(mod, leaves)
	mod[5][1] = mod[5][1].Add(One())
	r3, _ := PoseidonCommit(mod)
	if r1 == r3 {
		t.Fatal("modifier une feuille ne change pas la racine (engagement non liant)")
	}

	// La racine doit dépendre de l'ORDRE : permuter deux feuilles change la racine.
	swapped := make([][poseidonDigestLen]Felt, len(leaves))
	copy(swapped, leaves)
	swapped[2], swapped[9] = swapped[9], swapped[2]
	r4, _ := PoseidonCommit(swapped)
	if r1 == r4 {
		t.Fatal("permuter deux feuilles ne change pas la racine (positions non engagées)")
	}
}

// ---------------------------------------------------------------------------
// SOUNDNESS / tests NÉGATIFS : toute falsification doit être rejetée.
// ---------------------------------------------------------------------------

// TestPoseidonMerkleFeuilleFalsifiee : altérer la feuille présentée (sans
// toucher au chemin ni à la racine) => rejet.
func TestPoseidonMerkleFeuilleFalsifiee(t *testing.T) {
	rng := newPRNG(0xAA11)
	leaves := randLeaves(rng, 12)
	root, tree := PoseidonCommit(leaves)

	for idx := 0; idx < len(leaves); idx++ {
		path := PoseidonOpen(tree, idx)
		bad := leaves[idx]
		bad[0] = bad[0].Add(One()) // +1 sur le premier Felt du digest
		if PoseidonVerifyPath(root, idx, bad, path) {
			t.Fatalf("idx=%d : feuille falsifiée acceptée (soundness cassée)", idx)
		}
	}
}

// TestPoseidonMerkleFrereFalsifie : altérer un élément du chemin
// d'authentification => rejet.
func TestPoseidonMerkleFrereFalsifie(t *testing.T) {
	rng := newPRNG(0xBB22)
	leaves := randLeaves(rng, 16)
	root, tree := PoseidonCommit(leaves)

	idx := 5
	path := PoseidonOpen(tree, idx)
	// Sanity : le chemin original est valide.
	if !PoseidonVerifyPath(root, idx, leaves[idx], path) {
		t.Fatal("préalable : ouverture valide rejetée")
	}

	// On falsifie chaque niveau du chemin, l'un après l'autre.
	for lvl := range path {
		tampered := make([][poseidonDigestLen]Felt, len(path))
		copy(tampered, path)
		tampered[lvl][2] = tampered[lvl][2].Add(One())
		if PoseidonVerifyPath(root, idx, leaves[idx], tampered) {
			t.Fatalf("niveau %d : frère falsifié accepté (soundness cassée)", lvl)
		}
	}
}

// TestPoseidonMerkleIndiceFalsifie : présenter une ouverture valide pour la
// feuille i sous un AUTRE indice j => rejet (soundness positionnelle).
func TestPoseidonMerkleIndiceFalsifie(t *testing.T) {
	rng := newPRNG(0xCC33)
	leaves := randLeaves(rng, 16)
	root, tree := PoseidonCommit(leaves)

	idx := 6
	path := PoseidonOpen(tree, idx)

	for j := 0; j < 16; j++ {
		if j == idx {
			continue // l'indice correct doit, lui, passer
		}
		if PoseidonVerifyPath(root, j, leaves[idx], path) {
			t.Fatalf("ouverture de l'indice %d acceptée sous l'indice %d (positionnel cassé)", idx, j)
		}
	}
	// L'indice correct reste valide.
	if !PoseidonVerifyPath(root, idx, leaves[idx], path) {
		t.Fatal("l'indice correct est rejeté : test incohérent")
	}
}

// TestPoseidonMerkleRacineFalsifiee : vérifier contre une mauvaise racine => rejet.
func TestPoseidonMerkleRacineFalsifiee(t *testing.T) {
	rng := newPRNG(0xDD44)
	leaves := randLeaves(rng, 8)
	root, tree := PoseidonCommit(leaves)

	idx := 3
	path := PoseidonOpen(tree, idx)

	badRoot := root
	badRoot[0] = badRoot[0].Add(One())
	if PoseidonVerifyPath(badRoot, idx, leaves[idx], path) {
		t.Fatal("racine falsifiée acceptée (soundness cassée)")
	}
}

// TestPoseidonMerkleCheminMalforme : un chemin tronqué ou allongé doit être
// rejeté (incohérence hauteur/indice).
func TestPoseidonMerkleCheminMalforme(t *testing.T) {
	rng := newPRNG(0xEE55)
	leaves := randLeaves(rng, 16) // hauteur 4
	root, tree := PoseidonCommit(leaves)

	idx := 10
	path := PoseidonOpen(tree, idx)
	if len(path) != 4 {
		t.Fatalf("préalable : hauteur attendue 4, got %d", len(path))
	}

	// Chemin tronqué (3 niveaux) : index 10 >= 2^3=8 => rejet par la borne, et de
	// toute façon la racine ne peut coïncider.
	if PoseidonVerifyPath(root, idx, leaves[idx], path[:3]) {
		t.Fatal("chemin tronqué accepté")
	}

	// Chemin allongé (5 niveaux) : la racine recalculée diverge => rejet.
	longer := make([][poseidonDigestLen]Felt, 0, 5)
	longer = append(longer, path...)
	longer = append(longer, randDigest(rng))
	if PoseidonVerifyPath(root, idx, leaves[idx], longer) {
		t.Fatal("chemin allongé accepté")
	}
}

// TestPoseidonMerkleMauvaiseDirection : reconstruire en intervertissant la
// direction gauche/droite à un niveau (Hash2 sensible à l'ordre) => rejet. On
// l'éprouve en falsifiant l'indice de telle sorte que le bit de direction d'un
// niveau diffère alors que les frères restent identiques — déjà couvert par
// IndiceFalsifie, mais on vérifie ici explicitement le cas frère commun.
func TestPoseidonMerkleMauvaiseDirection(t *testing.T) {
	rng := newPRNG(0xFF66)
	leaves := randLeaves(rng, 8)
	root, tree := PoseidonCommit(leaves)

	// idx=0 (pair partout) vs idx=1 : le frère du niveau 0 est le même (layers[0][1]
	// est frère de 0, layers[0][0] est frère de 1) mais la direction au niveau 0
	// est inversée. Présenter le chemin de 0 sous l'indice 1 doit échouer car la
	// feuille présentée (leaves[0]) serait combinée à droite au lieu de gauche.
	path0 := PoseidonOpen(tree, 0)
	if PoseidonVerifyPath(root, 1, leaves[0], path0) {
		t.Fatal("chemin de l'indice 0 accepté sous l'indice 1 (direction non liée)")
	}
}

// TestPoseidonMerklePaddingDupliqueDerniere : pour une taille non puissance de
// 2, la feuille de padding est bien la dernière feuille réelle dupliquée, et
// ce padding n'est PAS ouvrable (index >= numLeaves panique côté Open ; ici on
// vérifie surtout que toutes les feuilles RÉELLES s'ouvrent).
func TestPoseidonMerklePaddingDupliqueDerniere(t *testing.T) {
	rng := newPRNG(0x9988)
	// 5 feuilles => padding à 8 : indices 5,6,7 = copie de la feuille 4.
	leaves := randLeaves(rng, 5)
	root, tree := PoseidonCommit(leaves)

	for idx := 0; idx < 5; idx++ {
		path := PoseidonOpen(tree, idx)
		if !PoseidonVerifyPath(root, idx, leaves[idx], path) {
			t.Fatalf("idx=%d : feuille réelle non ouvrable après padding", idx)
		}
	}

	// Vérifie le padding au niveau 0 de l'arbre : layers[0][5..7] == layers[0][4].
	lvl0 := tree.layers[0]
	if len(lvl0) != 8 {
		t.Fatalf("niveau 0 attendu de taille 8, got %d", len(lvl0))
	}
	for i := 5; i < 8; i++ {
		if lvl0[i] != lvl0[4] {
			t.Fatalf("feuille de padding %d != dernière feuille réelle", i)
		}
	}
}

// TestPoseidonMerkleOpenHorsBornesPanique : ouvrir un indice de padding ou hors
// bornes doit paniquer (erreur d'appelant), pas renvoyer un chemin silencieux.
func TestPoseidonMerkleOpenHorsBornesPanique(t *testing.T) {
	rng := newPRNG(0x7766)
	leaves := randLeaves(rng, 5)
	_, tree := PoseidonCommit(leaves)

	for _, badIdx := range []int{-1, 5, 6, 100} {
		func(i int) {
			defer func() {
				if recover() == nil {
					t.Fatalf("PoseidonOpen(idx=%d) aurait dû paniquer", i)
				}
			}()
			_ = PoseidonOpen(tree, i)
		}(badIdx)
	}
}

// TestPoseidonCommitVidePanique : engager zéro feuille doit paniquer.
func TestPoseidonCommitVidePanique(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("PoseidonCommit(nil) aurait dû paniquer")
		}
	}()
	_, _ = PoseidonCommit(nil)
}

// TestPoseidonVerifyIndiceNegatif : un indice négatif est rejeté sans panique.
func TestPoseidonVerifyIndiceNegatif(t *testing.T) {
	rng := newPRNG(0x4455)
	leaves := randLeaves(rng, 4)
	root, tree := PoseidonCommit(leaves)
	path := PoseidonOpen(tree, 0)
	if PoseidonVerifyPath(root, -1, leaves[0], path) {
		t.Fatal("indice négatif accepté")
	}
}
