// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Briques algébriques du STARK Fibonacci : arithmétique polynomiale en forme
// coefficients (division exacte, division par un binôme, combinaison linéaire),
// construction du polynôme de composition H, du quotient DEEP P, et contrôle
// algébrique des contraintes au point hors-domaine z. Tout est déterministe et
// n'utilise que la bibliothèque standard + golang.org/x/crypto/sha3 (via les
// briques Merkle/transcript déjà présentes).
package stark

// ---------------------------------------------------------------------------
// Arithmétique polynomiale (forme coefficients, ordre croissant des degrés)
// ---------------------------------------------------------------------------

// polyTrim retire les zéros de tête (coefficients de plus haut degré nuls) pour
// obtenir une représentation canonique du degré. Un polynôme nul devient le
// slice vide.
func polyTrim(a []Felt) []Felt {
	i := len(a)
	for i > 0 && a[i-1].IsZero() {
		i--
	}
	return a[:i]
}

// polyAddInto ajoute scale·b à acc (combinaison linéaire en place, acc agrandi au
// besoin). acc et b sont en ordre croissant des degrés. Renvoie le slice acc
// (éventuellement réalloué).
func polyAddScaled(acc, b []Felt, scale Felt) []Felt {
	if len(b) > len(acc) {
		grown := make([]Felt, len(b))
		copy(grown, acc)
		acc = grown
	}
	for i := range b {
		acc[i] = acc[i].Add(scale.Mul(b[i]))
	}
	return acc
}

// polyScale multiplie tous les coefficients par s (NOUVEAU slice).
func polyScale(a []Felt, s Felt) []Felt {
	out := make([]Felt, len(a))
	for i := range a {
		out[i] = a[i].Mul(s)
	}
	return out
}

// polySubConst renvoie a(x) - c (NOUVEAU slice) : on retire la constante c du
// terme de degré 0. Si a est vide, renvoie [-c].
func polySubConst(a []Felt, c Felt) []Felt {
	out := make([]Felt, len(a))
	copy(out, a)
	if len(out) == 0 {
		return []Felt{c.Neg()}
	}
	out[0] = out[0].Sub(c)
	return out
}

// polyDivByLinear divise a(x) par le binôme (x - c) et renvoie le quotient q
// ainsi que le reste r (constante). Schéma de Horner / Ruffini : pour
// a(x) = sum a_i x^i, le quotient q(x) = sum q_i x^i et le reste r vérifient
// a(x) = (x - c)·q(x) + r, avec :
//
//	q_{deg-1} = a_deg
//	q_{i-1}   = a_i + c·q_i        (i descendant)
//	r         = a_0 + c·q_0
//
// Division EXACTE ssi r == 0. On renvoie q (de degré deg-1) et r ; l'appelant
// décide si un reste non nul est une erreur (cas honnête) ou un signal de
// contrainte violée (cas malhonnête, qui doit produire une preuve rejetée).
func polyDivByLinear(a []Felt, c Felt) (q []Felt, r Felt) {
	a = polyTrim(a)
	if len(a) == 0 {
		return []Felt{}, Zero()
	}
	deg := len(a) - 1
	q = make([]Felt, deg) // quotient de degré deg-1 (len deg)
	// On descend des coefficients de plus haut degré vers les plus bas.
	acc := a[deg] // q_{deg-1}
	if deg >= 1 {
		q[deg-1] = acc
	}
	for i := deg - 1; i >= 1; i-- {
		acc = a[i].Add(c.Mul(acc))
		q[i-1] = acc
	}
	// Reste : r = a_0 + c·acc (avec acc = q_0 si deg>=1, sinon acc = a_0 traité
	// à part).
	if deg == 0 {
		// a est une constante : a(x) = a_0 ; q = 0 ; r = a_0.
		return []Felt{}, a[0]
	}
	r = a[0].Add(c.Mul(q[0]))
	return polyTrim(q), r
}

