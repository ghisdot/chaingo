// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// VÉRIFICATION ADVERSE du moteur multi-colonnes (stark_mc.go) et de l'AIR
// Poseidon COMPLET (poseidon_air_full.go) — étage 3.3. Ce fichier COMPLÈTE
// stark_mc_test.go et poseidon_air_full_test.go par les attaques qu'ils ne
// couvraient PAS explicitement. Objectif : tenter de FORGER une preuve
// acceptée pour un énoncé faux, et constater le rejet. Chaque test est NÉGATIF
// (on exige un rejet) ou DOCUMENTE une limite.
//
// Surfaces attaquées (cf. cahier des charges de l'étage) :
//   - composition H qui ne LIE PAS toutes les colonnes : une colonne « fantôme »
//     présente dans AUCUNE contrainte est-elle tout de même engagée et liée par
//     la couche DEEP/FRI ? (sinon un prouveur y mettrait n'importe quoi) ;
//   - OOD PARTIEL : une ouverture hors-domaine de colonne tronquée / désynchronisée ;
//   - DEEP qui OUBLIE une colonne : décalage de l'OOD de CHAQUE colonne, une à une ;
//   - contrainte MDS FAUSSE : une trace dont une ronde applique une AUTRE matrice
//     linéaire que params.mds (couche de diffusion truquée) doit être rejetée ;
//   - sélecteurs full/partial INVERSABLES : forcer le motif plein/partiel, et
//     tenter un motif auto-cohérent mais ancré au mauvais sélecteur ;
//   - FORGERIE de digest : prouver un faux digest Poseidon pour un input public ;
//   - REJEU : présenter une preuve honnête sur une AUTRE instance (input/digest).
//
// HONNÊTETÉ : crypto MAISON, paramètres Poseidon NON AUDITÉS (poseidon.go). Ces
// tests CONSTATENT la soundness du MÉCANISME (Fiat-Shamir + FRI + DEEP + bords),
// PAS la sécurité cryptographique des paramètres. Déterministe : aucun
// time/math/rand ; PRNG à graine fixe (newPRNG) et flux SHAKE du paquet.
package stark

import "testing"

// ---------------------------------------------------------------------------
// 1) COMPOSITION H : une colonne « fantôme » est-elle liée malgré tout ?
// ---------------------------------------------------------------------------
//
// ghostAIR a 2 colonnes : la colonne 0 est une suite contrainte (constante
// décalée x'=x, ancrée en bord), la colonne 1 (« fantôme ») n'apparaît dans
// AUCUNE contrainte de transition NI de bord. Si le moteur ne liait pas cette
// colonne par la couche DEEP/FRI, un prouveur pourrait engager n'importe quoi
// dans son commitment sans être détecté. On vérifie au contraire que CORROMPRE
// la valeur ouverte de la colonne fantôme (sans corriger sa racine/chemin)
// déclenche un rejet — donc qu'elle EST liée par l'authenticité Merkle et la
// recombinaison DEEP, même sans contrainte.
type ghostAIR struct {
	n     int
	start Felt
}

func (g ghostAIR) NumColumns() int { return 2 }
func (g ghostAIR) NumSteps() int   { return g.n }
func (g ghostAIR) MaxDegree() int  { return 1 }

// EvalTransition ne contraint QUE la colonne 0 (x'=x). La colonne 1 est libre :
// elle n'apparaît dans aucun résidu. C'est une colonne « fantôme ».
func (g ghostAIR) EvalTransition(cur, next []Felt) []Felt {
	return []Felt{next[0].Sub(cur[0])} // un seul résidu, sur la colonne 0
}

// Boundaries n'ancre que la colonne 0. La colonne 1 n'a AUCUN bord.
func (g ghostAIR) Boundaries() []Boundary {
	return []Boundary{{Col: 0, Row: 0, Value: g.start}}
}

func buildGhostTrace(start Felt, n int, rng *prng) [][]Felt {
	trace := make([][]Felt, n)
	for i := 0; i < n; i++ {
		// Colonne 0 constante ; colonne 1 = bruit (libre, non contraint).
		trace[i] = []Felt{start, rng.felt()}
	}
	return trace
}

