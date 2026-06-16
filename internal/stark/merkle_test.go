// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Tests de l'arbre de Merkle : aller-retour d'ouverture sur plusieurs indices
// et plusieurs tailles (dont des comptes non puissances de 2), déterminisme du
// hachage, et tests de SOUNDNESS négatifs — un chemin falsifié, un mauvais
// indice, une feuille altérée, une racine fausse ou une longueur de chemin
// incohérente DOIVENT tous être rejetés. Déterministe : PRNG à graine fixe.
package stark

import (
	"testing"
)

// makeLeaves fabrique `count` feuilles déterministes de `width` Felt chacune.
func makeLeaves(rng *prng, count, width int) [][]Felt {
	leaves := make([][]Felt, count)
	for i := range leaves {
		leaf := make([]Felt, width)
		for j := range leaf {
			leaf[j] = rng.felt()
		}
		leaves[i] = leaf
	}
	return leaves
}

// TestMerkleAllerRetour : pour plusieurs tailles (puissances de 2 ET comptes
// quelconques) et plusieurs largeurs de feuille, chaque feuille réelle s'ouvre
// et se vérifie contre la racine.
func TestMerkleAllerRetour(t *testing.T) {
	rng := newPRNG(300)
	for _, count := range []int{1, 2, 3, 4, 5, 7, 8, 9, 16, 31, 64, 100} {
		for _, width := range []int{1, 2, 4} {
			leaves := makeLeaves(rng, count, width)
			root, tree := Commit(leaves)

			if tree.NumLeaves() != count {
				t.Fatalf("NumLeaves=%d, attendu %d", tree.NumLeaves(), count)
			}
			if tree.Root() != root {
				t.Fatalf("tree.Root() != racine renvoyée par Commit (count=%d)", count)
			}

			for idx := 0; idx < count; idx++ {
				path := Open(tree, idx)
				if !VerifyPath(root, idx, leaves[idx], path) {
					t.Fatalf("ouverture invalide count=%d width=%d idx=%d", count, width, idx)
				}
			}
		}
	}
}

// TestMerkleLongueurChemin : la longueur du chemin doit être log2(nextPow2(N)).
func TestMerkleLongueurChemin(t *testing.T) {
	rng := newPRNG(301)
	cases := map[int]int{1: 0, 2: 1, 3: 2, 4: 2, 5: 3, 8: 3, 9: 4, 16: 4, 17: 5}
	for count, wantLen := range cases {
		leaves := makeLeaves(rng, count, 2)
		_, tree := Commit(leaves)
		path := Open(tree, 0)
		if len(path) != wantLen {
			t.Fatalf("count=%d : longueur chemin=%d, attendu %d", count, len(path), wantLen)
		}
	}
}

// TestMerkleDéterminisme : engager deux fois les mêmes feuilles donne la même
// racine, bit-à-bit. (Exigence de reproductibilité du prouveur/vérifieur.)
func TestMerkleDéterminisme(t *testing.T) {
	leaves := makeLeaves(newPRNG(302), 37, 3)
	// Deux jeux IDENTIQUES (mêmes valeurs) construits séparément.
	leavesBis := makeLeaves(newPRNG(302), 37, 3)

	root1, _ := Commit(leaves)
	root2, _ := Commit(leavesBis)
	if root1 != root2 {
		t.Fatal("Commit non déterministe : deux racines différentes pour le même contenu")
	}
}

// TestMerkleSéparationDomaine : une feuille dont le contenu, hachée, coïnciderait
// avec un nœud ne doit pas pouvoir être confondue. On vérifie au minimum que les
// étiquettes de domaine diffèrent et que hashLeaf != hashNode pour des entrées
// de tailles compatibles.
func TestMerkleSéparationDomaine(t *testing.T) {
	if domainLeaf == domainNode {
		t.Fatal("les étiquettes de domaine feuille/nœud doivent différer")
	}
	// hashLeaf d'une feuille vide vs hashNode de deux zéros : doivent différer
	// uniquement grâce au préfixe de domaine.
	var z [32]byte
	hl := hashLeaf(nil)
	hn := hashNode(z, z)
	if hl == hn {
		t.Fatal("séparation de domaine inefficace : hashLeaf == hashNode")
	}
}

// ---------------------------------------------------------------------------
// SOUNDNESS négatif : toute ouverture falsifiée DOIT être rejetée.
// ---------------------------------------------------------------------------

// TestSoundnessCheminFalsifié : altérer un seul hash du chemin fait échouer la
// vérification.
func TestSoundnessCheminFalsifié(t *testing.T) {
	rng := newPRNG(400)
	leaves := makeLeaves(rng, 16, 2)
	root, tree := Commit(leaves)

	idx := 5
	path := Open(tree, idx)
	// Vérification de contrôle : le chemin honnête passe.
	if !VerifyPath(root, idx, leaves[idx], path) {
		t.Fatal("le chemin honnête aurait dû passer")
	}

	for k := range path {
		forged := make([][32]byte, len(path))
		copy(forged, path)
		forged[k][0] ^= 0xFF // corruption d'un octet d'un frère
		if VerifyPath(root, idx, leaves[idx], forged) {
			t.Fatalf("SOUNDNESS : chemin falsifié au niveau %d accepté", k)
		}
	}
}

