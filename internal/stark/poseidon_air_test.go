// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Tests du STARK de hachage S-box (poseidon_air.go) de bout en bout. On vérifie :
//   - CHEMIN HEUREUX : une preuve honnête (bon digest) VÉRIFIE, pour plusieurs
//     tailles de chaîne n et plusieurs préimages.
//   - COHÉRENCE : le digest renvoyé par ProveHash == SboxDigest (oracle).
//   - SOUNDNESS (mauvais digest public) : vérifier contre un digest faux ÉCHOUE.
//   - SOUNDNESS (falsification) : toute altération de la preuve (racines, valeurs
//     hors-domaine, ouvertures, chemins de Merkle, n, FRI, positions) ÉCHOUE.
//   - SOUNDNESS (forgerie OOD cohérente en z) : un faux (Tz,Hz) ajusté pour
//     passer le contrôle algébrique en z est rejeté par la liaison DEEP/FRI.
//   - SOUNDNESS (trace mal calculée) : une trace qui dévie de la S-box (chaîne
//     incorrecte) ne produit pas de preuve acceptée pour son « digest » final.
//   - DÉTERMINISME : deux preuves de la même instance sont identiques bit-à-bit.
//   - ROBUSTESSE : VerifyHash ne panique jamais ; ProveHash panique sur n invalide.
//
// Déterministe : aucune dépendance à time / math/rand. PRNG à graine fixe et
// helpers de clonage (clone32 de merkle_test.go, cloneProof de fri_test.go).
package stark

import "testing"

// ---------------------------------------------------------------------------
// Chemin heureux et cohérence du digest.
// ---------------------------------------------------------------------------

// TestSboxHonnêteVérifie : pour diverses tailles n et préimages, une preuve
// produite avec le VRAI digest doit être acceptée.
func TestSboxHonnêteVérifie(t *testing.T) {
	rng := newPRNG(0x5B0C0DE5)
	for _, n := range []int{4, 8, 16, 32} {
		for k := 0; k < 4; k++ {
			pre := rng.felt()
			digest, proof := ProveHash(pre, n)

			// Cohérence : le digest annoncé doit être l'oracle S-box.
			if !digest.Equal(SboxDigest(pre, n)) {
				t.Fatalf("digest != SboxDigest pour n=%d", n)
			}
			if !VerifyHash(digest, proof) {
				t.Fatalf("preuve honnête rejetée pour n=%d (preimage=%d)", n, pre.Uint64())
			}
		}
	}
}

// TestSboxPreimageNonRévélé : le préimage ne doit apparaître NULLE PART dans la
// preuve en clair (ni TraceRoot bien sûr — c'est un hash —, mais surtout pas dans
// les valeurs hors-domaine ni les ouvertures, qui sont des évaluations en des
// points distincts du domaine). On vérifie que t[0] (le préimage) ne figure pas
// tel quel parmi les valeurs exposées. C'est une garantie FAIBLE (le vrai ZK
// exigerait un masquage) mais elle documente l'intention.
func TestSboxPreimageNonRévélé(t *testing.T) {
	pre := FromUint64(0xDEADBEEFCAFE)
	digest, proof := ProveHash(pre, 16)
	if !VerifyHash(digest, proof) {
		t.Fatal("contrôle : preuve honnête doit passer")
	}
	if proof.OodTz.Equal(pre) || proof.OodTgz.Equal(pre) {
		t.Fatal("le préimage apparaît dans une valeur hors-domaine")
	}
	for _, op := range proof.Openings {
		if op.TraceVal.Equal(pre) {
			// Coïncidence improbable mais possible ; on le signale comme info.
			t.Logf("note : une valeur de trace ouverte égale le préimage (coïncidence de domaine)")
		}
	}
}

// ---------------------------------------------------------------------------
// SOUNDNESS : mauvais digest public.
// ---------------------------------------------------------------------------

// TestSboxMauvaisDigestÉchoue : vérifier une preuve honnête contre un digest
// FAUX doit échouer (le digest est absorbé en tête du transcript : il diverge).
func TestSboxMauvaisDigestÉchoue(t *testing.T) {
	rng := newPRNG(0xBAD016E57)
	for _, n := range []int{8, 16, 32} {
		pre := rng.felt()
		digest, proof := ProveHash(pre, n)
		faux := digest.Add(One())
		if VerifyHash(faux, proof) {
			t.Fatalf("SOUNDNESS : preuve acceptée contre un digest faux pour n=%d", n)
		}
	}
}