// TestMC_ColonneFantomeQuandMemeLiee : la colonne fantôme (sans contrainte) est
// engagée par sa propre racine Merkle ET liée par le quotient DEEP. Corrompre
// son ouverture sans recalculer la racine/chemin => rejet. Cela prouve que le
// moteur LIE TOUTES les colonnes engagées, pas seulement celles contraintes.
func TestMC_ColonneFantomeQuandMemeLiee(t *testing.T) {
	rng := newPRNG(0x6405701)
	n := 16
	start := FromUint64(42)
	trace := buildGhostTrace(start, n, rng)
	air := ghostAIR{n: n, start: start}

	proof := ProveAIR(air, trace, start)
	if !VerifyAIR(air, proof, start) {
		t.Fatal("préparation : preuve honnête (colonne fantôme) rejetée")
	}

	// La preuve engage bien W=2 colonnes (la fantôme est commitée).
	if len(proof.ColRoots) != 2 {
		t.Fatalf("attendu 2 racines de colonne (dont la fantôme), got %d", len(proof.ColRoots))
	}

	// Corruption de la valeur OUVERTE de la colonne fantôme (col 1) à une requête :
	// l'authenticité Merkle (et/ou la recombinaison DEEP) doit la rejeter.
	bad := clonePoof(proof)
	bad.Openings[0].ColVals[1] = bad.Openings[0].ColVals[1].Add(One())
	if VerifyAIR(air, bad, start) {
		t.Fatal("SOUNDNESS : valeur de colonne fantôme corrompue acceptée (colonne non liée)")
	}

	// Corruption de l'OOD de la colonne fantôme en z : la recombinaison DEEP
	// (qui inclut un terme par colonne, y compris la fantôme via γ_z[1]) diverge.
	bad2 := clonePoof(proof)
	bad2.OodColZ[1] = bad2.OodColZ[1].Add(One())
	if VerifyAIR(air, bad2, start) {
		t.Fatal("SOUNDNESS : OOD de colonne fantôme falsifié accepté (DEEP ne lie pas la colonne)")
	}
}

// ---------------------------------------------------------------------------
// 2) OOD PARTIEL : ouverture hors-domaine tronquée ou désynchronisée.
// ---------------------------------------------------------------------------

// TestMC_OodColonneTronquee : retirer une colonne du vecteur OOD (len != W) doit
// être rejeté structurellement, sans panique.
func TestMC_OodColonneTronquee(t *testing.T) {
	n := 16
	a0, b0 := FromUint64(3), FromUint64(8)
	trace := buildCoupledTrace(a0, b0, n)
	lastA := trace[n-1][0]
	air := coupledAIR{n: n, a0: a0, b0: b0, lastA: lastA}
	proof := ProveAIR(air, trace, a0, b0, lastA)

	// OOD en z tronqué d'une colonne.
	bad := clonePoof(proof)
	bad.OodColZ = bad.OodColZ[:len(bad.OodColZ)-1]
	if VerifyAIR(air, bad, a0, b0, lastA) {
		t.Fatal("SOUNDNESS : OodColZ tronqué accepté")
	}

	// OOD en g·z tronqué d'une colonne.
	bad2 := clonePoof(proof)
	bad2.OodColGZ = bad2.OodColGZ[:len(bad2.OodColGZ)-1]
	if VerifyAIR(air, bad2, a0, b0, lastA) {
		t.Fatal("SOUNDNESS : OodColGZ tronqué accepté")
	}

	// Racines de colonnes tronquées (len != W).
	bad3 := clonePoof(proof)
	bad3.ColRoots = bad3.ColRoots[:len(bad3.ColRoots)-1]
	if VerifyAIR(air, bad3, a0, b0, lastA) {
		t.Fatal("SOUNDNESS : ColRoots tronqué accepté")
	}
}