// polyDivByVanishing divise a(x) par Z_D(x) = x^n - 1 et renvoie le quotient q
// et un indicateur exact de la division (reste nul). Algorithme de division
// longue spécialisé pour le diviseur x^n - 1 (très creux) :
//
//	x^n ≡ 1 (mod Z_D), donc on réduit a en repliant les degrés >= n.
//
// On procède du plus haut degré vers le bas : tant que deg(courant) >= n, le
// terme de tête c·x^d se réécrit c·x^(d-n) (quotient) + c·x^(d-n) (repli dans le
// reste, car x^d = x^(d-n)·x^n = x^(d-n)·(Z_D + 1)). Concrètement on accumule le
// quotient et on réduit le reste jusqu'à degré < n ; la division est exacte ssi
// le reste final est nul.
func polyDivByVanishing(a []Felt, n int) (q []Felt, exact bool) {
	a = polyTrim(a)
	if len(a) == 0 {
		return []Felt{}, true
	}
	// rem est une copie de travail qu'on réduit.
	rem := make([]Felt, len(a))
	copy(rem, a)

	if len(rem) <= n {
		// degré < n : quotient nul, exact ssi rem est nul (impossible ici car
		// trimé non vide => reste non nul).
		return []Felt{}, false
	}

	qLen := len(rem) - n
	q = make([]Felt, qLen)

	// On traite les coefficients de degré d = len(rem)-1 jusqu'à n inclus.
	for d := len(rem) - 1; d >= n; d-- {
		c := rem[d]
		if c.IsZero() {
			continue
		}
		// Quotient : c·x^(d-n).
		q[d-n] = q[d-n].Add(c)
		// Repli : x^d ≡ x^(d-n) (mod Z_D) car x^n ≡ 1. On ajoute c à rem[d-n] et
		// on annule rem[d].
		rem[d-n] = rem[d-n].Add(c)
		rem[d] = Zero()
	}
	// Le reste est rem[0..n-1] ; exact ssi tout est nul.
	exact = true
	for i := 0; i < n && i < len(rem); i++ {
		if !rem[i].IsZero() {
			exact = false
			break
		}
	}
	return polyTrim(q), exact
}

// ---------------------------------------------------------------------------
// Polynôme de composition H(x)
// ---------------------------------------------------------------------------

// constraintQuotients calcule les quatre quotients de contrainte (forme
// coefficients) à partir du polynôme de trace :
//
//	q0 = transition : C(x)·(x-g^(n-2))(x-g^(n-1)) / (x^n - 1)
//	q1 = bord t[0]=1 : (T(x) - 1) / (x - g^0)
//	q2 = bord t[1]=1 : (T(x) - 1) / (x - g^1)
//	q3 = bord t[n-1]=publicOutput : (T(x) - publicOutput) / (x - g^(n-1))
//
// La transition utilise C(x) = T(g²x) - T(gx) - T(x) ; on construit T(gx) et
// T(g²x) en composant T avec la mise à l'échelle x -> g·x (multiplier le
// coefficient de degré i par g^i).
func constraintQuotients(traceCoeffs []Felt, g Felt, n int, publicOutput Felt) (q0, q1, q2, q3 []Felt) {
	// T(gx) et T(g²x) : composition affine multiplicative.
	tgx := polyComposeScale(traceCoeffs, g)
	tg2x := polyComposeScale(traceCoeffs, g.Mul(g))

	// C(x) = T(g²x) - T(gx) - T(x). Les trois polynômes ont une longueur <= n
	// (T de degré < n, compositions de même degré) ; on combine sur longueur n.
	c := polyLinComb3(tg2x, tgx, traceCoeffs, One(), One().Neg(), One().Neg(), n)

	// Numérateur transition : C(x)·(x - g^(n-2))·(x - g^(n-1)).
	gN2 := g.Exp(uint64(n - 2))
	gN1 := g.Exp(uint64(n - 1))
	num := MulPoly(c, []Felt{gN2.Neg(), One()})     // C·(x - g^(n-2))
	num = MulPoly(num, []Felt{gN1.Neg(), One()})    // ·(x - g^(n-1))
	q0t, _ := polyDivByVanishing(num, n)            // / (x^n - 1)
	q0 = q0t

	// Bords : (T - c0)/(x - point).
	one := One()
	tm1 := polySubConst(traceCoeffs, one)           // T - 1
	q1t, _ := polyDivByLinear(tm1, one)             // /(x - g^0=1)
	q1 = q1t
	q2t, _ := polyDivByLinear(tm1, g)               // /(x - g^1)
	q2 = q2t
	tmOut := polySubConst(traceCoeffs, publicOutput) // T - publicOutput
	q3t, _ := polyDivByLinear(tmOut, gN1)            // /(x - g^(n-1))
	q3 = q3t
	return q0, q1, q2, q3
}