// TestSboxTraceIncorrecteÉchoue : un prouveur qui engage une trace NE respectant
// PAS la récurrence S-box (chaîne « cassée » à un pas) ne doit pas obtenir une
// preuve acceptée pour le digest final de cette trace. On reconstruit une preuve
// à la main avec une trace altérée, en rejouant le transcript de ProveHash.
func TestSboxTraceIncorrecteÉchoue(t *testing.T) {
	n := 16
	g := RootOfUnity(log2(n))
	bigN := sbBigN(n)

	pre := FromUint64(7)
	// Trace honnête puis CORRUPTION d'une cellule intermédiaire : la transition
	// S-box t[k] = t[k-1]^7 est alors violée autour de k.
	trace := buildSboxTrace(pre, n)
	trace[n/2] = trace[n/2].Add(One()) // chaîne cassée
	digest := trace[n-1]               // « digest » de la trace corrompue

	traceCoeffs := Interpolate(trace)
	traceLDE := evalOnLDE(traceCoeffs, bigN)
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
	forged := HashProof{
		N: n, TraceRoot: traceRoot, CompRoot: compRoot,
		OodTz: oodTz, OodTgz: oodTgz, OodHz: oodHz,
		Fri: friProof, Openings: openings,
	}

	// La composition n'est pas un vrai polynôme de bas degré (la transition est
	// violée) : H a un degré PLEIN, donc FRI rejette le quotient DEEP correspondant.
	if VerifyHash(digest, forged) {
		t.Fatal("SOUNDNESS : preuve d'une chaîne S-box cassée acceptée")
	}
}

// ---------------------------------------------------------------------------
// SOUNDNESS : falsification ponctuelle d'une preuve honnête.
// ---------------------------------------------------------------------------

// honnêteHash construit une preuve honnête de référence pour n donné.
func honnêteHash(n int) (Felt, HashProof) {
	pre := FromUint64(uint64(n) * 1000003)
	digest, proof := ProveHash(pre, n)
	return digest, proof
}

func TestSboxRacineTraceFalsifiée(t *testing.T) {
	digest, proof := honnêteHash(32)
	if !VerifyHash(digest, proof) {
		t.Fatal("contrôle : preuve honnête doit passer")
	}
	bad := cloneHashProof(proof)
	bad.TraceRoot[0] ^= 0xFF
	if VerifyHash(digest, bad) {
		t.Fatal("SOUNDNESS : racine de trace falsifiée acceptée")
	}
}

func TestSboxRacineCompFalsifiée(t *testing.T) {
	digest, proof := honnêteHash(32)
	bad := cloneHashProof(proof)
	bad.CompRoot[0] ^= 0xFF
	if VerifyHash(digest, bad) {
		t.Fatal("SOUNDNESS : racine de composition falsifiée acceptée")
	}
}

func TestSboxOodFalsifiée(t *testing.T) {
	digest, proof := honnêteHash(32)

	bad := cloneHashProof(proof)
	bad.OodTz = bad.OodTz.Add(One())
	if VerifyHash(digest, bad) {
		t.Fatal("SOUNDNESS : T(z) falsifié accepté")
	}

	bad2 := cloneHashProof(proof)
	bad2.OodTgz = bad2.OodTgz.Add(One())
	if VerifyHash(digest, bad2) {
		t.Fatal("SOUNDNESS : T(g·z) falsifié accepté")
	}

	bad3 := cloneHashProof(proof)
	bad3.OodHz = bad3.OodHz.Add(One())
	if VerifyHash(digest, bad3) {
		t.Fatal("SOUNDNESS : H(z) falsifié accepté")
	}
}

func TestSboxValeurOuverteFalsifiée(t *testing.T) {
	digest, proof := honnêteHash(32)

	bad := cloneHashProof(proof)
	bad.Openings[0].TraceVal = bad.Openings[0].TraceVal.Add(One())
	if VerifyHash(digest, bad) {
		t.Fatal("SOUNDNESS : valeur de trace ouverte falsifiée acceptée")
	}

	bad2 := cloneHashProof(proof)
	bad2.Openings[0].CompVal = bad2.Openings[0].CompVal.Add(One())
	if VerifyHash(digest, bad2) {
		t.Fatal("SOUNDNESS : valeur de composition ouverte falsifiée acceptée")
	}

	bad3 := cloneHashProof(proof)
	bad3.Openings[0].DeepVal = bad3.Openings[0].DeepVal.Add(One())
	if VerifyHash(digest, bad3) {
		t.Fatal("SOUNDNESS : valeur DEEP ouverte falsifiée acceptée")
	}
}

func TestSboxCheminFalsifié(t *testing.T) {
	digest, proof := honnêteHash(32)

	bad := cloneHashProof(proof)
	if len(bad.Openings[0].TracePath) == 0 {
		t.Skip("chemin vide")
	}
	bad.Openings[0].TracePath[0][0] ^= 0xFF
	if VerifyHash(digest, bad) {
		t.Fatal("SOUNDNESS : chemin de Merkle de trace falsifié accepté")
	}

	bad2 := cloneHashProof(proof)
	bad2.Openings[0].DeepPath[0][0] ^= 0xFF
	if VerifyHash(digest, bad2) {
		t.Fatal("SOUNDNESS : chemin de Merkle DEEP falsifié accepté")
	}
}