// TestMC_OodColonnesPermutees : intervertir les OOD de deux colonnes (sans
// toucher au reste) brise l'identité algébrique en z et/ou la recombinaison
// DEEP => rejet. Désynchronisation de l'ouverture hors-domaine.
func TestMC_OodColonnesPermutees(t *testing.T) {
	n := 16
	a0, b0 := FromUint64(5), FromUint64(2)
	trace := buildCoupledTrace(a0, b0, n)
	lastA := trace[n-1][0]
	air := coupledAIR{n: n, a0: a0, b0: b0, lastA: lastA}
	proof := ProveAIR(air, trace, a0, b0, lastA)

	// Sanity : la permutation des deux colonnes doit changer au moins une valeur.
	if proof.OodColZ[0].Equal(proof.OodColZ[1]) {
		t.Skip("OOD des deux colonnes égales par hasard : permutation inopérante")
	}

	bad := clonePoof(proof)
	bad.OodColZ[0], bad.OodColZ[1] = bad.OodColZ[1], bad.OodColZ[0]
	if VerifyAIR(air, bad, a0, b0, lastA) {
		t.Fatal("SOUNDNESS : OOD de colonnes permutées accepté (désync OOD non détectée)")
	}
}

// ---------------------------------------------------------------------------
// 3) DEEP qui OUBLIE une colonne : décalage de l'OOD de CHAQUE colonne, une à une.
// ---------------------------------------------------------------------------

// TestPoseidonFull_ChaqueColonneOodLiee : sur l'AIR Poseidon COMPLET (W=26),
// décaler l'OOD en z de CHAQUE colonne (état, constantes, sélecteurs), une par
// une, doit TOUJOURS être rejeté. Si une seule colonne échappait à la liaison
// DEEP, ce décalage passerait. C'est l'attaque « DEEP qui oublie une colonne »,
// menée exhaustivement sur les 26 colonnes.
func TestPoseidonFull_ChaqueColonneOodLiee(t *testing.T) {
	input := pfRandomState(2024)
	digest, proof := ProveHashFull(input)
	if !VerifyHashFull(input, digest, proof) {
		t.Fatal("préparation : preuve honnête rejetée")
	}
	if len(proof.OodColZ) != pfNumCols {
		t.Fatalf("attendu %d OOD de colonne, got %d", pfNumCols, len(proof.OodColZ))
	}

	for c := 0; c < pfNumCols; c++ {
		bad := clonePoof(proof)
		bad.OodColZ[c] = bad.OodColZ[c].Add(One())
		if VerifyHashFull(input, digest, bad) {
			t.Fatalf("SOUNDNESS : OOD en z de la colonne %d non liée (décalage accepté)", c)
		}

		bad2 := clonePoof(proof)
		bad2.OodColGZ[c] = bad2.OodColGZ[c].Add(One())
		if VerifyHashFull(input, digest, bad2) {
			t.Fatalf("SOUNDNESS : OOD en g·z de la colonne %d non liée (décalage accepté)", c)
		}
	}
}

// TestPoseidonFull_ChaqueOuvertureColonneLiee : décaler la VALEUR OUVERTE
// (in-domaine) de chaque colonne à une requête doit être rejeté (authenticité
// Merkle par colonne). On le fait sur la première requête pour les 26 colonnes.
func TestPoseidonFull_ChaqueOuvertureColonneLiee(t *testing.T) {
	input := pfRandomState(2025)
	digest, proof := ProveHashFull(input)

	for c := 0; c < pfNumCols; c++ {
		bad := clonePoof(proof)
		bad.Openings[0].ColVals[c] = bad.Openings[0].ColVals[c].Add(One())
		if VerifyHashFull(input, digest, bad) {
			t.Fatalf("SOUNDNESS : valeur ouverte de la colonne %d non liée", c)
		}
	}
}

// ---------------------------------------------------------------------------
// 4) CONTRAINTE MDS FAUSSE : une couche de diffusion truquée doit être rejetée.
// ---------------------------------------------------------------------------