// buildComposition assemble H(x) = α0·q0 + α1·q1 + α2·q2 + α3·q3 (forme
// coefficients). C'est la combinaison aléatoire des quotients de contrainte qui
// rend H un polynôme ssi TOUTES les contraintes sont satisfaites.
func buildComposition(traceCoeffs []Felt, g Felt, n int, publicOutput Felt, alpha [4]Felt) []Felt {
	q0, q1, q2, q3 := constraintQuotients(traceCoeffs, g, n, publicOutput)
	acc := []Felt{}
	acc = polyAddScaled(acc, q0, alpha[0])
	acc = polyAddScaled(acc, q1, alpha[1])
	acc = polyAddScaled(acc, q2, alpha[2])
	acc = polyAddScaled(acc, q3, alpha[3])
	return polyTrim(acc)
}

// polyComposeScale renvoie le polynôme a(s·x) en forme coefficients : le
// coefficient de degré i est multiplié par s^i (NOUVEAU slice). Déterministe.
func polyComposeScale(a []Felt, s Felt) []Felt {
	out := make([]Felt, len(a))
	p := One()
	for i := range a {
		out[i] = a[i].Mul(p)
		p = p.Mul(s)
	}
	return out
}

// polyLinComb3 renvoie c0·a + c1·b + c2·d sur une longueur cible len=n (les
// entrées sont de longueur <= n). NOUVEAU slice de longueur n.
func polyLinComb3(a, b, d []Felt, c0, c1, c2 Felt, n int) []Felt {
	out := make([]Felt, n)
	for i := 0; i < n; i++ {
		var va, vb, vd Felt
		if i < len(a) {
			va = a[i]
		}
		if i < len(b) {
			vb = b[i]
		}
		if i < len(d) {
			vd = d[i]
		}
		out[i] = c0.Mul(va).Add(c1.Mul(vb)).Add(c2.Mul(vd))
	}
	return out
}

// ---------------------------------------------------------------------------
// Contrôle algébrique des contraintes au point hors-domaine z (vérifieur)
// ---------------------------------------------------------------------------