func TestSboxPositionFalsifiée(t *testing.T) {
	digest, proof := honnêteHash(32)
	bad := cloneHashProof(proof)
	bad.Openings[0].Pos = (bad.Openings[0].Pos + 1) % sbBigN(32)
	if VerifyHash(digest, bad) {
		t.Fatal("SOUNDNESS : position d'ouverture falsifiée acceptée")
	}
}

func TestSboxFriFalsifié(t *testing.T) {
	digest, proof := honnêteHash(32)

	bad := cloneHashProof(proof)
	bad.Fri.LayerRoots[0][0] ^= 0xFF
	if VerifyHash(digest, bad) {
		t.Fatal("SOUNDNESS : racine de couche FRI falsifiée acceptée")
	}

	bad2 := cloneHashProof(proof)
	bad2.Fri.FinalCoeffs[0] = bad2.Fri.FinalCoeffs[0].Add(One())
	if VerifyHash(digest, bad2) {
		t.Fatal("SOUNDNESS : coefficient final FRI falsifié accepté")
	}
}

func TestSboxNombreOuverturesFalsifié(t *testing.T) {
	digest, proof := honnêteHash(32)
	bad := cloneHashProof(proof)
	bad.Openings = bad.Openings[:len(bad.Openings)-1]
	if VerifyHash(digest, bad) {
		t.Fatal("SOUNDNESS : nombre d'ouvertures tronqué accepté")
	}
}

// TestSboxMauvaisNFalsifié : modifier proof.N (énoncé public absorbé) doit
// invalider la preuve (transcript divergent + structure FRI incohérente).
func TestSboxMauvaisNFalsifié(t *testing.T) {
	digest, proof := honnêteHash(16)
	bad := cloneHashProof(proof)
	bad.N = 32
	if VerifyHash(digest, bad) {
		t.Fatal("SOUNDNESS : preuve acceptée avec N falsifié (32 au lieu de 16)")
	}
	bad2 := cloneHashProof(proof)
	bad2.N = 8
	if VerifyHash(digest, bad2) {
		t.Fatal("SOUNDNESS : preuve acceptée avec N falsifié (8 au lieu de 16)")
	}
}

// ---------------------------------------------------------------------------
// SOUNDNESS adverse : OOD mensonger mais cohérent en z (cf. forgerie Fibonacci).
// ---------------------------------------------------------------------------

// TestSboxForgerieOodCohérentEnZ : un prouveur annonce un faux T(z) et un H(z)
// ajusté pour satisfaire EXACTEMENT le contrôle de contrainte en z. Le contrôle
// algébrique passe, mais le quotient DEEP construit avec le faux T(z) n'est pas
// un vrai polynôme (reste non nul), donc la recombinaison DEEP des ouvertures ne
// coïncide pas avec la valeur DEEP engagée. Rejet attendu.
func TestSboxForgerieOodCohérentEnZ(t *testing.T) {
	n := 16
	g := RootOfUnity(log2(n))
	bigN := sbBigN(n)

	pre := FromUint64(123456)
	trace := buildSboxTrace(pre, n)
	digest := trace[n-1]
	traceCoeffs := Interpolate(trace)
	traceLDE := evalOnLDE(traceCoeffs, bigN)
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
	realTz := evalNaïfPoly(traceCoeffs, z)
	realTgz := evalNaïfPoly(traceCoeffs, gz)

	// Mensonge : Tz décalé de 1, et on calcule le Hz qui rend l'identité en z vraie.
	fakeTz := realTz.Add(One())
	one := One()
	gN1 := g.Exp(uint64(n - 1))
	zn := z.Exp(uint64(n))
	qTrans := realTgz.Sub(fakeTz.Exp(sbSboxExp)).
		Mul(z.Sub(gN1)).Div(zn.Sub(one))
	qBn := fakeTz.Sub(digest).Div(z.Sub(gN1))
	fakeHz := alpha[0].Mul(qTrans).Add(alpha[1].Mul(qBn))

	if !checkSboxConstraintsAtZ(z, g, n, digest, alpha, fakeTz, realTgz, fakeHz) {
		t.Fatal("setup invalide : le faux (Tz,Hz) ne satisfait pas l'identité en z")
	}

	tr.AbsorbFelt("sbox/ood-tz", fakeTz)
	tr.AbsorbFelt("sbox/ood-tgz", realTgz)
	tr.AbsorbFelt("sbox/ood-hz", fakeHz)
	gamma := drawSboxGammas(tr)

	deepCoeffs := buildSboxDeep(traceCoeffs, compCoeffs, z, gz, fakeTz, realTgz, fakeHz, gamma)
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
	forged := HashProof{
		N: n, TraceRoot: traceRoot, CompRoot: compRoot,
		OodTz: fakeTz, OodTgz: realTgz, OodHz: fakeHz,
		Fri: friProof, Openings: openings,
	}

	if VerifyHash(digest, forged) {
		t.Fatal("SOUNDNESS : OOD mensonger (cohérent en z) accepté — liaison DEEP/FRI défaillante")
	}
}