// pfApplyRoundWrongMDS applique une ronde Poseidon mais avec une matrice MDS
// TRUQUÉE (params.mds dont on a perturbé un coefficient). La trace produite avec
// cette matrice ne satisfait PAS EvalTransition (qui, lui, utilise la VRAIE
// params.mds). On l'utilise pour fabriquer une trace « MDS faux » et vérifier le
// rejet.
func pfApplyRoundWrongMDS(s [pfStateCols]Felt, rc [pfStateCols]Felt, full bool,
	wrong [pfStateCols][pfStateCols]Felt) [pfStateCols]Felt {
	var a [pfStateCols]Felt
	for k := 0; k < pfStateCols; k++ {
		a[k] = s[k].Add(rc[k])
	}
	var sb [pfStateCols]Felt
	sb[0] = a[0].Exp(poseidonAlpha)
	for k := 1; k < pfStateCols; k++ {
		if full {
			sb[k] = a[k].Exp(poseidonAlpha)
		} else {
			sb[k] = a[k]
		}
	}
	var out [pfStateCols]Felt
	for k := 0; k < pfStateCols; k++ {
		acc := Zero()
		for j := 0; j < pfStateCols; j++ {
			acc = acc.Add(wrong[k][j].Mul(sb[j]))
		}
		out[k] = acc
	}
	return out
}

// TestPoseidonFull_MDSFausseRejetee : on construit une trace COHÉRENTE avec une
// matrice MDS DIFFÉRENTE (un coefficient perturbé) à TOUTES les rondes. Chaque
// ligne suivante est l'image de la ronde par la FAUSSE matrice ; les colonnes
// rc/fsel/active restent les vraies (ancrées en bord). EvalTransition utilise la
// VRAIE params.mds : la transition est donc violée à chaque ronde réelle.
// La composition n'est pas de bas degré => rejet. On prouve ainsi qu'on ne peut
// PAS faire passer un calcul dont la couche linéaire diffère de params.mds.
func TestPoseidonFull_MDSFausseRejetee(t *testing.T) {
	input := pfRandomState(321)

	// Matrice truquée : params.mds avec un coefficient incrémenté de 1.
	wrong := params.mds
	wrong[3][7] = wrong[3][7].Add(One())

	// Construction de la trace avec la FAUSSE matrice (état ronde par ronde).
	trace := make([][]Felt, pfSteps)
	s := input
	for row := 0; row < pfSteps; row++ {
		rc, fsel, active := pfRowConstants(row)
		line := make([]Felt, pfNumCols)
		for k := 0; k < pfStateCols; k++ {
			line[pfStateOff+k] = s[k]
			line[pfRcOff+k] = rc[k]
		}
		line[pfFselCol] = fsel
		line[pfActCol] = active
		trace[row] = line
		if row+1 < pfSteps && !active.IsZero() {
			s = pfApplyRoundWrongMDS(s, rc, !fsel.IsZero(), wrong)
		}
	}

	// Le digest « annoncé » est la sortie de la FAUSSE permutation (cohérent avec
	// la trace truquée, mais ce N'EST PAS Poseidon). Sanity : il diffère du natif.
	var digest [poseidonDigestLen]Felt
	copy(digest[:], s[:poseidonDigestLen])
	native := Permute(input)
	diff := false
	for k := 0; k < poseidonDigestLen; k++ {
		if !digest[k].Equal(native[k]) {
			diff = true
		}
	}
	if !diff {
		t.Fatal("setup invalide : la fausse MDS donne le même digest que Permute")
	}

	air := poseidonFullAIR{input: input, digest: digest}
	public := pfPublicInputs(input, digest)
	badProof := ProveAIR(air, trace, public...)

	if VerifyAIR(air, badProof, public...) {
		t.Fatal("SOUNDNESS : trace à couche MDS truquée acceptée (contrainte MDS non liante)")
	}
}