// checkConstraintsAtZ recalcule, au point hors-domaine z, la combinaison des
// quotients de contrainte à partir des valeurs hors-domaine annoncées
// (Tz, Tgz, Tg2z, Hz), et la compare à Hz. C'est l'identité DEEP qui lie le
// polynôme de composition aux contraintes sur la trace, testée en un seul point
// aléatoire hors du domaine d'évaluation.
//
//	q_trans(z) = (Tg2z - Tgz - Tz)·(z - g^(n-2))(z - g^(n-1)) / (z^n - 1)
//	q_b0(z)    = (Tz - 1)/(z - 1)
//	q_b1(z)    = (Tz - 1)/(z - g)
//	q_bn(z)    = (Tz - publicOutput)/(z - g^(n-1))
//	attendu    = α0·q_trans + α1·q_b0 + α2·q_b1 + α3·q_bn
//
// Renvoie true ssi attendu == Hz. Si un dénominateur s'annule (z tombe sur un
// point de bord — événement de proba négligeable, mais on le gère pour ne jamais
// paniquer), on renvoie false : la preuve est alors rejetée proprement.
func checkConstraintsAtZ(z, g Felt, n int, publicOutput Felt, alpha [4]Felt,
	Tz, Tgz, Tg2z, Hz Felt) bool {

	one := One()
	gN1 := g.Exp(uint64(n - 1))
	gN2 := g.Exp(uint64(n - 2))

	// Dénominateurs ; rejet si l'un s'annule (point de bord touché par z).
	zn := z.Exp(uint64(n))
	denTrans := zn.Sub(one) // z^n - 1
	den0 := z.Sub(one)      // z - g^0
	den1 := z.Sub(g)        // z - g^1
	denN := z.Sub(gN1)      // z - g^(n-1)
	if denTrans.IsZero() || den0.IsZero() || den1.IsZero() || denN.IsZero() {
		return false
	}

	// Transition.
	cz := Tg2z.Sub(Tgz).Sub(Tz)
	numTrans := cz.Mul(z.Sub(gN2)).Mul(z.Sub(gN1))
	qTrans := numTrans.Div(denTrans)

	// Bords.
	qB0 := Tz.Sub(one).Div(den0)
	qB1 := Tz.Sub(one).Div(den1)
	qBn := Tz.Sub(publicOutput).Div(denN)

	expected := alpha[0].Mul(qTrans).
		Add(alpha[1].Mul(qB0)).
		Add(alpha[2].Mul(qB1)).
		Add(alpha[3].Mul(qBn))

	return expected.Equal(Hz)
}

// ---------------------------------------------------------------------------
// Quotient DEEP P(x)
// ---------------------------------------------------------------------------

// buildDeep construit le quotient DEEP (forme coefficients) :
//
//	P(x) = γ0·(T(x) - Tz)/(x - z)
//	     + γ1·(T(x) - Tgz)/(x - gz)
//	     + γ2·(T(x) - Tg2z)/(x - g2z)
//	     + γ3·(H(x) - Hz)/(x - z)
//
// Chaque terme est un polynôme EXACT (division par binôme à reste nul) ssi la
// valeur hors-domaine annoncée est bien l'évaluation du polynôme engagé au point
// correspondant — ce que le prouveur honnête garantit en posant Tz=T(z), etc.
func buildDeep(traceCoeffs, compCoeffs []Felt, z, gz, g2z Felt,
	Tz, Tgz, Tg2z, Hz Felt, gamma [4]Felt) []Felt {

	t0, _ := polyDivByLinear(polySubConst(traceCoeffs, Tz), z)
	t1, _ := polyDivByLinear(polySubConst(traceCoeffs, Tgz), gz)
	t2, _ := polyDivByLinear(polySubConst(traceCoeffs, Tg2z), g2z)
	h0, _ := polyDivByLinear(polySubConst(compCoeffs, Hz), z)

	acc := []Felt{}
	acc = polyAddScaled(acc, t0, gamma[0])
	acc = polyAddScaled(acc, t1, gamma[1])
	acc = polyAddScaled(acc, t2, gamma[2])
	acc = polyAddScaled(acc, h0, gamma[3])
	return polyTrim(acc)
}

