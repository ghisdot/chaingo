// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Tests de SOUNDNESS « adverses » avancés, ajoutés lors de la vérification
// adverse (étage 5). Ils vont au-delà des falsifications ponctuelles d'octets
// (couvertes par stark_test.go) : ils simulent un PROUVEUR MALHONNÊTE qui
// reconstruit une preuve cohérente avec une intention de tricher. Chaque test
// confirme que la barrière de soundness (liaison DEEP/FRI ou engagement avant
// défis) rejette la tentative.
//
// Toutes ces preuves forgées sont construites à la main en rejouant le même
// transcript Fiat-Shamir que ProveFib, afin que les défis (alpha, z, gamma,
// positions) soient ceux que VerifyFib recalculera : la forgerie est donc
// « bien formée » du point de vue du transcript, et seul le cœur cryptographique
// peut la rejeter.
package stark

import "testing"

// TestForgerieOodMensongerCohérentEnZ est l'attaque la plus fine : un prouveur
// annonce des valeurs hors-domaine FAUSSES (OodTz/OodHz décalés) mais ajustées
// pour satisfaire EXACTEMENT l'identité de contrainte checkConstraintsAtZ au
// point z. Le contrôle algébrique en z passe alors ; la barrière restante est la
// liaison DEEP/FRI : le quotient DEEP construit avec un faux T(z) n'est pas un
// vrai polynôme (la division (T(x)-fauxTz)/(x-z) a un reste), donc à chaque
// position interrogée, la recombinaison DEEP exacte du vérifieur ne coïncide pas
// avec la valeur DEEP engagée. La preuve DOIT être rejetée.
func TestForgerieOodMensongerCohérentEnZ(t *testing.T) {
	n := 16
	out := fibValue(n)
	logN := log2(n)
	g := RootOfUnity(logN)
	bigN := starkBlowup * n

	// Trace honnête et son engagement LDE.
	trace := buildTrace(n)
	traceCoeffs := Interpolate(trace)
	traceLDE := evalOnLDE(traceCoeffs, bigN)
	traceRoot, traceTree := commitEvals(traceLDE)

	// Rejoue le transcript du prouveur jusqu'à z.
	tr := NewTranscript(starkDomain)
	tr.AbsorbFelt("fib/n", FromUint64(uint64(n)))
	tr.AbsorbFelt("fib/blowup", FromUint64(uint64(starkBlowup)))
	tr.AbsorbFelt("fib/num-queries", FromUint64(uint64(starkNumQueries)))
	tr.AbsorbFelt("fib/public-output", out)
	tr.Absorb("fib/trace-root", traceRoot[:])
	alpha := drawAlphas(tr)

	compCoeffs := buildComposition(traceCoeffs, g, n, out, alpha)
	compLDE := evalOnLDE(compCoeffs, bigN)
	compRoot, compTree := commitEvals(compLDE)
	tr.Absorb("fib/comp-root", compRoot[:])

	z := tr.Challenge("fib/ood-z")
	gz := g.Mul(z)
	g2z := g.Mul(gz)
	realTgz := evalNaïfPoly(traceCoeffs, gz)
	realTg2z := evalNaïfPoly(traceCoeffs, g2z)
	realTz := evalNaïfPoly(traceCoeffs, z)

	// Mensonge : Tz décalé de 1. On calcule le Hz qui rend l'identité de
	// contrainte vraie en z pour ce faux Tz (les autres OOD honnêtes).
	fakeTz := realTz.Add(One())
	one := One()
	gN1 := g.Exp(uint64(n - 1))
	gN2 := g.Exp(uint64(n - 2))
	zn := z.Exp(uint64(n))
	qTrans := realTg2z.Sub(realTgz).Sub(fakeTz).
		Mul(z.Sub(gN2)).Mul(z.Sub(gN1)).Div(zn.Sub(one))
	qB0 := fakeTz.Sub(one).Div(z.Sub(one))
	qB1 := fakeTz.Sub(one).Div(z.Sub(g))
	qBn := fakeTz.Sub(out).Div(z.Sub(gN1))
	fakeHz := alpha[0].Mul(qTrans).Add(alpha[1].Mul(qB0)).
		Add(alpha[2].Mul(qB1)).Add(alpha[3].Mul(qBn))

	// Pré-condition de l'attaque : l'identité en z DOIT être satisfaite par le
	// faux couple, sinon on ne teste pas la bonne barrière.
	if !checkConstraintsAtZ(z, g, n, out, alpha, fakeTz, realTgz, realTg2z, fakeHz) {
		t.Fatal("setup invalide : le faux (Tz,Hz) ne satisfait pas l'identité en z")
	}

	// Suite du transcript avec les FAUX OOD.
	tr.AbsorbFelt("fib/ood-tz", fakeTz)
	tr.AbsorbFelt("fib/ood-tgz", realTgz)
	tr.AbsorbFelt("fib/ood-tg2z", realTg2z)
	tr.AbsorbFelt("fib/ood-hz", fakeHz)
	gamma := drawGammas(tr)

	// Quotient DEEP construit avec le faux Tz (division non exacte, reste ignoré).
	deepCoeffs := buildDeep(traceCoeffs, compCoeffs, z, gz, g2z,
		fakeTz, realTgz, realTg2z, fakeHz, gamma)
	deepLDE := evalOnLDE(deepCoeffs, bigN)
	friProof := proveFRI(deepLDE)
	deepTree := buildDeepTree(deepLDE)

	absorbFriDigest(tr, friProof)
	positions := tr.ChallengeIndices("fib/query", starkNumQueries, bigN)
	openings := make([]FibOpening, len(positions))
	for i, pos := range positions {
		openings[i] = FibOpening{
			Pos:       pos,
			TraceVal:  traceLDE[pos],
			TracePath: Open(traceTree, pos),
			CompVal:   compLDE[pos],
			CompPath:  Open(compTree, pos),
			DeepVal:   deepLDE[pos],
			DeepPath:  Open(deepTree, pos),
		}
	}

	forged := FibProof{
		TraceRoot: traceRoot, CompRoot: compRoot,
		OodTz: fakeTz, OodTgz: realTgz, OodTg2z: realTg2z, OodHz: fakeHz,
		Fri: friProof, Openings: openings,
	}

	if VerifyFib(forged, n, out) {
		t.Fatal("SOUNDNESS : OOD mensonger (cohérent en z) accepté — liaison DEEP/FRI défaillante")
	}
}

