// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Tests de l'AIR COMPLET de la permutation Poseidon (poseidon_air_full.go).
//
// Objectifs (du plus important au moins) :
//
//  1. COHÉRENCE : le digest prouvé par ProveHashFull est EXACTEMENT celui de la
//     permutation native Permute() de poseidon.go, pour des entrées aléatoires
//     déterministes. C'est l'IMPÉRATIF : si l'AIR ne calcule pas Poseidon, ce
//     test échoue.
//  2. POSITIF : une preuve honnête vérifie.
//  3. NÉGATIFS : mauvais digest annoncé => rejet ; mauvais état d'entrée public
//     => rejet ; trace falsifiée (une cellule corrompue à une ronde) => rejet ;
//     ouverture / OOD falsifiées => rejet.
package stark

import "testing"

// pfRandomState produit un état d'entrée [12]Felt DÉTERMINISTE à partir d'une
// graine entière (via un flux SHAKE256 du paquet, sans time ni math/rand). Sert
// à exercer l'AIR sur des entrées variées mais reproductibles.
func pfRandomState(seed uint64) [pfStateCols]Felt {
	xof := newXOF("test/poseidon-air-full/" + itoaU64(seed))
	var s [pfStateCols]Felt
	for k := 0; k < pfStateCols; k++ {
		s[k] = nextFelt(xof)
	}
	return s
}

// itoaU64 convertit un uint64 en chaîne décimale (sans importer strconv dans le
// test ; suffisant pour une étiquette de domaine).
func itoaU64(x uint64) string {
	if x == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for x > 0 {
		i--
		buf[i] = byte('0' + x%10)
		x /= 10
	}
	return string(buf[i:])
}

// ---------------------------------------------------------------------------
// 1) COHÉRENCE avec la permutation native — l'IMPÉRATIF
// ---------------------------------------------------------------------------

func TestPoseidonFull_CoherenceAvecPermute(t *testing.T) {
	skipShort(t)
	for seed := uint64(0); seed < 8; seed++ {
		input := pfRandomState(seed)

		// Digest prouvé par l'AIR complet.
		digest, _ := ProveHashFull(input)

		// Digest natif : 4 premières cellules de Permute(input).
		native := Permute(input)
		for k := 0; k < poseidonDigestLen; k++ {
			if !digest[k].Equal(native[k]) {
				t.Fatalf("seed %d: digest AIR != Permute natif à la cellule %d : AIR=%d natif=%d",
					seed, k, digest[k].Uint64(), native[k].Uint64())
			}
		}
	}
}

// La trace construite doit reproduire l'état natif à CHAQUE ronde (recoupe fine
// du calcul, pas seulement du digest). On compare l'état des lignes 0..30 de la
// trace à un déroulé manuel de Permute ronde par ronde.
func TestPoseidonFull_TraceSuitPermuteRondeParRonde(t *testing.T) {
	input := pfRandomState(123)
	trace, output := buildPoseidonFullTrace(input)

	// Déroulé manuel identique à Permute (mais en exposant chaque état).
	s := input
	// Ligne 0 = entrée.
	pfAssertStateRow(t, trace[0], s, 0)
	for r := 0; r < poseidonTotalRounds; r++ {
		rc := params.roundConstants[r]
		s = pfApplyRound(s, rc, pfIsFullRound(r))
		// L'état après la ronde r est l'état de la ligne r+1.
		pfAssertStateRow(t, trace[r+1], s, r+1)
	}
	// L'état final exposé doit égaler la sortie et Permute(input).
	native := Permute(input)
	for k := 0; k < pfStateCols; k++ {
		if !output[k].Equal(native[k]) {
			t.Fatalf("sortie trace != Permute à la cellule %d", k)
		}
		if !s[k].Equal(native[k]) {
			t.Fatalf("déroulé manuel != Permute à la cellule %d", k)
		}
	}
}

