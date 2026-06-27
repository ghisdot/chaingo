// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Tests du circuit d'appartenance Merkle Poseidon (membership_air.go).
//
// Objectifs (du plus important au moins) :
//
//  1. COHÉRENCE — l'IMPÉRATIF : la racine prouvée par ProveMembership / la trace
//     est EXACTEMENT celle calculée par merkle_poseidon.go (PoseidonCommit /
//     PoseidonVerifyPath) pour le même (feuille, chemin). Si l'AIR ne calcule pas
//     la même chaîne Hash2, ce test échoue.
//  2. POSITIF : une preuve d'appartenance honnête vérifie ; déterminisme.
//  3. NÉGATIFS : feuille hors-arbre, mauvaise racine, chemin incohérent (sibling
//     ou bit faux), bit non binaire, trace falsifiée, ouverture / OOD falsifiées
//     => REJET.
//
// COÛT : une preuve STARK d'appartenance (n=256, W=32, bigN=32768) prend ~55 s.
// On MINIMISE donc le nombre d'appels au prouveur :
//   - les vérifications de racine (cohérence, feuille hors-arbre, sibling/bit
//     faux) utilisent buildMembershipTrace (reconstruction NATIVE, millisecondes,
//     SANS STARK) — c'est là que se prouve la cohérence avec merkle_poseidon.go ;
//   - une UNIQUE preuve honnête partagée (sync.Once) sert au test positif et à
//     tous les rejets « racine publique fausse / ouverture / OOD falsifiée » ;
//   - seuls les rejets exigeant une TRACE falsifiée (bit non binaire, état
//     intermédiaire corrompu) lancent un prouveur dédié.
//
// Aléa de test : déterministe (flux SHAKE256 du paquet, sans time ni math/rand).
package stark

import (
	"sync"
	"testing"
)

// memRandomDigest produit un digest [4]Felt DÉTERMINISTE depuis une étiquette.
func memRandomDigest(label string) [poseidonDigestLen]Felt {
	xof := newXOF("test/membership/" + label)
	var d [poseidonDigestLen]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		d[k] = nextFelt(xof)
	}
	return d
}

// memBuildTree construit un arbre Poseidon de 2^memDepth feuilles déterministes
// (étiquetées par leur indice) et renvoie l'arbre + les feuilles.
func memBuildTree(seed string) (*PoseidonMerkleTree, [][poseidonDigestLen]Felt) {
	numLeaves := 1 << memDepth // 256
	leaves := make([][poseidonDigestLen]Felt, numLeaves)
	for i := 0; i < numLeaves; i++ {
		leaves[i] = memRandomDigest(seed + "/" + itoaU64(uint64(i)))
	}
	_, tree := PoseidonCommit(leaves)
	return tree, leaves
}

// memPathFromIndex convertit l'ouverture PoseidonOpen (siblings du bas vers le
// haut) et l'indice de la feuille en MembershipPath (siblings + bits). Le bit du
// niveau i est le LSB de l'indice à ce niveau — MÊME convention que
// PoseidonVerifyPath (et donc que memLevelOutput).
func memPathFromIndex(tree *PoseidonMerkleTree, index int) MembershipPath {
	sibs := PoseidonOpen(tree, index)
	if len(sibs) != memDepth {
		panic("test: profondeur d'ouverture inattendue")
	}
	var path MembershipPath
	idx := index
	for i := 0; i < memDepth; i++ {
		path.Siblings[i] = sibs[i]
		if idx&1 == 0 {
			path.Bits[i] = Zero()
		} else {
			path.Bits[i] = One()
		}
		idx >>= 1
	}
	return path
}

// ---------------------------------------------------------------------------
// Preuve honnête partagée (une seule preuve STARK pour tous les tests qui n'ont
// pas besoin d'une trace falsifiée). Construite paresseusement une fois.
// ---------------------------------------------------------------------------

