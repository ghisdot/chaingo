// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Tests de la NTT : NTT∘INTT = identité, NTT == évaluation naïve sur petites
// tailles, multiplication de polynômes contre la convolution scolaire, et
// tests de SOUNDNESS négatifs (résultats falsifiés rejetés). Déterministe.
package stark

import "testing"

// evalNaïf évalue le polynôme coeffs (ordre croissant des degrés) au point x
// par schéma de Horner. Référence indépendante de la NTT.
func evalNaïf(coeffs []Felt, x Felt) Felt {
	acc := Zero()
	for i := len(coeffs) - 1; i >= 0; i-- {
		acc = acc.Mul(x).Add(coeffs[i])
	}
	return acc
}

// convNaïve effectue la multiplication de polynômes par convolution directe
// O(n*m). Référence indépendante de MulPoly.
func convNaïve(a, b []Felt) []Felt {
	if len(a) == 0 || len(b) == 0 {
		return []Felt{}
	}
	res := make([]Felt, len(a)+len(b)-1)
	for i := range a {
		for j := range b {
			res[i+j] = res[i+j].Add(a[i].Mul(b[j]))
		}
	}
	return res
}

func clone(a []Felt) []Felt {
	c := make([]Felt, len(a))
	copy(c, a)
	return c
}

// TestNTTContreÉvaluationNaïve vérifie que NTT[i] == P(ω^i) pour de petites
// tailles, en comparant à l'évaluation de Horner.
func TestNTTContreÉvaluationNaïve(t *testing.T) {
	rng := newPRNG(100)
	for _, logN := range []uint32{0, 1, 2, 3, 4, 6} {
		n := 1 << logN
		coeffs := make([]Felt, n)
		for i := range coeffs {
			coeffs[i] = rng.felt()
		}
		w := RootOfUnity(logN)

		got := clone(coeffs)
		NTT(got)

		// Référence : P(ω^i) pour i = 0..n-1.
		wi := One()
		for i := 0; i < n; i++ {
			want := evalNaïf(coeffs, wi)
			if !got[i].Equal(want) {
				t.Fatalf("NTT KO logN=%d i=%d : got=%d want=%d", logN, i, got[i], want)
			}
			wi = wi.Mul(w)
		}
	}
}

// TestNTTPuisINTTIdentité : INTT(NTT(a)) == a et NTT(INTT(a)) == a.
func TestNTTPuisINTTIdentité(t *testing.T) {
	rng := newPRNG(101)
	for _, logN := range []uint32{0, 1, 2, 3, 5, 8, 10, 12} {
		n := 1 << logN
		orig := make([]Felt, n)
		for i := range orig {
			orig[i] = rng.felt()
		}

		// INTT∘NTT.
		a := clone(orig)
		NTT(a)
		INTT(a)
		for i := range a {
			if !a[i].Equal(orig[i]) {
				t.Fatalf("INTT(NTT) != id logN=%d i=%d : got=%d want=%d", logN, i, a[i], orig[i])
			}
		}

		// NTT∘INTT.
		b := clone(orig)
		INTT(b)
		NTT(b)
		for i := range b {
			if !b[i].Equal(orig[i]) {
				t.Fatalf("NTT(INTT) != id logN=%d i=%d : got=%d want=%d", logN, i, b[i], orig[i])
			}
		}
	}
}

// TestEvaluateInterpolateAllerRetour : Interpolate(Evaluate(p)) redonne p
// (complété à la puissance de 2).
func TestEvaluateInterpolateAllerRetour(t *testing.T) {
	rng := newPRNG(102)
	for _, deg := range []int{1, 2, 3, 5, 7, 16, 17, 31, 64} {
		coeffs := make([]Felt, deg)
		for i := range coeffs {
			coeffs[i] = rng.felt()
		}
		evals := Evaluate(coeffs)
		back := Interpolate(evals)
		// back est de longueur nextPow2(deg) ; les premiers deg coeffs doivent
		// coïncider, le reste doit être nul.
		for i := range back {
			var want Felt
			if i < deg {
				want = coeffs[i]
			}
			if !back[i].Equal(want) {
				t.Fatalf("aller-retour eval/interp KO deg=%d i=%d : got=%d want=%d", deg, i, back[i], want)
			}
		}
	}
}

// TestMulPolyContreConvolution : MulPoly == convolution naïve.
func TestMulPolyContreConvolution(t *testing.T) {
	rng := newPRNG(103)
	for iter := 0; iter < 200; iter++ {
		la := 1 + int(rng.next()%40)
		lb := 1 + int(rng.next()%40)
		a := make([]Felt, la)
		b := make([]Felt, lb)
		for i := range a {
			a[i] = rng.felt()
		}
		for i := range b {
			b[i] = rng.felt()
		}
		got := MulPoly(a, b)
		want := convNaïve(a, b)
		if len(got) != len(want) {
			t.Fatalf("MulPoly longueur %d != %d (la=%d lb=%d)", len(got), len(want), la, lb)
		}
		for i := range want {
			if !got[i].Equal(want[i]) {
				t.Fatalf("MulPoly KO i=%d : got=%d want=%d (la=%d lb=%d)", i, got[i], want[i], la, lb)
			}
		}
	}
}

func TestMulPolyCasVides(t *testing.T) {
	if got := MulPoly(nil, []Felt{One()}); len(got) != 0 {
		t.Fatalf("MulPoly(nil, x) devrait être vide, got len=%d", len(got))
	}
	if got := MulPoly([]Felt{One()}, nil); len(got) != 0 {
		t.Fatalf("MulPoly(x, nil) devrait être vide, got len=%d", len(got))
	}
}