// pfAssertStateRow vérifie que les 12 colonnes d'état d'une ligne de trace
// valent l'état attendu.
func pfAssertStateRow(t *testing.T, line []Felt, want [pfStateCols]Felt, row int) {
	t.Helper()
	for k := 0; k < pfStateCols; k++ {
		if !line[pfStateOff+k].Equal(want[k]) {
			t.Fatalf("ligne %d, cellule d'état %d : trace=%d attendu=%d",
				row, k, line[pfStateOff+k].Uint64(), want[k].Uint64())
		}
	}
}

// ---------------------------------------------------------------------------
// 2) POSITIF : preuve honnête vérifie
// ---------------------------------------------------------------------------

func TestPoseidonFull_PreuveHonnete(t *testing.T) {
	skipShort(t)
	input := pfRandomState(42)
	digest, proof := ProveHashFull(input)
	if !VerifyHashFull(input, digest, proof) {
		t.Fatalf("preuve Poseidon complète honnête rejetée")
	}
}

// Déterminisme : deux preuves de la même instance sont identiques (aléa =
// transcript uniquement).
func TestPoseidonFull_Deterministe(t *testing.T) {
	skipShort(t)
	input := pfRandomState(7)
	d1, p1 := ProveHashFull(input)
	d2, p2 := ProveHashFull(input)

	for k := 0; k < poseidonDigestLen; k++ {
		if !d1[k].Equal(d2[k]) {
			t.Fatalf("digest non déterministe à la cellule %d", k)
		}
	}
	if p1.CompRoot != p2.CompRoot {
		t.Fatalf("CompRoot non déterministe")
	}
	if len(p1.ColRoots) != len(p2.ColRoots) {
		t.Fatalf("nombre de ColRoots non déterministe")
	}
	for c := range p1.ColRoots {
		if p1.ColRoots[c] != p2.ColRoots[c] {
			t.Fatalf("ColRoots[%d] non déterministe", c)
		}
	}
}

// ---------------------------------------------------------------------------
// 3) NÉGATIFS
// ---------------------------------------------------------------------------

// Mauvais digest annoncé au vérifieur : le bord de sortie ne tient pas ET le
// transcript diverge => rejet.
func TestPoseidonFull_MauvaisDigest(t *testing.T) {
	skipShort(t)
	input := pfRandomState(99)
	digest, proof := ProveHashFull(input)

	wrong := digest
	wrong[0] = wrong[0].Add(One())
	if VerifyHashFull(input, wrong, proof) {
		t.Fatalf("preuve acceptée avec un digest faux")
	}
}

// Mauvais état d'entrée public : le bord d'entrée ne tient pas ET le transcript
// diverge => rejet.
func TestPoseidonFull_MauvaisInput(t *testing.T) {
	skipShort(t)
	input := pfRandomState(100)
	digest, proof := ProveHashFull(input)

	wrongInput := input
	wrongInput[5] = wrongInput[5].Add(One())
	if VerifyHashFull(wrongInput, digest, proof) {
		t.Fatalf("preuve acceptée avec un état d'entrée public faux")
	}
}

// Trace falsifiée : on corrompt UNE cellule d'état à une ronde du milieu, puis on
// prouve. La transition (et/ou le bord) est violée => la composition n'est pas de
// bas degré => FRI / OOD rejettent.
func TestPoseidonFull_TraceFalsifiee(t *testing.T) {
	skipShort(t)
	input := pfRandomState(7)

	// On reconstruit la trace honnête puis on corrompt une cellule à la ronde 10
	// (une ronde partielle), colonne d'état 3.
	trace, output := buildPoseidonFullTrace(input)
	var digest [poseidonDigestLen]Felt
	copy(digest[:], output[:poseidonDigestLen])

	trace[10][pfStateOff+3] = trace[10][pfStateOff+3].Add(FromUint64(987654321))

	air := poseidonFullAIR{input: input, digest: digest}
	public := pfPublicInputs(input, digest)
	badProof := ProveAIR(air, trace, public...)

	if VerifyAIR(air, badProof, public...) {
		t.Fatalf("preuve acceptée pour une trace violant la transition Poseidon")
	}
}