var (
	memShareOnce  sync.Once
	memShareTree  *PoseidonMerkleTree
	memShareIndex = 42
	memShareLeaf  [poseidonDigestLen]Felt
	memSharePath  MembershipPath
	memShareRoot  [poseidonDigestLen]Felt
	memShareProof AirProof
)

func memShared() (root [poseidonDigestLen]Felt, proof AirProof) {
	memShareOnce.Do(func() {
		var leaves [][poseidonDigestLen]Felt
		memShareTree, leaves = memBuildTree("shared")
		memShareLeaf = leaves[memShareIndex]
		memSharePath = memPathFromIndex(memShareTree, memShareIndex)
		memShareRoot, memShareProof = ProveMembership(memShareLeaf, memSharePath)
	})
	return memShareRoot, memShareProof
}

// ---------------------------------------------------------------------------
// 1) COHÉRENCE avec merkle_poseidon.go — l'IMPÉRATIF (sans STARK : rapide)
// ---------------------------------------------------------------------------

// La racine reconstruite par la trace DOIT être celle de l'arbre natif pour des
// positions variées (motifs de bits variés), et PoseidonVerifyPath doit accepter
// le même chemin. C'est la cohérence croisée totale (circuit <-> merkle_poseidon).
func TestMembership_CoherenceAvecMerklePoseidon(t *testing.T) {
	tree, leaves := memBuildTree("coherence")
	nativeRoot := tree.Root()

	for _, index := range []int{0, 1, 2, 7, 42, 128, 200, 255} {
		leaf := leaves[index]
		path := memPathFromIndex(tree, index)

		// Racine reconstruite par la trace == racine native de l'arbre.
		_, root := buildMembershipTrace(leaf, path)
		if root != nativeRoot {
			t.Fatalf("index %d: racine trace != racine native", index)
		}
		// Le chemin natif est accepté par PoseidonVerifyPath (même convention).
		if !PoseidonVerifyPath(nativeRoot, index, leaf, PoseidonOpen(tree, index)) {
			t.Fatalf("index %d: PoseidonVerifyPath rejette le chemin natif", index)
		}
	}
}

// La racine renvoyée par ProveMembership (chemin STARK complet) coïncide avec la
// racine native — recoupe le prouveur lui-même avec merkle_poseidon.go (1 preuve,
// partagée).
func TestMembership_ProveMembershipRacineNative(t *testing.T) {
	skipShort(t)
	root, _ := memShared()
	if root != memShareTree.Root() {
		t.Fatalf("racine ProveMembership != racine native de l'arbre")
	}
}

// ---------------------------------------------------------------------------
// 2) POSITIF + déterminisme
// ---------------------------------------------------------------------------

// Une preuve honnête (partagée) vérifie contre la racine publique.
func TestMembership_PreuveHonnete(t *testing.T) {
	skipShort(t)
	root, proof := memShared()
	if !VerifyMembership(root, proof) {
		t.Fatalf("preuve d'appartenance honnête rejetée")
	}
}

// Déterminisme : reprouver la MÊME instance redonne la même preuve (aléa =
// transcript uniquement). Un seul prouveur supplémentaire.
func TestMembership_Deterministe(t *testing.T) {
	skipShort(t)
	root, proof := memShared()
	r2, p2 := ProveMembership(memShareLeaf, memSharePath)

	if r2 != root {
		t.Fatalf("racine non déterministe")
	}
	if p2.CompRoot != proof.CompRoot {
		t.Fatalf("CompRoot non déterministe")
	}
	if len(p2.ColRoots) != len(proof.ColRoots) {
		t.Fatalf("nombre de ColRoots non déterministe")
	}
	for c := range proof.ColRoots {
		if p2.ColRoots[c] != proof.ColRoots[c] {
			t.Fatalf("ColRoots[%d] non déterministe", c)
		}
	}
}