// ---------------------------------------------------------------------------
// Déterminisme.
// ---------------------------------------------------------------------------

func TestSboxDéterminisme(t *testing.T) {
	pre := FromUint64(0xABCDEF)
	n := 16
	d1, p1 := ProveHash(pre, n)
	d2, p2 := ProveHash(pre, n)

	if !d1.Equal(d2) {
		t.Fatal("déterminisme : digest diffère")
	}
	if p1.TraceRoot != p2.TraceRoot || p1.CompRoot != p2.CompRoot {
		t.Fatal("déterminisme : racines diffèrent")
	}
	if !p1.OodTz.Equal(p2.OodTz) || !p1.OodTgz.Equal(p2.OodTgz) || !p1.OodHz.Equal(p2.OodHz) {
		t.Fatal("déterminisme : valeurs hors-domaine diffèrent")
	}
	if len(p1.Fri.LayerRoots) != len(p2.Fri.LayerRoots) {
		t.Fatal("déterminisme : nombre de racines FRI diffère")
	}
	for i := range p1.Fri.LayerRoots {
		if p1.Fri.LayerRoots[i] != p2.Fri.LayerRoots[i] {
			t.Fatalf("déterminisme : racine FRI %d diffère", i)
		}
	}
	if len(p1.Openings) != len(p2.Openings) {
		t.Fatal("déterminisme : nombre d'ouvertures diffère")
	}
	for i := range p1.Openings {
		a, b := p1.Openings[i], p2.Openings[i]
		if a.Pos != b.Pos || !a.TraceVal.Equal(b.TraceVal) ||
			!a.CompVal.Equal(b.CompVal) || !a.DeepVal.Equal(b.DeepVal) {
			t.Fatalf("déterminisme : ouverture %d diffère", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Robustesse : panique côté prouveur sur n invalide, jamais côté vérifieur.
// ---------------------------------------------------------------------------

func TestSboxProveEntréeInvalidePanique(t *testing.T) {
	mustPanic := func(name string, f func()) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("%s : panique attendue", name)
			}
		}()
		f()
	}
	mustPanic("n non pow2", func() { ProveHash(One(), 6) })
	mustPanic("n trop petit", func() { ProveHash(One(), 2) })
	mustPanic("n nul", func() { ProveHash(One(), 0) })
}

func TestSboxVerifyNeJamaisPaniquer(t *testing.T) {
	// Preuve zéro-valeur (N==0) : rejet propre, sans panique.
	if VerifyHash(One(), HashProof{}) {
		t.Fatal("preuve vide acceptée")
	}
	// N invalide : rejet propre.
	if VerifyHash(One(), HashProof{N: 6}) {
		t.Fatal("N invalide accepté")
	}
	// Preuve honnête mais digest incohérent.
	digest, proof := honnêteHash(8)
	if VerifyHash(digest.Add(One()), proof) {
		t.Fatal("digest divergent accepté")
	}
}

// ---------------------------------------------------------------------------
// Clonage profond d'une HashProof pour les tests de falsification.
// ---------------------------------------------------------------------------

// cloneHashProof effectue une copie PROFONDE d'une HashProof afin que les
// falsifications d'un test ne contaminent pas la preuve d'origine.
func cloneHashProof(p HashProof) HashProof {
	out := HashProof{
		N:         p.N,
		TraceRoot: p.TraceRoot,
		CompRoot:  p.CompRoot,
		OodTz:     p.OodTz,
		OodTgz:    p.OodTgz,
		OodHz:     p.OodHz,
		Fri:       cloneProof(p.Fri), // helper de fri_test.go
		Openings:  make([]HashOpening, len(p.Openings)),
	}
	for i := range p.Openings {
		o := p.Openings[i]
		out.Openings[i] = HashOpening{
			Pos:       o.Pos,
			TraceVal:  o.TraceVal,
			TracePath: clone32(o.TracePath), // helper de merkle_test.go
			CompVal:   o.CompVal,
			CompPath:  clone32(o.CompPath),
			DeepVal:   o.DeepVal,
			DeepPath:  clone32(o.DeepPath),
		}
	}
	return out
}