// TestMulPolyPropriétéNeutre : multiplier par le polynôme constant 1 redonne a.
func TestMulPolyNeutre(t *testing.T) {
	rng := newPRNG(104)
	a := make([]Felt, 13)
	for i := range a {
		a[i] = rng.felt()
	}
	got := MulPoly(a, []Felt{One()})
	if len(got) != len(a) {
		t.Fatalf("longueur changée : %d != %d", len(got), len(a))
	}
	for i := range a {
		if !got[i].Equal(a[i]) {
			t.Fatalf("a*1 != a à i=%d", i)
		}
	}
}

// ---------------------------------------------------------------------------
// SOUNDNESS négatif : un transcript / résultat falsifié DOIT être rejeté.
// ---------------------------------------------------------------------------

// TestSoundnessNTTFalsifiée : si on altère un seul coefficient avant la NTT, la
// sortie diffère de la référence d'évaluation — on ne doit JAMAIS accepter une
// NTT « presque » correcte comme égale. Ce test garantit que notre comparaison
// détecte bien une falsification (pas de faux positif d'acceptation).
func TestSoundnessNTTFalsifiée(t *testing.T) {
	rng := newPRNG(200)
	logN := uint32(6)
	n := 1 << logN
	coeffs := make([]Felt, n)
	for i := range coeffs {
		coeffs[i] = rng.felt()
	}
	w := RootOfUnity(logN)

	// Évaluation correcte.
	correct := clone(coeffs)
	NTT(correct)

	// Évaluation FALSIFIÉE : on corrompt un coefficient puis on transforme.
	tampered := clone(coeffs)
	tampered[3] = tampered[3].Add(One()) // +1 sur un coeff
	NTT(tampered)

	// Une falsification de coefficient se propage : au moins une évaluation
	// doit différer (en réalité toutes, car ω^i != 0).
	diffCount := 0
	for i := 0; i < n; i++ {
		ref := evalNaïf(coeffs, powFelt(w, i))
		if !correct[i].Equal(ref) {
			t.Fatalf("la NTT correcte ne matche pas l'évaluation de référence à i=%d", i)
		}
		if !tampered[i].Equal(correct[i]) {
			diffCount++
		}
	}
	if diffCount == 0 {
		t.Fatal("SOUNDNESS : une NTT falsifiée a produit exactement la même sortie — inacceptable")
	}
}

// TestSoundnessInterpolationFausseRejetée : si on prétend qu'un jeu
// d'évaluations interpole un polynôme donné mais qu'on a corrompu une
// évaluation, l'interpolation obtenue NE DOIT PAS coïncider avec le polynôme
// d'origine. Vérifie qu'on ne peut pas « tricher » sur les valeurs.
func TestSoundnessInterpolationFausseRejetée(t *testing.T) {
	rng := newPRNG(201)
	logN := uint32(5)
	n := 1 << logN
	coeffs := make([]Felt, n)
	for i := range coeffs {
		coeffs[i] = rng.felt()
	}

	evals := Evaluate(coeffs)

	// On corrompt une évaluation (falsification du « témoin »).
	evals[7] = evals[7].Add(One())

	recovered := Interpolate(evals)

	// Le polynôme reconstruit doit DIFFÉRER de l'original : sinon on aurait pu
	// falsifier une valeur sans changer le polynôme, ce qui briserait la
	// soundness (l'interpolation est une bijection).
	identique := true
	for i := range coeffs {
		if !recovered[i].Equal(coeffs[i]) {
			identique = false
			break
		}
	}
	if identique {
		t.Fatal("SOUNDNESS : évaluation corrompue a donné le même polynôme — bijection violée")
	}
}

// TestSoundnessMulPolyFalsifiée : un produit de polynômes falsifié (un coeff
// modifié) doit être rejeté par la comparaison à la convolution de référence.
func TestSoundnessMulPolyFalsifiée(t *testing.T) {
	rng := newPRNG(202)
	a := make([]Felt, 10)
	b := make([]Felt, 12)
	for i := range a {
		a[i] = rng.felt()
	}
	for i := range b {
		b[i] = rng.felt()
	}
	correct := MulPoly(a, b)

	// Produit « prétendu » falsifié.
	forged := clone(correct)
	forged[5] = forged[5].Add(One())

	ref := convNaïve(a, b)
	// Le vrai produit matche la référence...
	for i := range ref {
		if !correct[i].Equal(ref[i]) {
			t.Fatalf("MulPoly correct ne matche pas la convolution à i=%d", i)
		}
	}
	// ...mais le falsifié NON : la vérification doit donc l'attraper.
	matchForged := true
	for i := range ref {
		if !forged[i].Equal(ref[i]) {
			matchForged = false
			break
		}
	}
	if matchForged {
		t.Fatal("SOUNDNESS : un produit falsifié a passé la vérification — inacceptable")
	}
}

// powFelt : ω^i par multiplications successives (utilitaire de test, évite de
// dépendre de Exp pour la référence d'évaluation).
func powFelt(w Felt, i int) Felt {
	r := One()
	for k := 0; k < i; k++ {
		r = r.Mul(w)
	}
	return r
}

// TestNTTTailleNonPuissanceDe2Panique : robustesse de l'API.
func TestNTTTailleNonPuissanceDe2Panique(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NTT sur taille 3 aurait dû paniquer")
		}
	}()
	NTT(make([]Felt, 3))
}

func TestNextPow2(t *testing.T) {
	cases := map[int]int{0: 1, 1: 1, 2: 2, 3: 4, 4: 4, 5: 8, 17: 32, 64: 64, 65: 128}
	for in, want := range cases {
		if got := nextPow2(in); got != want {
			t.Fatalf("nextPow2(%d) = %d, want %d", in, got, want)
		}
	}
}