// Le témoin (feuille, siblings, bits) ne figure dans AUCUNE valeur publique :
// l'énoncé public est EXACTEMENT la racine (4 Felt).
func TestMembership_TemoinNonPublie(t *testing.T) {
	tree, _ := memBuildTree("prive")
	root := tree.Root()
	pub := memPublicInputs(root)
	if len(pub) != poseidonDigestLen {
		t.Fatalf("valeurs publiques inattendues: %d (attendu %d = racine seule)",
			len(pub), poseidonDigestLen)
	}
	for k := 0; k < poseidonDigestLen; k++ {
		if !pub[k].Equal(root[k]) {
			t.Fatalf("valeur publique %d != racine", k)
		}
	}
}

// ---------------------------------------------------------------------------
// 3) NÉGATIFS — reconstruction de racine (sans STARK, rapide)
// ---------------------------------------------------------------------------

// FEUILLE HORS-ARBRE : une feuille étrangère sur un chemin valide reconstruit une
// AUTRE racine que celle de l'arbre. La trace en témoigne sans STARK.
func TestMembership_FeuilleHorsArbre(t *testing.T) {
	tree, leaves := memBuildTree("hors-arbre")
	root := tree.Root()
	index := 30

	foreign := memRandomDigest("feuille-etrangere")
	if foreign == leaves[index] {
		t.Fatalf("collision improbable: feuille étrangère == feuille réelle")
	}
	path := memPathFromIndex(tree, index)

	_, reconstructed := buildMembershipTrace(foreign, path)
	if reconstructed == root {
		t.Fatalf("la feuille étrangère reconstruit la vraie racine (impossible)")
	}
	// PoseidonVerifyPath confirme indépendamment le rejet.
	if PoseidonVerifyPath(root, index, foreign, PoseidonOpen(tree, index)) {
		t.Fatalf("PoseidonVerifyPath accepte une feuille hors-arbre")
	}
}

// CHEMIN INCOHÉRENT (sibling faux) : corrompre un sibling reconstruit une autre
// racine (Hash2 dépend du sibling).
func TestMembership_SiblingFalsifie(t *testing.T) {
	tree, leaves := memBuildTree("sibling")
	root := tree.Root()
	index := 100
	leaf := leaves[index]
	path := memPathFromIndex(tree, index)

	path.Siblings[3][0] = path.Siblings[3][0].Add(One())

	_, reconstructed := buildMembershipTrace(leaf, path)
	if reconstructed == root {
		t.Fatalf("un sibling falsifié reconstruit la vraie racine (impossible)")
	}
}

// CHEMIN INCOHÉRENT (bit inversé) : inverser un bit change l'ordre gauche/droite
// d'un niveau ; Hash2 n'étant pas symétrique, la racine diverge.
func TestMembership_BitInverse(t *testing.T) {
	tree, leaves := memBuildTree("bit-inverse")
	root := tree.Root()
	index := 170 // 0b10101010 : bits non triviaux
	leaf := leaves[index]
	path := memPathFromIndex(tree, index)

	if path.Bits[0].IsZero() {
		path.Bits[0] = One()
	} else {
		path.Bits[0] = Zero()
	}

	_, reconstructed := buildMembershipTrace(leaf, path)
	if reconstructed == root {
		t.Fatalf("un bit inversé reconstruit la vraie racine (impossible)")
	}
}

// ---------------------------------------------------------------------------
// 3 bis) NÉGATIFS — rejet par le VÉRIFIEUR (réutilisent la preuve partagée)
// ---------------------------------------------------------------------------

// MAUVAISE RACINE : preuve honnête présentée contre une racine fausse. Le bord
// racine ne tient pas + le transcript diverge => REJET. (Capture aussi, au sens
// du vérifieur, les cas feuille/sibling/bit faux : tous mènent à une racine
// différente présentée au vérifieur.)
func TestMembership_MauvaiseRacine(t *testing.T) {
	skipShort(t)
	root, proof := memShared()
	wrong := root
	wrong[0] = wrong[0].Add(One())
	if VerifyMembership(wrong, proof) {
		t.Fatalf("preuve acceptée avec une racine fausse")
	}
}

