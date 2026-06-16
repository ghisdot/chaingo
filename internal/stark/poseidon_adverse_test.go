// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Tests de VÉRIFICATION ADVERSE de l'étage Poseidon (étage 2.4). Ce fichier
// COMPLÈTE poseidon_test.go / merkle_poseidon_test.go / poseidon_air_test.go par
// des contrôles que ces suites NE couvraient PAS, en attaquant explicitement :
//
//   - MDS RÉELLEMENT MDS : la suite existante ne teste que le déterminant GLOBAL
//     (det != 0). Or « MDS » exige que TOUS les mineurs carrés (1×1, 2×2, …)
//     soient inversibles. On teste exhaustivement les mineurs 1×1 et 2×2, et un
//     échantillon de mineurs 3×3 (un mineur déficient ouvrirait une perte de
//     diffusion locale non détectée par le seul déterminant global).
//   - CONSTANTES DE RONDE non biaisées : aucune ronde entièrement nulle (une
//     ronde sans constante laisse la ronde « addRC » sans effet et facilite les
//     attaques à différentielle/invariant), et pas de constante nulle isolée
//     suspecte au-delà du hasard.
//   - ABSENCE DE POINT FIXE TRIVIAL de la permutation (Permute(0)!=0, etc.).
//   - DISJONCTION DE DOMAINE Hash vs Hash2 : recherche active d'une collision
//     entre les deux constructions sur des entrées choisies (padding ambigu).
//   - AIR — STATEMENT VACUEUX : on démontre par construction que ProveHash/
//     VerifyHash accepte un digest ARBITRAIRE (car la chaîne S-box est bijective
//     et t[0] n'a AUCUNE contrainte de bord). C'est une limite de SÉMANTIQUE, pas
//     une faille de soundness du mécanisme STARK, mais elle DOIT être documentée :
//     la preuve ne lie le digest à rien de public.
//   - AIR — SOUNDNESS PROBABILISTE FRI : on mesure le taux d'acceptation d'une
//     trace dont le LDE engagé est bruité à 1 position (near-codeword). Le taux
//     élevé attendu (~1 - q/bigN) est la SOUNDNESS PROBABILISTE classique de FRI
//     avec un faible nombre de requêtes — À CONNAÎTRE pour calibrer queries/blowup
//     avant tout usage sérieux. Une corruption MASSIVE (≥50 %) est, elle, rejetée
//     quasi sûrement.
//
// HONNÊTETÉ : ces tests CONSTATENT le comportement (ils ne le « corrigent » pas).
// Les paramètres Poseidon (MDS + constantes) sont CHOISIS PAR NOUS, NON AUDITÉS.
//
// Déterministe : aucun time/math/rand global ; PRNG à graine fixe (newPRNG) et
// math/big (stdlib) pour l'inverse de la S-box dans le test de digest arbitraire.
package stark

import (
	"math/big"
	"testing"
)

// ---------------------------------------------------------------------------
// 1) MDS RÉELLEMENT MDS : mineurs carrés inversibles (pas seulement det global).
// ---------------------------------------------------------------------------

// minorDet2 calcule le déterminant 2×2 d'un mineur de m (lignes r0,r1 ;
// colonnes c0,c1) dans le corps.
func minorDet2(m *[poseidonWidth][poseidonWidth]Felt, r0, r1, c0, c1 int) Felt {
	return m[r0][c0].Mul(m[r1][c1]).Sub(m[r0][c1].Mul(m[r1][c0]))
}

// minorDet3 calcule le déterminant 3×3 d'un mineur de m par développement.
func minorDet3(m *[poseidonWidth][poseidonWidth]Felt, r [3]int, c [3]int) Felt {
	a := m[r[0]][c[0]].Mul(minorDet2sub(m, r[1], r[2], c[1], c[2]))
	b := m[r[0]][c[1]].Mul(minorDet2sub(m, r[1], r[2], c[0], c[2]))
	d := m[r[0]][c[2]].Mul(minorDet2sub(m, r[1], r[2], c[0], c[1]))
	return a.Sub(b).Add(d)
}

func minorDet2sub(m *[poseidonWidth][poseidonWidth]Felt, r0, r1, c0, c1 int) Felt {
	return m[r0][c0].Mul(m[r1][c1]).Sub(m[r0][c1].Mul(m[r1][c0]))
}