// TestForgerieTraceHautDegré simule un prouveur qui engage une trace dont le
// polynôme interpolé n'est PAS de degré < n : il corrompt un point du LDE de
// trace, ce qui porte le degré à N-1. Comme la racine de trace est absorbée
// AVANT tous les défis, recoudre les ouvertures contre l'arbre corrompu ne suffit
// pas : z, gamma et les positions divergent de la preuve honnête. Rejet attendu.
func TestForgerieTraceHautDegré(t *testing.T) {
	n := 16
	out := fibValue(n)
	bigN := starkBlowup * n

	honnête := ProveFib(n, out)
	if !VerifyFib(honnête, n, out) {
		t.Fatal("contrôle : preuve honnête rejetée")
	}

	trace := buildTrace(n)
	traceCoeffs := Interpolate(trace)
	traceLDE := evalOnLDE(traceCoeffs, bigN)

	// Corruption d'un point -> trace de degré plein.
	badLDE := clonePoly(traceLDE)
	badLDE[7] = badLDE[7].Add(One())
	badRoot, badTree := commitEvals(badLDE)

	bad := cloneFibProof(honnête)
	bad.TraceRoot = badRoot
	for i := range bad.Openings {
		pos := bad.Openings[i].Pos
		bad.Openings[i].TraceVal = badLDE[pos]
		bad.Openings[i].TracePath = Open(badTree, pos)
	}
	if VerifyFib(bad, n, out) {
		t.Fatal("SOUNDNESS : trace de haut degré (LDE corrompu) acceptée")
	}
}

// TestForgerieGreffeFriIncohérente : on remplace toute la preuve FRI interne par
// une preuve FRI honnête d'un AUTRE polynôme de bas degré, et on recoud les
// ouvertures DEEP contre la nouvelle racine. La preuve FRI vérifie isolément,
// mais (a) les positions, dépendant de absorbFriDigest, changent, et (b) la
// recombinaison DEEP des ouvertures trace/comp honnêtes ne donne pas le nouveau
// P. Rejet attendu.
func TestForgerieGreffeFriIncohérente(t *testing.T) {
	n := 16
	out := fibValue(n)
	bigN := starkBlowup * n

	honnête := ProveFib(n, out)

	// Polynôme de bas degré arbitraire, sans rapport avec la trace.
	rng := newPRNG(0xBADF1)
	fakeCoeffs := make([]Felt, n-1)
	for i := range fakeCoeffs {
		fakeCoeffs[i] = rng.felt()
	}
	fakeLDE := evalOnLDE(fakeCoeffs, bigN)
	fakeFri := proveFRI(fakeLDE)
	if !Verify(fakeFri, FriParams{Blowup: starkBlowup, NumQueries: starkNumQueries}) {
		t.Fatal("setup : la preuve FRI greffée devait vérifier isolément")
	}
	_, fakeTree := commitEvals(fakeLDE)

	bad := cloneFibProof(honnête)
	bad.Fri = fakeFri
	for i := range bad.Openings {
		pos := bad.Openings[i].Pos
		bad.Openings[i].DeepVal = fakeLDE[pos]
		bad.Openings[i].DeepPath = Open(fakeTree, pos)
	}
	if VerifyFib(bad, n, out) {
		t.Fatal("SOUNDNESS : greffe d'une preuve FRI étrangère acceptée")
	}
}

// TestForgerieRejouerPreuveAutreInstance : une preuve produite pour n=32 ne doit
// JAMAIS vérifier pour une autre instance (n différent OU même n mais autre
// sortie), même si la structure FRI est compatible. C'est l'anti-rejeu : la
// sortie publique et n sont absorbés en tête du transcript.
func TestForgerieRejouerPreuveAutreInstance(t *testing.T) {
	p32 := ProveFib(32, fibValue(32))

	// Réutiliser la preuve de n=32 pour une AUTRE sortie publique (même n).
	autreSortie := fibValue(32).Add(FromUint64(12345))
	if VerifyFib(p32, 32, autreSortie) {
		t.Fatal("SOUNDNESS : preuve rejouée acceptée pour une autre sortie publique")
	}

	// Réutiliser pour un autre n dont la structure FRI est identique ? n=32 a
	// bigN=256 ; aucune autre puissance de 2 ne donne la même structure tout en
	// gardant un transcript cohérent. On teste n voisins.
	for _, m := range []int{16, 64} {
		if VerifyFib(p32, m, fibValue(32)) {
			t.Fatalf("SOUNDNESS : preuve de n=32 acceptée pour n=%d", m)
		}
	}
}