// deepCombineAt recalcule, côté vérifieur, P(x) à un point x du domaine LDE à
// partir des valeurs ouvertes Tx = T(x), Hx = H(x) et des valeurs hors-domaine.
// C'est exactement la même combinaison que buildDeep, mais ponctuelle :
//
//	P(x) = γ0·(Tx - Tz)/(x - z) + γ1·(Tx - Tgz)/(x - gz)
//	     + γ2·(Tx - Tg2z)/(x - g2z) + γ3·(Hx - Hz)/(x - z)
//
// Si x coïncide avec un des points z, gz, g2z (dénominateur nul), on renvoie une
// valeur sentinelle qui ne pourra pas égaler l'ouverture honnête : la preuve est
// alors rejetée. En pratique x est dans le domaine LDE et z est hors-domaine
// (tiré dans [0,P)), donc l'égalité est d'une probabilité négligeable.
func deepCombineAt(x, Tx, Hx, z, gz, g2z, Tz, Tgz, Tg2z, Hz Felt, gamma [4]Felt) Felt {
	d0 := x.Sub(z)
	d1 := x.Sub(gz)
	d2 := x.Sub(g2z)
	if d0.IsZero() || d1.IsZero() || d2.IsZero() {
		// Sentinelle : un Felt « impossible » à reproduire honnêtement n'existe
		// pas dans un corps, mais renvoyer une valeur déterministe non liée aux
		// ouvertures suffit : avec une proba écrasante elle diffère de DeepVal.
		// On renvoie la combinaison sans division (jamais égale à l'ouverture
		// honnête sauf coïncidence négligeable). Pour rester strictement sûr, le
		// vérifieur traite ce cas comme un échec implicite (valeur arbitraire).
		return Tx.Add(Hx).Add(One()) // arbitraire, non lié à l'ouverture DEEP
	}
	term0 := gamma[0].Mul(Tx.Sub(Tz).Div(d0))
	term1 := gamma[1].Mul(Tx.Sub(Tgz).Div(d1))
	term2 := gamma[2].Mul(Tx.Sub(Tg2z).Div(d2))
	term3 := gamma[3].Mul(Hx.Sub(Hz).Div(d0))
	return term0.Add(term1).Add(term2).Add(term3)
}

// ---------------------------------------------------------------------------
// Défis du transcript (déterministes)
// ---------------------------------------------------------------------------

// drawAlphas tire les 4 coefficients de combinaison des contraintes depuis le
// transcript. Étiquettes distinctes => défis indépendants.
func drawAlphas(tr *Transcript) [4]Felt {
	return [4]Felt{
		tr.Challenge("fib/alpha-0"),
		tr.Challenge("fib/alpha-1"),
		tr.Challenge("fib/alpha-2"),
		tr.Challenge("fib/alpha-3"),
	}
}

// drawGammas tire les 4 coefficients de combinaison DEEP depuis le transcript.
func drawGammas(tr *Transcript) [4]Felt {
	return [4]Felt{
		tr.Challenge("fib/gamma-0"),
		tr.Challenge("fib/gamma-1"),
		tr.Challenge("fib/gamma-2"),
		tr.Challenge("fib/gamma-3"),
	}
}

// absorbFriDigest absorbe une empreinte de la preuve FRI dans le transcript
// STARK : racines de couche, coefficients finaux et taille de domaine. Cela lie
// les positions de requête du STARK à la preuve FRI exacte (toute altération de
// FRI change les positions interrogées). Déterministe.
func absorbFriDigest(tr *Transcript, p FriProof) {
	tr.AbsorbFelt("fri-digest/log-domain", FromUint64(uint64(p.LogDomain)))
	for _, r := range p.LayerRoots {
		tr.Absorb("fri-digest/layer-root", r[:])
	}
	for _, c := range p.FinalCoeffs {
		tr.AbsorbFelt("fri-digest/final-coeff", c)
	}
}

// ---------------------------------------------------------------------------
// FRI et engagement du quotient DEEP
// ---------------------------------------------------------------------------

// proveFRI lance le prouveur FRI sur les évaluations LDE du quotient DEEP avec
// les paramètres STARK. La couche 0 de la preuve engage exactement deepLDE.
func proveFRI(deepLDE []Felt) FriProof {
	return Prove(deepLDE, FriParams{Blowup: starkBlowup, NumQueries: starkNumQueries})
}

// buildDeepTree reconstruit l'arbre de Merkle des évaluations LDE du quotient
// DEEP, à l'identique de la couche 0 engagée par FRI (mêmes feuilles leafOf).
// Sert à produire les ouvertures de P aux positions de requête du STARK, contre
// la racine de couche 0 de la preuve FRI.
func buildDeepTree(deepLDE []Felt) *MerkleTree {
	_, tree := commitEvals(deepLDE)
	return tree
}