// TestPoseidonFull_MDSPartielleRejetee : variante plus fine — on truque la MDS à
// UNE SEULE ronde du milieu (ronde 12, partielle), le reste honnête. La
// transition n'est violée qu'à cette ronde, mais cela suffit à rendre la
// composition de degré plein => rejet.
func TestPoseidonFull_MDSPartielleRejetee(t *testing.T) {
	input := pfRandomState(322)

	trace := make([][]Felt, pfSteps)
	s := input
	for row := 0; row < pfSteps; row++ {
		rc, fsel, active := pfRowConstants(row)
		line := make([]Felt, pfNumCols)
		for k := 0; k < pfStateCols; k++ {
			line[pfStateOff+k] = s[k]
			line[pfRcOff+k] = rc[k]
		}
		line[pfFselCol] = fsel
		line[pfActCol] = active
		trace[row] = line
		if row+1 < pfSteps && !active.IsZero() {
			if row == 12 {
				wrong := params.mds
				wrong[0][0] = wrong[0][0].Add(One())
				s = pfApplyRoundWrongMDS(s, rc, !fsel.IsZero(), wrong)
			} else {
				s = pfApplyRound(s, rc, !fsel.IsZero())
			}
		}
	}
	var digest [poseidonDigestLen]Felt
	copy(digest[:], s[:poseidonDigestLen])

	air := poseidonFullAIR{input: input, digest: digest}
	public := pfPublicInputs(input, digest)
	badProof := ProveAIR(air, trace, public...)
	if VerifyAIR(air, badProof, public...) {
		t.Fatal("SOUNDNESS : MDS truquée à une seule ronde acceptée")
	}
}

// ---------------------------------------------------------------------------
// 5) SÉLECTEURS full/partial : motif inversé non manipulable.
// ---------------------------------------------------------------------------

// TestPoseidonFull_MotifSboxInverseRejete : on tente une trace AUTO-COHÉRENTE
// avec un motif S-box INVERSÉ à une ronde — on calcule l'état suivant comme si
// la ronde 1 (normalement PLEINE) était PARTIELLE — tout en gardant les
// sélecteurs ancrés (fsel=1) aux valeurs OFFICIELLES. La transition
// EvalTransition (qui lit fsel=1 dans la trace) attend alors une S-box PLEINE,
// que la trace ne fournit pas => violation => rejet.
//
// Cela démontre que le prouveur ne peut pas substituer un autre motif plein/
// partiel : le motif est fixé par les bords sur fsel, et la transition est
// évaluée selon ce fsel ancré.
func TestPoseidonFull_MotifSboxInverseRejete(t *testing.T) {
	input := pfRandomState(404)

	trace := make([][]Felt, pfSteps)
	s := input
	for row := 0; row < pfSteps; row++ {
		rc, fsel, active := pfRowConstants(row)
		line := make([]Felt, pfNumCols)
		for k := 0; k < pfStateCols; k++ {
			line[pfStateOff+k] = s[k]
			line[pfRcOff+k] = rc[k]
		}
		line[pfFselCol] = fsel // sélecteur OFFICIEL (non manipulé)
		line[pfActCol] = active
		trace[row] = line
		if row+1 < pfSteps && !active.IsZero() {
			if row == 1 {
				// Ronde 1 est PLEINE (fsel=1) ; on calcule l'état suivant comme si
				// elle était PARTIELLE (full=false) : la trace dévie de ce que la
				// transition (qui lira fsel=1) impose.
				s = pfApplyRound(s, rc, false)
			} else {
				s = pfApplyRound(s, rc, !fsel.IsZero())
			}
		}
	}
	var digest [poseidonDigestLen]Felt
	copy(digest[:], s[:poseidonDigestLen])

	air := poseidonFullAIR{input: input, digest: digest}
	public := pfPublicInputs(input, digest)
	badProof := ProveAIR(air, trace, public...)
	if VerifyAIR(air, badProof, public...) {
		t.Fatal("SOUNDNESS : motif S-box inversé (état partiel sous fsel=1) accepté")
	}
}