// TestPoseidonMDSVraimentMDS vérifie la propriété MDS au-delà du déterminant
// global : tout coefficient (mineur 1×1) est non nul, tout mineur 2×2 est
// inversible, et un large échantillon de mineurs 3×3 l'est aussi. Une matrice de
// Cauchy l'a par construction ; ce test attraperait une dérivation buggée
// (doublon de points non rejeté, repli de réduction erroné) qui passerait le
// simple test de déterminant global.
func TestPoseidonMDSVraimentMDS(t *testing.T) {
	m := &params.mds

	// Mineurs 1×1 : aucun coefficient nul.
	for i := 0; i < poseidonWidth; i++ {
		for j := 0; j < poseidonWidth; j++ {
			if m[i][j].IsZero() {
				t.Fatalf("MDS non-MDS : coefficient nul en (%d,%d)", i, j)
			}
		}
	}

	// Mineurs 2×2 : exhaustif sur toutes paires de lignes × paires de colonnes.
	for r0 := 0; r0 < poseidonWidth; r0++ {
		for r1 := r0 + 1; r1 < poseidonWidth; r1++ {
			for c0 := 0; c0 < poseidonWidth; c0++ {
				for c1 := c0 + 1; c1 < poseidonWidth; c1++ {
					if minorDet2(m, r0, r1, c0, c1).IsZero() {
						t.Fatalf("MDS non-MDS : mineur 2×2 singulier lignes(%d,%d) colonnes(%d,%d)",
							r0, r1, c0, c1)
					}
				}
			}
		}
	}

	// Mineurs 3×3 : échantillon déterministe large (toutes les sous-matrices 3×3
	// adjacentes par décalage cyclique des indices). Couvre une diagonale de cas.
	for s := 0; s < poseidonWidth; s++ {
		r := [3]int{s % poseidonWidth, (s + 1) % poseidonWidth, (s + 2) % poseidonWidth}
		for cs := 0; cs < poseidonWidth; cs++ {
			c := [3]int{cs % poseidonWidth, (cs + 3) % poseidonWidth, (cs + 6) % poseidonWidth}
			// On s'assure que les indices de ligne et de colonne sont distincts.
			if r[0] == r[1] || r[1] == r[2] || r[0] == r[2] {
				continue
			}
			if c[0] == c[1] || c[1] == c[2] || c[0] == c[2] {
				continue
			}
			if minorDet3(m, r, c).IsZero() {
				t.Fatalf("MDS non-MDS : mineur 3×3 singulier lignes%v colonnes%v", r, c)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 2) CONSTANTES DE RONDE : aucune ronde toute nulle, biais de nullité borné.
// ---------------------------------------------------------------------------

// TestPoseidonRoundConstantsNonDegenerees vérifie qu'aucune ronde n'a TOUTES ses
// constantes nulles (l'addRC serait alors inopérant sur cette ronde, ce qui
// affaiblit la résistance aux invariants) et que le nombre total de constantes
// nulles reste compatible avec un tirage uniforme (au plus quelques-unes sur 360).
func TestPoseidonRoundConstantsNonDegenerees(t *testing.T) {
	totalZero := 0
	for r := 0; r < poseidonTotalRounds; r++ {
		allZero := true
		for i := 0; i < poseidonWidth; i++ {
			if params.roundConstants[r][i].IsZero() {
				totalZero++
			} else {
				allZero = false
			}
		}
		if allZero {
			t.Fatalf("ronde %d : toutes les constantes de ronde sont nulles (addRC inopérant)", r)
		}
	}
	// Sur 360 constantes tirées uniformément dans [0,P), l'espérance de zéros est
	// ~360/P ≈ 0. En voir plus d'une poignée signalerait un biais de dérivation.
	if totalZero > 3 {
		t.Fatalf("biais suspect : %d constantes de ronde nulles sur %d (attendu ~0)",
			totalZero, poseidonTotalRounds*poseidonWidth)
	}
}

// ---------------------------------------------------------------------------
// 3) ABSENCE DE POINT FIXE TRIVIAL de la permutation.
// ---------------------------------------------------------------------------

// TestPoseidonPasDePointFixeTrivial vérifie que la permutation ne fixe pas les
// états triviaux : Permute(0)!=0 (sinon absorber un bloc nul serait inopérant) et
// Permute(id)!=id. Un point fixe trivial trahirait une couche addRC/MDS dégénérée.
func TestPoseidonPasDePointFixeTrivial(t *testing.T) {
	var zero [poseidonWidth]Felt
	if Permute(zero) == zero {
		t.Fatal("Permute(0...0) == 0 : point fixe trivial (addRC/MDS dégénérés)")
	}

	id := stateFromU64([poseidonWidth]uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11})
	if Permute(id) == id {
		t.Fatal("Permute(0..11) == (0..11) : point fixe trivial")
	}

	// Quelques états aléatoires : aucun ne doit être un point fixe (proba ~0).
	rng := newPRNG(0xF13D1234)
	for k := 0; k < 256; k++ {
		var s [poseidonWidth]Felt
		for i := range s {
			s[i] = rng.felt()
		}
		if Permute(s) == s {
			t.Fatalf("point fixe inattendu de la permutation à l'échantillon %d", k)
		}
	}
}

// ---------------------------------------------------------------------------
// 4) DISJONCTION DE DOMAINE Hash vs Hash2 (recherche active de collision).
// ---------------------------------------------------------------------------

// TestPoseidonHashHash2Disjoints recherche activement une collision entre la
// construction éponge Hash et la compression Hash2 sur des entrées « adverses »
// (les 8 Felt de Hash2 présentés à Hash sous diverses longueurs). Le commentaire
// de poseidon.go affirme que les deux constructions sont dans des domaines
// disjoints (Hash ajoute toujours un séparateur, et la longueur encodée diffère) :
// on le met à l'épreuve. Aucune collision ne doit être trouvée.
func TestPoseidonHashHash2Disjoints(t *testing.T) {
	rng := newPRNG(0xD1570)
	for iter := 0; iter < 2000; iter++ {
		var l, r [poseidonDigestLen]Felt
		for j := range l {
			l[j] = rng.felt()
			r[j] = rng.felt()
		}
		h2 := Hash2(l, r)

		// Présente les mêmes 8 Felt à Hash, et aussi des préfixes de longueurs
		// 7..8, là où la frontière de bloc/longueur est la plus tendue.
		full := []Felt{l[0], l[1], l[2], l[3], r[0], r[1], r[2], r[3]}
		for ln := 6; ln <= 8; ln++ {
			if Hash(full[:ln]) == h2 {
				t.Fatalf("COLLISION Hash(len=%d) == Hash2 à l'itération %d", ln, iter)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 5) AIR — LE DIGEST PROUVÉ N'EST LIÉ À RIEN (statement vacueux).
// ---------------------------------------------------------------------------

// sboxInverse inverse la S-box x^7 dans Goldilocks : x = y^d où 7·d ≡ 1 (mod p-1).
// Utilise math/big (stdlib) ; déterministe.
func sboxInverse(y Felt) Felt {
	pm1 := new(big.Int).SetUint64(P - 1)
	d := new(big.Int).ModInverse(big.NewInt(int64(poseidonAlpha)), pm1)
	if d == nil {
		panic("sboxInverse: alpha non inversible mod p-1 (la S-box ne serait pas bijective)")
	}
	yb := new(big.Int).SetUint64(y.Uint64())
	pb := new(big.Int).SetUint64(P)
	return FromUint64(new(big.Int).Exp(yb, d, pb).Uint64())
}

// TestSboxAIRDigestArbitraireAccepte démontre une LIMITE DE SÉMANTIQUE de l'AIR
// réduit : comme la chaîne S-box est une bijection et que t[0] (le préimage) n'a
// AUCUNE contrainte de bord publique, un prouveur peut produire une preuve
// ACCEPTÉE pour un digest ARBITRAIRE de son choix — il lui suffit d'inverser la
// chaîne pour trouver le préimage correspondant. La preuve atteste donc seulement
// « je connais UN préimage de chaîne S-box menant à ce digest », ce qui est VRAI
// pour TOUT élément du corps : elle ne lie le digest à aucune donnée publique.
//
// Ce n'est PAS une faille de soundness du STARK (la preuve est honnête pour la
// relation prouvée) ; c'est l'AVERTISSEMENT que la relation prouvée est, seule,
// dénuée de portée — il faut un contexte (ex. lier t[0] à un engagement public)
// pour en faire un énoncé utile. À garder en tête avant tout usage.
func TestSboxAIRDigestArbitraireAccepte(t *testing.T) {
	n := 16
	for _, cibleU := range []uint64{0xDEADBEEF, 1, 0, 0xFFFFFFFF00000000} {
		cible := FromUint64(cibleU)
		// Préimage = (sbox^{-1})^{n-1}(cible).
		pre := cible
		for i := 0; i < n-1; i++ {
			pre = sboxInverse(pre)
		}
		digest, proof := ProveHash(pre, n)
		if !digest.Equal(cible) {
			t.Fatalf("inversion S-box incorrecte : cible=%d obtenu=%d", cibleU, digest.Uint64())
		}
		if !VerifyHash(digest, proof) {
			t.Fatalf("la preuve d'un digest arbitraire (%d) devrait être acceptée (statement vacueux)", cibleU)
		}
	}
	t.Log("CONSTAT : VerifyHash accepte un digest ARBITRAIRE — la relation S-box seule ne lie le digest à rien de public (À AUDITER avant usage).")
}

// ---------------------------------------------------------------------------
// 6) AIR — SOUNDNESS PROBABILISTE FRI sur trace bruitée (near-codeword).
// ---------------------------------------------------------------------------

// forgeTraceBruitee reconstruit, à la main et en rejouant le transcript de
// ProveHash, une preuve dont le LDE de TRACE engagé est bruité à `nCorrupt`
// positions (le polynôme de trace honnête servant aux OOD/DEEP). Le préimage
// `pre` fait varier le transcript (donc les positions de requête) d'un essai à
// l'autre. Renvoie (digest, preuve forgée).
func forgeTraceBruitee(pre Felt, n, nCorrupt int) (Felt, HashProof) {
	g := RootOfUnity(log2(n))
	bigN := sbBigN(n)

	trace := buildSboxTrace(pre, n)
	digest := trace[n-1]
	traceCoeffs := Interpolate(trace)
	traceLDE := evalOnLDE(traceCoeffs, bigN)

	// Bruitage de positions éparses (déterministe).
	for k := 0; k < nCorrupt; k++ {
		pos := (k*131 + 5) % bigN
		traceLDE[pos] = traceLDE[pos].Add(One())
	}
	traceRoot, traceTree := commitEvals(traceLDE)

	tr := NewTranscript(sbDomain)
	tr.AbsorbFelt("sbox/n", FromUint64(uint64(n)))
	tr.AbsorbFelt("sbox/blowup", FromUint64(uint64(sbBlowup)))
	tr.AbsorbFelt("sbox/num-queries", FromUint64(uint64(sbNumQueries)))
	tr.AbsorbFelt("sbox/exp", FromUint64(uint64(sbSboxExp)))
	tr.AbsorbFelt("sbox/digest", digest)
	tr.Absorb("sbox/trace-root", traceRoot[:])
	alpha := drawSboxAlphas(tr)

	compCoeffs := buildSboxComposition(traceCoeffs, g, n, digest, alpha)
	compLDE := evalOnLDE(compCoeffs, bigN)
	compRoot, compTree := commitEvals(compLDE)
	tr.Absorb("sbox/comp-root", compRoot[:])

	z := tr.Challenge("sbox/ood-z")
	gz := g.Mul(z)
	oodTz := evalNaïfPoly(traceCoeffs, z)
	oodTgz := evalNaïfPoly(traceCoeffs, gz)
	oodHz := evalNaïfPoly(compCoeffs, z)
	tr.AbsorbFelt("sbox/ood-tz", oodTz)
	tr.AbsorbFelt("sbox/ood-tgz", oodTgz)
	tr.AbsorbFelt("sbox/ood-hz", oodHz)
	gamma := drawSboxGammas(tr)

	deepCoeffs := buildSboxDeep(traceCoeffs, compCoeffs, z, gz, oodTz, oodTgz, oodHz, gamma)
	deepLDE := evalOnLDE(deepCoeffs, bigN)
	friProof := proveFRISbox(deepLDE)
	deepTree := buildDeepTree(deepLDE)

	absorbFriDigest(tr, friProof)
	positions := tr.ChallengeIndices("sbox/query", sbNumQueries, bigN)
	openings := make([]HashOpening, len(positions))
	for i, pos := range positions {
		openings[i] = HashOpening{
			Pos: pos, TraceVal: traceLDE[pos], TracePath: Open(traceTree, pos),
			CompVal: compLDE[pos], CompPath: Open(compTree, pos),
			DeepVal: deepLDE[pos], DeepPath: Open(deepTree, pos),
		}
	}
	return digest, HashProof{
		N: n, TraceRoot: traceRoot, CompRoot: compRoot,
		OodTz: oodTz, OodTgz: oodTgz, OodHz: oodHz,
		Fri: friProof, Openings: openings,
	}
}

// TestSboxTraceCorruptionMassiveRejetee : une trace dont le LDE engagé est
// corrompu sur la MOITIÉ du domaine est rejetée à TOUS les essais (FRI + la
// recombinaison DEEP des ouvertures échouent quasi sûrement). C'est le pendant
// POSITIF de la soundness : une déviation MACROSCOPIQUE du codeword est attrapée.
func TestSboxTraceCorruptionMassiveRejetee(t *testing.T) {
	n := 16
	bigN := sbBigN(n)
	for k := 0; k < 16; k++ {
		pre := FromUint64(uint64(7777*k + 3))
		digest, proof := forgeTraceBruitee(pre, n, bigN/2) // 50 % corrompu
		if VerifyHash(digest, proof) {
			t.Fatalf("SOUNDNESS : trace corrompue à 50%% acceptée (essai %d)", k)
		}
	}
}

// TestSboxTraceNearCodewordSoundnessProbabiliste DOCUMENTE la soundness
// PROBABILISTE : une trace bruitée à UNE SEULE position (near-codeword, distance
// de Hamming 1 sur bigN) est acceptée la PLUPART du temps, car les sbNumQueries
// positions interrogées ne tombent que rarement sur l'unique position corrompue.
//
// CE N'EST PAS UN BUG : c'est l'erreur de soundness intrinsèque à FRI avec un
// faible nombre de requêtes. Le test mesure le taux et VÉRIFIE qu'il est cohérent
// avec la borne (1 - 1/bigN)^queries (donc proche de 1). Il sert d'AVERTISSEMENT
// quantitatif : sbBlowup=8 / sbNumQueries=32 ne donnent PAS une rétention forte
// contre les corruptions MINIMES ; calibrer ces paramètres (plus de requêtes,
// borne de distance) est un point À AUDITER avant tout usage sérieux.
func TestSboxTraceNearCodewordSoundnessProbabiliste(t *testing.T) {
	n := 16
	bigN := sbBigN(n)
	trials := 120
	accepts := 0
	for k := 0; k < trials; k++ {
		pre := FromUint64(uint64(1000003*k + 7))
		digest, proof := forgeTraceBruitee(pre, n, 1)
		if VerifyHash(digest, proof) {
			accepts++
		}
	}
	rate := float64(accepts) / float64(trials)

	// Borne théorique : la probabilité de RATER l'unique position corrompue est
	// (1 - 1/bigN)^queries. On vérifie que le taux observé est cohérent (élevé) ;
	// un taux ANORMALEMENT BAS signalerait au contraire que la corruption d'1
	// position est sur-détectée (improbable) — ici on borne juste par le bas pour
	// éviter un faux échec de flakiness, l'objet étant de DOCUMENTER, pas d'exiger.
	t.Logf("CONSTAT soundness probabiliste : trace bruitée 1 pos sur bigN=%d, queries=%d => %d/%d acceptées (%.1f%%). "+
		"C'est l'erreur de soundness FRI attendue ; calibrer blowup/queries À AUDITER.",
		bigN, sbNumQueries, accepts, trials, 100*rate)

	// Garde-fou minimal anti-régression : avec ces paramètres le taux DOIT rester
	// très majoritaire (sinon le mécanisme de requête a changé de nature).
	if rate < 0.80 {
		t.Fatalf("taux d'acceptation near-codeword inattendu : %.1f%% (attendu ~%.1f%%) — "+
			"le mécanisme de requête a-t-il changé ?",
			100*rate, 100*pow(1-1.0/float64(bigN), sbNumQueries))
	}
}

// pow calcule base^exp en flottant (exp petit), pour l'affichage de la borne.
func pow(base float64, exp int) float64 {
	r := 1.0
	for i := 0; i < exp; i++ {
		r *= base
	}
	return r
}