// Falsification d'une constante de ronde dans la trace : on remplace une cellule
// rc par une autre valeur. Le bord correspondant (qui ancre cette constante) est
// violé => rejet. Cela prouve que les constantes ne sont pas manipulables.
func TestPoseidonFull_ConstanteFalsifiee(t *testing.T) {
	skipShort(t)
	input := pfRandomState(55)
	trace, output := buildPoseidonFullTrace(input)
	var digest [poseidonDigestLen]Felt
	copy(digest[:], output[:poseidonDigestLen])

	// Corrompt rc[2] de la ronde 5 (et l'état correspondant pour rester cohérent
	// avec la transition : on garde la transition satisfaite mais on viole le bord
	// de constante). Ici on corrompt UNIQUEMENT la colonne rc : la transition ne
	// tient alors plus non plus, mais surtout le bord d'ancrage de constante est
	// violé.
	trace[5][pfRcOff+2] = trace[5][pfRcOff+2].Add(One())

	air := poseidonFullAIR{input: input, digest: digest}
	public := pfPublicInputs(input, digest)
	badProof := ProveAIR(air, trace, public...)

	if VerifyAIR(air, badProof, public...) {
		t.Fatalf("preuve acceptée avec une constante de ronde falsifiée dans la trace")
	}
}

// Falsification d'un sélecteur (fsel) dans la trace : transformer une ronde
// partielle en « pleine » dans la colonne fsel viole le bord d'ancrage du
// sélecteur => rejet.
func TestPoseidonFull_SelecteurFalsifie(t *testing.T) {
	skipShort(t)
	input := pfRandomState(56)
	trace, output := buildPoseidonFullTrace(input)
	var digest [poseidonDigestLen]Felt
	copy(digest[:], output[:poseidonDigestLen])

	// Ronde 10 est partielle (fsel=0) ; on la force à 1.
	trace[10][pfFselCol] = One()

	air := poseidonFullAIR{input: input, digest: digest}
	public := pfPublicInputs(input, digest)
	badProof := ProveAIR(air, trace, public...)

	if VerifyAIR(air, badProof, public...) {
		t.Fatalf("preuve acceptée avec un sélecteur fsel falsifié")
	}
}

// Falsification d'une valeur de colonne ouverte : l'ouverture Merkle ne
// correspond plus à la racine OU la combinaison DEEP diverge => rejet.
func TestPoseidonFull_FalsifieColonne(t *testing.T) {
	skipShort(t)
	input := pfRandomState(11)
	digest, proof := ProveHashFull(input)
	if !VerifyHashFull(input, digest, proof) {
		t.Fatalf("préparation: preuve honnête rejetée")
	}

	bad := clonePoof(proof)
	bad.Openings[0].ColVals[0] = bad.Openings[0].ColVals[0].Add(One())
	if VerifyHashFull(input, digest, bad) {
		t.Fatalf("preuve acceptée avec une valeur de colonne falsifiée")
	}
}

// Falsification d'une valeur hors-domaine (OOD) : le contrôle algébrique en z
// échoue => rejet.
func TestPoseidonFull_FalsifieOOD(t *testing.T) {
	skipShort(t)
	input := pfRandomState(13)
	digest, proof := ProveHashFull(input)

	bad := clonePoof(proof)
	bad.OodHz = bad.OodHz.Add(One())
	if VerifyHashFull(input, digest, bad) {
		t.Fatalf("preuve acceptée avec OodHz falsifié")
	}

	bad2 := clonePoof(proof)
	bad2.OodColZ[0] = bad2.OodColZ[0].Add(One())
	if VerifyHashFull(input, digest, bad2) {
		t.Fatalf("preuve acceptée avec OodColZ falsifié")
	}
}