// TestPoseidonFull_ActiveFalsifieRejete : forcer active=1 sur une ligne de
// padding (ligne 31) viole le bord d'ancrage du sélecteur active => rejet. Le
// sélecteur d'activité (qui « gèle » l'état après la 30e ronde) n'est pas
// manipulable.
func TestPoseidonFull_ActiveFalsifieRejete(t *testing.T) {
	input := pfRandomState(405)
	trace, output := buildPoseidonFullTrace(input)
	var digest [poseidonDigestLen]Felt
	copy(digest[:], output[:poseidonDigestLen])

	// Ligne 31 = padding (active=0) ; on la force à 1.
	trace[31][pfActCol] = One()

	air := poseidonFullAIR{input: input, digest: digest}
	public := pfPublicInputs(input, digest)
	badProof := ProveAIR(air, trace, public...)
	if VerifyAIR(air, badProof, public...) {
		t.Fatal("SOUNDNESS : sélecteur active falsifié (padding rendu actif) accepté")
	}
}

// ---------------------------------------------------------------------------
// 6) FORGERIE de digest Poseidon (le digest est-il VRAIMENT lié au calcul ?).
// ---------------------------------------------------------------------------

// TestPoseidonFull_ForgerieDigestArbitraire : à la différence de l'AIR S-box
// réduit (où un digest ARBITRAIRE est accepté car la chaîne est inversible et
// l'entrée libre), l'AIR COMPLET ancre l'ÉTAT D'ENTRÉE en bord (ligne 0) ET le
// digest en bord (ligne 30). Pour un input public FIXE, le digest est donc
// déterminé de façon unique par le calcul. On tente de prouver un digest
// arbitraire (input fixe, digest = input[:4], qui n'est PAS Permute(input)[:4])
// et on EXIGE le rejet. C'est la différence sémantique clé avec l'AIR réduit :
// ici le digest est LIÉ au préimage public.
func TestPoseidonFull_ForgerieDigestArbitraire(t *testing.T) {
	input := pfRandomState(606)

	// Digest cible bidon = 4 premières cellules de l'INPUT (≠ sortie réelle, sauf
	// coïncidence négligeable).
	var faux [poseidonDigestLen]Felt
	copy(faux[:], input[:poseidonDigestLen])

	native := Permute(input)
	collision := true
	for k := 0; k < poseidonDigestLen; k++ {
		if !faux[k].Equal(native[k]) {
			collision = false
		}
	}
	if collision {
		t.Skip("coïncidence négligeable : input[:4] == Permute(input)[:4]")
	}

	// Le prouveur HONNÊTE produit la VRAIE trace (digest réel) ; on tente ensuite
	// de la faire vérifier contre le FAUX digest. Le bord de sortie ne tient pas
	// + transcript divergent => rejet.
	digest, proof := ProveHashFull(input)
	if faux[0].Equal(digest[0]) && faux[1].Equal(digest[1]) {
		t.Skip("digest réel coïncide avec le faux : test inopérant")
	}
	if VerifyHashFull(input, faux, proof) {
		t.Fatal("SOUNDNESS : faux digest accepté pour un input public fixe (digest non lié)")
	}

	// Variante : un prouveur malhonnête FABRIQUE une trace dont la sortie (ligne
	// 30) est forcée au faux digest, sans recalculer la permutation. Le bord de
	// sortie est alors satisfait MAIS la transition de la dernière ronde (29->30)
	// est violée => rejet.
	trace, output := buildPoseidonFullTrace(input)
	_ = output
	for k := 0; k < poseidonDigestLen; k++ {
		trace[pfOutputRow][pfStateOff+k] = faux[k]
	}
	air := poseidonFullAIR{input: input, digest: faux}
	public := pfPublicInputs(input, faux)
	forged := ProveAIR(air, trace, public...)
	if VerifyAIR(air, forged, public...) {
		t.Fatal("SOUNDNESS : sortie forcée au faux digest acceptée (transition finale non liante)")
	}
}

// ---------------------------------------------------------------------------
// 7) REJEU : preuve honnête présentée sur une AUTRE instance.
// ---------------------------------------------------------------------------