// TestSoundnessMauvaisIndice : un chemin valide pour l'indice i ne doit PAS
// vérifier pour un autre indice j != i (sauf collision improbable de positions
// dans l'arbre — exclue ici).
func TestSoundnessMauvaisIndice(t *testing.T) {
	rng := newPRNG(401)
	leaves := makeLeaves(rng, 16, 2)
	root, tree := Commit(leaves)

	idx := 5
	path := Open(tree, idx)
	for j := 0; j < 16; j++ {
		if j == idx {
			continue
		}
		// Avec la VRAIE feuille de idx mais un MAUVAIS indice annoncé : la
		// direction de combinaison change => racine différente.
		if VerifyPath(root, j, leaves[idx], path) {
			t.Fatalf("SOUNDNESS : chemin de idx=%d accepté pour indice annoncé j=%d", idx, j)
		}
	}
}

// TestSoundnessFeuilleAltérée : changer le contenu de la feuille (même indice,
// même chemin) doit faire échouer la vérification.
func TestSoundnessFeuilleAltérée(t *testing.T) {
	rng := newPRNG(402)
	leaves := makeLeaves(rng, 32, 3)
	root, tree := Commit(leaves)

	idx := 11
	path := Open(tree, idx)

	tampered := clone(leaves[idx])
	tampered[0] = tampered[0].Add(One()) // +1 sur un Felt de la feuille
	if VerifyPath(root, idx, tampered, path) {
		t.Fatal("SOUNDNESS : feuille altérée acceptée")
	}

	// Une feuille de largeur différente (préimage tronquée) doit aussi échouer.
	if VerifyPath(root, idx, leaves[idx][:1], path) {
		t.Fatal("SOUNDNESS : feuille tronquée acceptée")
	}
}

// TestSoundnessMauvaiseRacine : vérifier contre une racine corrompue échoue.
func TestSoundnessMauvaiseRacine(t *testing.T) {
	rng := newPRNG(403)
	leaves := makeLeaves(rng, 8, 2)
	root, tree := Commit(leaves)

	idx := 3
	path := Open(tree, idx)

	badRoot := root
	badRoot[0] ^= 0x01
	if VerifyPath(badRoot, idx, leaves[idx], path) {
		t.Fatal("SOUNDNESS : racine corrompue acceptée")
	}
}

// TestSoundnessLongueurCheminIncohérente : un chemin trop court ou trop long
// (indice ne tenant pas dans 2^len, ou hauteur fausse) doit être rejeté.
func TestSoundnessLongueurCheminIncohérente(t *testing.T) {
	rng := newPRNG(404)
	leaves := makeLeaves(rng, 16, 2) // arbre de hauteur 4
	root, tree := Commit(leaves)

	idx := 6
	path := Open(tree, idx) // longueur 4

	// Chemin tronqué : l'indice 6 ne tient pas dans 2^3=8 ? si — mais la racine
	// recalculée à partir de 3 niveaux ne sera pas la vraie racine.
	short := path[:len(path)-1]
	if VerifyPath(root, idx, leaves[idx], short) {
		t.Fatal("SOUNDNESS : chemin tronqué accepté")
	}

	// Chemin rallongé : on ajoute un frère bidon ; idx tient dans 2^5 mais la
	// racine recalculée diffère.
	long := append(clone32(path), [32]byte{})
	if VerifyPath(root, idx, leaves[idx], long) {
		t.Fatal("SOUNDNESS : chemin rallongé accepté")
	}

	// Indice négatif : rejet immédiat.
	if VerifyPath(root, -1, leaves[idx], path) {
		t.Fatal("SOUNDNESS : indice négatif accepté")
	}

	// Indice hors de 2^len(path) : rejet immédiat.
	if VerifyPath(root, 1<<uint(len(path)), leaves[idx], path) {
		t.Fatal("SOUNDNESS : indice hors borne accepté")
	}
}

// TestOpenIndexHorsBornesPanique : ouvrir un indice de padding ou inexistant
// panique (erreur d'appelant).
func TestOpenIndexHorsBornesPanique(t *testing.T) {
	leaves := makeLeaves(newPRNG(405), 5, 2) // padding jusqu'à 8
	_, tree := Commit(leaves)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Open sur indice 5 (padding) aurait dû paniquer")
		}
	}()
	Open(tree, 5)
}

// TestCommitVidePanique : engager zéro feuille panique.
func TestCommitVidePanique(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Commit([]) aurait dû paniquer")
		}
	}()
	Commit(nil)
}

// clone32 duplique un chemin de hash (les tests modifient des copies).
func clone32(p [][32]byte) [][32]byte {
	c := make([][32]byte, len(p))
	copy(c, p)
	return c
}