// OUVERTURE FALSIFIÉE : une valeur de colonne ouverte ne correspond plus à sa
// racine Merkle / la combinaison DEEP diverge => REJET.
func TestMembership_FalsifieOuverture(t *testing.T) {
	skipShort(t)
	root, proof := memShared()
	bad := clonePoof(proof)
	bad.Openings[0].ColVals[0] = bad.Openings[0].ColVals[0].Add(One())
	if VerifyMembership(root, bad) {
		t.Fatalf("preuve acceptée avec une valeur de colonne ouverte falsifiée")
	}
}

// OOD FALSIFIÉE : corrompre une valeur hors-domaine fait échouer le contrôle
// algébrique en z => REJET.
func TestMembership_FalsifieOOD(t *testing.T) {
	skipShort(t)
	root, proof := memShared()

	bad := clonePoof(proof)
	bad.OodHz = bad.OodHz.Add(One())
	if VerifyMembership(root, bad) {
		t.Fatalf("preuve acceptée avec OodHz falsifié")
	}

	bad2 := clonePoof(proof)
	bad2.OodColZ[0] = bad2.OodColZ[0].Add(One())
	if VerifyMembership(root, bad2) {
		t.Fatalf("preuve acceptée avec OodColZ falsifié")
	}
}

// ---------------------------------------------------------------------------
// 3 ter) NÉGATIFS — trace falsifiée (prouveur dédié : les plus coûteux)
// ---------------------------------------------------------------------------

// BIT NON BINAIRE : on construit une trace honnête puis on force la colonne `bit`
// d'une ligne de ré-assemblage à 2. La contrainte rmode·bit·(bit-1) ne s'annule
// plus (et l'état suivant est faux) => la composition n'est pas de bas degré =>
// REJET. C'est le test dédié de la contrainte de binarité.
func TestMembership_BitNonBinaire(t *testing.T) {
	skipShort(t)
	tree, leaves := memBuildTree("bit-non-binaire")
	index := 50
	leaf := leaves[index]
	path := memPathFromIndex(tree, index)

	trace, root := buildMembershipTrace(leaf, path)
	air := membershipAIR{root: root}
	public := memPublicInputs(root)

	reasmRow := memOutputRowOf(0) // ligne de sortie/ré-assemblage du niveau 0 = 30
	if !trace[reasmRow][memRmodeCol].Equal(One()) {
		t.Fatalf("préparation: ligne %d n'est pas une ligne de ré-assemblage", reasmRow)
	}
	badTrace := memCloneTrace(trace)
	badTrace[reasmRow][memBitCol] = FromUint64(2) // bit non binaire

	badProof := ProveAIR(air, badTrace, public...)
	if VerifyAIR(air, badProof, public...) {
		t.Fatalf("preuve acceptée avec un bit de direction non binaire (=2)")
	}
}

// TRACE FALSIFIÉE (état intermédiaire) : corrompre une cellule d'état à une ronde
// du milieu viole la transition Poseidon => composition pas de bas degré => REJET.
func TestMembership_TraceFalsifiee(t *testing.T) {
	skipShort(t)
	tree, leaves := memBuildTree("trace-falsifiee")
	index := 21
	leaf := leaves[index]
	path := memPathFromIndex(tree, index)

	trace, root := buildMembershipTrace(leaf, path)
	air := membershipAIR{root: root}
	public := memPublicInputs(root)

	row := 2*memBlock + 10 // ronde 10 du niveau 2
	badTrace := memCloneTrace(trace)
	badTrace[row][memStateOff+3] = badTrace[row][memStateOff+3].Add(FromUint64(123456789))

	badProof := ProveAIR(air, badTrace, public...)
	if VerifyAIR(air, badProof, public...) {
		t.Fatalf("preuve acceptée pour une trace violant une ronde Poseidon")
	}
}

// memCloneTrace duplique une trace (lignes + cellules) pour falsification isolée.
func memCloneTrace(trace [][]Felt) [][]Felt {
	out := make([][]Felt, len(trace))
	for i, line := range trace {
		out[i] = append([]Felt(nil), line...)
	}
	return out
}