// TestPoseidonFull_RejeuAutreInstance : une preuve honnête pour (inputA,digestA)
// ne doit PAS vérifier pour une autre instance (inputB, ou digestB), ni en
// croisant input/digest. Le transcript absorbe input ET digest (valeurs
// publiques + bords) : toute substitution diverge.
func TestPoseidonFull_RejeuAutreInstance(t *testing.T) {
	inputA := pfRandomState(700)
	inputB := pfRandomState(701)
	digestA, proofA := ProveHashFull(inputA)
	digestB, _ := ProveHashFull(inputB)

	// Même preuve, autre input public.
	if VerifyHashFull(inputB, digestA, proofA) {
		t.Fatal("SOUNDNESS : preuve de A rejouée sous l'input de B acceptée")
	}
	// Même preuve, autre digest public.
	if VerifyHashFull(inputA, digestB, proofA) {
		t.Fatal("SOUNDNESS : preuve de A rejouée sous le digest de B acceptée")
	}
	// Croisement input A / digest B.
	if VerifyHashFull(inputA, digestB, proofA) {
		t.Fatal("SOUNDNESS : croisement input A / digest B accepté")
	}
}

// TestMC_RejeuFRIGreffee : greffer la preuve FRI d'une instance sur la structure
// d'une autre (la couche 0 de FRI engage P_A ; les ouvertures DEEP de A ne
// peuvent coïncider avec P_B). Rejet attendu — la liaison FRI<->DEEP est par
// instance via le transcript.
func TestMC_RejeuFRIGreffee(t *testing.T) {
	n := 16
	trA := buildCoupledTrace(FromUint64(1), FromUint64(1), n)
	lastA := trA[n-1][0]
	airA := coupledAIR{n: n, a0: FromUint64(1), b0: FromUint64(1), lastA: lastA}
	proofA := ProveAIR(airA, trA, FromUint64(1), FromUint64(1), lastA)

	trB := buildCoupledTrace(FromUint64(2), FromUint64(3), n)
	lastB := trB[n-1][0]
	airB := coupledAIR{n: n, a0: FromUint64(2), b0: FromUint64(3), lastA: lastB}
	proofB := ProveAIR(airB, trB, FromUint64(2), FromUint64(3), lastB)

	// Greffe : on remplace la preuve FRI de A par celle de B (couche 0 = engagement
	// de P_B, incohérent avec les ouvertures DEEP de A et le transcript de A).
	bad := clonePoof(proofA)
	bad.Fri = cloneProof(proofB.Fri)
	if VerifyAIR(airA, bad, FromUint64(1), FromUint64(1), lastA) {
		t.Fatal("SOUNDNESS : FRI greffée d'une autre instance acceptée")
	}
}

// ---------------------------------------------------------------------------
// 8) ROBUSTESSE : le vérifieur multi-colonnes ne panique jamais.
// ---------------------------------------------------------------------------

// TestMC_VerifyNeJamaisPaniquer : preuves dégénérées / mal formées => rejet
// propre (false), sans panique.
func TestMC_VerifyNeJamaisPaniquer(t *testing.T) {
	n := 16
	a0, b0 := FromUint64(1), FromUint64(1)
	trace := buildCoupledTrace(a0, b0, n)
	lastA := trace[n-1][0]
	air := coupledAIR{n: n, a0: a0, b0: b0, lastA: lastA}

	// Preuve entièrement vide.
	if VerifyAIR(air, AirProof{}, a0, b0, lastA) {
		t.Fatal("preuve vide acceptée")
	}

	// Preuve honnête mais valeurs publiques absentes (transcript divergent).
	proof := ProveAIR(air, trace, a0, b0, lastA)
	if VerifyAIR(air, proof) {
		t.Fatal("preuve acceptée sans valeurs publiques")
	}

	// Openings vidées.
	bad := clonePoof(proof)
	bad.Openings = nil
	if VerifyAIR(air, bad, a0, b0, lastA) {
		t.Fatal("preuve sans ouvertures acceptée")
	}

	// FriProof zéro-valeur (LogDomain=0, pas de couches).
	bad2 := clonePoof(proof)
	bad2.Fri = FriProof{}
	if VerifyAIR(air, bad2, a0, b0, lastA) {
		t.Fatal("preuve avec FRI vide acceptée")
	}
}
