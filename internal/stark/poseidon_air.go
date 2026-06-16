// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// AIR + STARK de « hachage algébrique correct » : prouver en zero-knowledge
// qu'un digest public est bien le résultat de l'application correcte de la
// PRIMITIVE NON LINÉAIRE de Poseidon — la S-box x^7 — itérée sur un préimage
// secret. C'est le pont vers le circuit blindé : pouvoir PROUVER, sans révéler
// le préimage, qu'un calcul de hachage a été mené correctement.
//
// ┌──────────────────────────────────────────────────────────────────────────┐
// │ HONNÊTETÉ SUR LA PORTÉE — VERSION RÉDUITE ASSUMÉE.                          │
// │                                                                            │
// │ L'AIR COMPLET de la permutation Poseidon (12 colonnes d'état, sélecteurs   │
// │ rondes pleines/partielles, couche MDS couplant les 12 colonnes, addRC par  │
// │ ronde) exige un moteur STARK MULTI-COLONNES. Le pipeline DEEP-ALI existant │
// │ (stark.go / stark_air.go) est MONO-COLONNE : il code en dur UNE colonne de │
// │ trace T, une transition fixe T(g²x)-T(gx)-T(x), et trois évaluations hors- │
// │ domaine T(z), T(gz), T(g²z). Le réécrire en multi-colonnes correct dépasse │
// │ le cadre de cet étage.                                                     │
// │                                                                            │
// │ On livre donc une version RÉDUITE mais CORRECTE : un STARK qui prouve      │
// │ l'itération correcte de la S-box x^7 (la SEULE non-linéarité de Poseidon,  │
// │ son cœur algébrique de degré 7) sur une chaîne. Sont OMIS de cette         │
// │ réduction : la couche MDS, les constantes de ronde, et la distinction      │
// │ rondes pleines / partielles. Ce qui est PROUVÉ ici l'est de façon          │
// │ rigoureuse (mêmes garanties de soundness que le STARK Fibonacci) ; ce qui  │
// │ est OMIS l'est explicitement. La CORRECTION prime sur la complétude.       │
// │                                                                            │
// │ De plus, les paramètres Poseidon eux-mêmes (matrice, constantes) sont      │
// │ CHOISIS PAR NOUS et NON AUDITÉS (voir poseidon.go). Ne pas utiliser en     │
// │ consensus / production.                                                    │
// └──────────────────────────────────────────────────────────────────────────┘
//
// ---------------------------------------------------------------------------
// L'AIR de la chaîne S-box (sous-composante de Poseidon)
// ---------------------------------------------------------------------------
//
// La trace d'exécution est une seule colonne t[0..n-1] avec :
//
//	t[0]      = preimage              (contrainte de bord, secret — non publié)
//	t[i+1]    = sbox(t[i]) = t[i]^7   (contrainte de transition, i in [0, n-2])
//	t[n-1]    = digest                (contrainte de bord, sortie PUBLIQUE)
//
// On interpole la trace en un polynôme T(x) de degré < n sur le domaine de
// trace D = {g^0, ..., g^(n-1)} où g = RootOfUnity(log2(n)) est d'ordre n. Le
// « décalage de ligne » i -> i+1 correspond à la multiplication du point par g :
// T(g·x) évalué en x = g^i vaut t[i+1].
//
// Contrainte de transition (S-box) :
//
//	C(x) = T(g·x) - T(x)^7   s'annule sur g^0, ..., g^(n-2) (D privé du DERNIER
//	  point g^(n-1)). Le polynôme d'annulation de D entier est Z_D(x) = x^n - 1.
//	  En multipliant C par (x - g^(n-1)) puis en divisant par Z_D, on garde
//	  l'annulation sur les n-1 premiers points :
//	      Q_trans(x) = C(x)·(x - g^(n-1)) / (x^n - 1)
//	  Q_trans est un POLYNÔME (division exacte) ssi toute la chaîne S-box tient.
//
//	DEGRÉ : T est de degré < n, donc T(x)^7 est de degré < 7n et DOMINE C. Après
//	  ·(x - g^(n-1)) le numérateur est de degré ≤ 7(n-1)+1 = 7n-6 ; après /Z_D
//	  (degré n) on obtient deg(Q_trans) ≤ 6n-6. C'est la raison du grand domaine
//	  LDE (voir sbBlowup / sbDegBound ci-dessous).
//
// Contraintes de bord :
//
//	(T(x) - digest)/(x - g^(n-1))   est un polynôme ssi t[n-1] == digest.
//
// REMARQUE : on n'impose AUCUNE contrainte de bord sur t[0] (le préimage est
// secret et n'est pas un paramètre public) — c'est précisément l'intérêt
// zero-knowledge : on prouve la connaissance d'un préimage menant au digest
// sans le révéler. (Notre prototype ne masque pas la trace par des aléas de
// zero-knowledge ; le « ZK » se limite ici à ne pas publier t[0]. Le masquage
// complet — randomized low-degree extension — est un point À AUDITER.)
//
// On combine ces quotients en un polynôme de composition H(x) avec des
// coefficients α_i tirés du transcript : si UNE contrainte échoue, la
// combinaison n'est pas un polynôme avec probabilité écrasante.
//
// ---------------------------------------------------------------------------
// DEEP-ALI : même mécanique que le STARK Fibonacci
// ---------------------------------------------------------------------------
//
// On engage (Merkle SHA3, via commitEvals) les évaluations LDE de T et de H sur
// un domaine étendu de taille bigN = sbBlowup · sbDegBound (Reed-Solomon). On
// tire un point hors-domaine z, on révèle T(z), T(g·z) et H(z), on recontrôle
// algébriquement en z l'identité de composition, puis on forme le quotient DEEP
//
//	P(x) = γ0·(T(x)-T(z))/(x-z) + γ1·(T(x)-T(gz))/(x-gz) + γ2·(H(x)-H(z))/(x-z)
//
// et on lance FRI dessus. La liaison finale (FRI couche 0 = engagement de P,
// ouvertures de T/H/P aux positions de requête, recombinaison DEEP ponctuelle)
// est IDENTIQUE à celle du STARK Fibonacci : voir les commentaires de stark.go.
//
// CHOIX DU DOMAINE (soundness) : FRI replie le domaine LDE jusqu'à la taille
// sbBlowup et exige une couche finale CONSTANTE ; il prouve donc le bas degré
// avec la borne D = bigN / sbBlowup. Comme P est de degré ≤ 6n-7, il faut
// D > 6n. On prend sbDegBound = la plus petite puissance de 2 strictement
// supérieure à 6n (donc ≥ 8n pour n puissance de 2), et bigN = sbBlowup·sbDegBound.
//
// DÉTERMINISME ABSOLU : aucun time, aucun math/rand. Tout l'aléa provient du
// transcript Fiat-Shamir (SHAKE256). Prouveur et vérifieur reproductibles
// bit-à-bit. Les constantes de ronde Poseidon n'interviennent pas dans cet AIR
// réduit (la S-box pure n'en a pas) ; aucune constante n'est codée en dur de
// façon non documentée.
package stark

// sbDomain est l'étiquette de domaine du transcript propre au STARK S-box
// (séparée du STARK Fibonacci, qui utilise starkDomain, et de FRI).
const sbDomain = "stark/poseidon-sbox/v1"

// sbBlowup est le facteur d'expansion Reed-Solomon : FRI replie jusqu'à cette
// taille et exige une couche finale constante. Puissance de 2 >= 2.
const sbBlowup = 8

// sbNumQueries est le nombre de positions interrogées (cohérence trace /
// composition / DEEP). La soundness décroît exponentiellement avec ce nombre.
const sbNumQueries = 32

// sbSboxExp est l'exposant de la S-box (x^7) prouvée par cet AIR. Il est
// IDENTIQUE à poseidonAlpha (la S-box de notre Poseidon) — on ne le redéfinit
// pas pour ne pas risquer une divergence silencieuse, mais on documente ici le
// lien : cet AIR arithmétise exactement la non-linéarité de Permute.
const sbSboxExp = poseidonAlpha // == 7

// HashProof est la preuve STARK pour une instance de chaîne S-box. Sa structure
// est calquée sur FibProof, à ceci près qu'il n'y a que DEUX valeurs hors-domaine
// de trace (T(z) et T(g·z)) car la transition S-box ne fait intervenir que deux
// lignes consécutives (contre trois pour Fibonacci).
//
// Sérialisable en mémoire (la sérialisation octets éventuelle relève d'un étage
// supérieur).
type HashProof struct {
	// N est le nombre de cellules de trace (longueur de la chaîne S-box, == n).
	// Il fait partie de l'énoncé public et est absorbé dans le transcript ; on
	// le transporte dans la preuve pour que VerifyHash reconstruise la structure
	// (domaine, FRI) sans paramètre externe au-delà du digest.
	N int

	// TraceRoot engage les évaluations LDE du polynôme de trace T.
	TraceRoot [32]byte
	// CompRoot engage les évaluations LDE du polynôme de composition H.
	CompRoot [32]byte

	// Valeurs hors-domaine au point z et au décalage de ligne g·z.
	OodTz  Felt // T(z)
	OodTgz Felt // T(g·z)
	OodHz  Felt // H(z)

	// Fri est la preuve de proximité au bas degré du quotient DEEP P. Sa couche 0
	// (Fri.LayerRoots[0]) EST l'engagement de P, réutilisé pour la liaison.
	Fri FriProof

	// Openings[q] regroupe, pour la q-ème position interrogée, les ouvertures de
	// T, H et P à cette position du domaine LDE.
	Openings []HashOpening
}

// HashOpening est l'ouverture, à une position du domaine LDE, des trois
// polynômes engagés (trace, composition, quotient DEEP), chacun avec son chemin
// de Merkle. Identique en esprit à FibOpening.
type HashOpening struct {
	Pos       int        // position interrogée dans [0, bigN)
	TraceVal  Felt       // T(ω_N^Pos)
	TracePath [][32]byte // chemin de Merkle de TraceVal contre TraceRoot
	CompVal   Felt       // H(ω_N^Pos)
	CompPath  [][32]byte // chemin de Merkle de CompVal contre CompRoot
	DeepVal   Felt       // P(ω_N^Pos)
	DeepPath  [][32]byte // chemin de Merkle de DeepVal contre Fri.LayerRoots[0]
}

// ---------------------------------------------------------------------------
// Bornes de degré et tailles de domaine
// ---------------------------------------------------------------------------

// sbDegBound est la borne de bas degré effective D (puissance de 2) que FRI
// prouvera (D == bigN / sbBlowup), choisie strictement supérieure à deg(P).
//
// Analyse de degré : deg(Q_trans) ≤ 7(n-1)+1 - n = 6n-6, donc deg(H) ≤ 6n-6 et
// deg(P) ≤ 6n-7 (le quotient DEEP réduit le degré de 1). On veut une puissance
// de 2 STRICTEMENT > 6n-6 pour que FRI (couche finale constante) accepte un
// polynôme de degré ≤ 6n-6. On prend D = nextPow2((α-1)·n + 1) = nextPow2(6n+1),
// soit 8n pour n puissance de 2 — qui domine bien 6n-6.
func sbDegBound(n int) int {
	return nextPow2((sbSboxExp-1)*n + 1) // nextPow2(6n+1) = 8n pour n pow2
}

// sbBigN renvoie la taille du domaine LDE : bigN = sbBlowup · sbDegBound(n).
// C'est une puissance de 2 (produit de deux puissances de 2). FRI prouvera le
// bas degré avec la borne sbDegBound(n) = bigN/sbBlowup.
func sbBigN(n int) int {
	return sbBlowup * sbDegBound(n)
}

// ---------------------------------------------------------------------------
// Construction de la trace S-box
// ---------------------------------------------------------------------------

// buildSboxTrace construit la trace t[0..n-1] avec t[0]=preimage et la
// récurrence t[i+1]=sbox(t[i]). n DOIT valoir >= 2 (garanti par l'appelant).
func buildSboxTrace(preimage Felt, n int) []Felt {
	t := make([]Felt, n)
	t[0] = preimage
	for i := 1; i < n; i++ {
		t[i] = sbox(t[i-1])
	}
	return t
}

// SboxDigest calcule le digest public d'un préimage pour une chaîne de n
// cellules : c'est t[n-1] = sbox^(n-1)(preimage). Exposé comme oracle de
// référence (utile aux appelants et aux tests pour connaître la sortie attendue
// sans construire de preuve).
func SboxDigest(preimage Felt, n int) Felt {
	t := buildSboxTrace(preimage, n)
	return t[n-1]
}

// ---------------------------------------------------------------------------
// Quotients de contrainte et polynôme de composition H(x)
// ---------------------------------------------------------------------------

// polyPow élève un polynôme (forme coefficients) à la puissance e par
// exponentiation rapide via MulPoly. e DOIT être >= 1. Déterministe.
func polyPow(a []Felt, e int) []Felt {
	// e >= 1 garanti par l'appelant (S-box exposant 7).
	result := []Felt(nil)
	base := clonePoly(a)
	first := true
	for e > 0 {
		if e&1 == 1 {
			if first {
				result = clonePoly(base)
				first = false
			} else {
				result = MulPoly(result, base)
			}
		}
		e >>= 1
		if e > 0 {
			base = MulPoly(base, base)
		}
	}
	return result
}

// sboxConstraintQuotients calcule les deux quotients de contrainte (forme
// coefficients) à partir du polynôme de trace :
//
//	q0 = transition S-box : C(x)·(x - g^(n-1)) / (x^n - 1) avec
//	     C(x) = T(g·x) - T(x)^7
//	q1 = bord t[n-1]=digest : (T(x) - digest) / (x - g^(n-1))
//
// Si la trace est honnête, q0 et q1 sont des polynômes EXACTS (reste nul) ; sinon
// la division produit un reste non nul (ignoré ici) et la composition résultante
// n'est pas un vrai polynôme de bas degré, ce que FRI rejettera.
func sboxConstraintQuotients(traceCoeffs []Felt, g Felt, n int, digest Felt) (q0, q1 []Felt) {
	// T(g·x) : composition affine multiplicative (coeff de degré i ·= g^i).
	tgx := polyComposeScale(traceCoeffs, g)
	// T(x)^7.
	t7 := polyPow(traceCoeffs, sbSboxExp)

	// C(x) = T(g·x) - T(x)^7. On combine sur la longueur max des deux.
	c := polySub(tgx, t7)

	// Numérateur transition : C(x)·(x - g^(n-1)).
	gN1 := g.Exp(uint64(n - 1))
	num := MulPoly(c, []Felt{gN1.Neg(), One()}) // C·(x - g^(n-1))
	q0t, _ := polyDivByVanishing(num, n)        // / (x^n - 1)
	q0 = q0t

	// Bord : (T - digest)/(x - g^(n-1)).
	tmOut := polySubConst(traceCoeffs, digest)
	q1t, _ := polyDivByLinear(tmOut, gN1)
	q1 = q1t
	return q0, q1
}

// polySub renvoie a(x) - b(x) (NOUVEAU slice, longueur = max(len(a),len(b))),
// puis trim. Complète l'arithmétique polynomiale de stark_air.go.
func polySub(a, b []Felt) []Felt {
	m := len(a)
	if len(b) > m {
		m = len(b)
	}
	out := make([]Felt, m)
	for i := 0; i < m; i++ {
		var va, vb Felt
		if i < len(a) {
			va = a[i]
		}
		if i < len(b) {
			vb = b[i]
		}
		out[i] = va.Sub(vb)
	}
	return polyTrim(out)
}

// buildSboxComposition assemble H(x) = α0·q0 + α1·q1 (forme coefficients).
func buildSboxComposition(traceCoeffs []Felt, g Felt, n int, digest Felt, alpha [2]Felt) []Felt {
	q0, q1 := sboxConstraintQuotients(traceCoeffs, g, n, digest)
	acc := []Felt{}
	acc = polyAddScaled(acc, q0, alpha[0])
	acc = polyAddScaled(acc, q1, alpha[1])
	return polyTrim(acc)
}

// ---------------------------------------------------------------------------
// Contrôle algébrique des contraintes au point hors-domaine z (vérifieur)
// ---------------------------------------------------------------------------

// checkSboxConstraintsAtZ recalcule au point z la combinaison des quotients de
// contrainte à partir des valeurs hors-domaine annoncées (Tz, Tgz, Hz) et la
// compare à Hz :
//
//	q_trans(z) = (Tgz - Tz^7)·(z - g^(n-1)) / (z^n - 1)
//	q_bn(z)    = (Tz - digest)/(z - g^(n-1))
//	attendu    = α0·q_trans + α1·q_bn
//
// Renvoie true ssi attendu == Hz. Si un dénominateur s'annule (z tombe sur un
// point de bord — proba négligeable), on rejette proprement (false), jamais de
// panique.
func checkSboxConstraintsAtZ(z, g Felt, n int, digest Felt, alpha [2]Felt,
	Tz, Tgz, Hz Felt) bool {

	one := One()
	gN1 := g.Exp(uint64(n - 1))

	zn := z.Exp(uint64(n))
	denTrans := zn.Sub(one) // z^n - 1
	denN := z.Sub(gN1)      // z - g^(n-1)
	if denTrans.IsZero() || denN.IsZero() {
		return false
	}

	// Transition : C(z) = Tgz - Tz^7.
	cz := Tgz.Sub(Tz.Exp(sbSboxExp))
	numTrans := cz.Mul(z.Sub(gN1))
	qTrans := numTrans.Div(denTrans)

	// Bord.
	qBn := Tz.Sub(digest).Div(denN)

	expected := alpha[0].Mul(qTrans).Add(alpha[1].Mul(qBn))
	return expected.Equal(Hz)
}

// ---------------------------------------------------------------------------
// Quotient DEEP P(x)
// ---------------------------------------------------------------------------

// buildSboxDeep construit le quotient DEEP (forme coefficients) :
//
//	P(x) = γ0·(T(x) - Tz)/(x - z)
//	     + γ1·(T(x) - Tgz)/(x - gz)
//	     + γ2·(H(x) - Hz)/(x - z)
//
// Chaque terme est un polynôme EXACT ssi la valeur hors-domaine annoncée est bien
// l'évaluation du polynôme engagé au point correspondant.
func buildSboxDeep(traceCoeffs, compCoeffs []Felt, z, gz Felt,
	Tz, Tgz, Hz Felt, gamma [3]Felt) []Felt {

	t0, _ := polyDivByLinear(polySubConst(traceCoeffs, Tz), z)
	t1, _ := polyDivByLinear(polySubConst(traceCoeffs, Tgz), gz)
	h0, _ := polyDivByLinear(polySubConst(compCoeffs, Hz), z)

	acc := []Felt{}
	acc = polyAddScaled(acc, t0, gamma[0])
	acc = polyAddScaled(acc, t1, gamma[1])
	acc = polyAddScaled(acc, h0, gamma[2])
	return polyTrim(acc)
}

// sboxDeepCombineAt recalcule, côté vérifieur, P(x) à un point x du domaine LDE à
// partir des valeurs ouvertes Tx = T(x), Hx = H(x) et des valeurs hors-domaine.
// Même combinaison que buildSboxDeep, ponctuelle. En cas de dénominateur nul
// (x coïncide avec z ou gz — proba négligeable), renvoie une valeur sentinelle
// non liée à l'ouverture honnête (rejet implicite).
func sboxDeepCombineAt(x, Tx, Hx, z, gz, Tz, Tgz, Hz Felt, gamma [3]Felt) Felt {
	d0 := x.Sub(z)
	d1 := x.Sub(gz)
	if d0.IsZero() || d1.IsZero() {
		return Tx.Add(Hx).Add(One()) // arbitraire, non lié à l'ouverture DEEP
	}
	term0 := gamma[0].Mul(Tx.Sub(Tz).Div(d0))
	term1 := gamma[1].Mul(Tx.Sub(Tgz).Div(d1))
	term2 := gamma[2].Mul(Hx.Sub(Hz).Div(d0))
	return term0.Add(term1).Add(term2)
}

// ---------------------------------------------------------------------------
// Défis du transcript (déterministes)
// ---------------------------------------------------------------------------

// drawSboxAlphas tire les 2 coefficients de combinaison des contraintes.
func drawSboxAlphas(tr *Transcript) [2]Felt {
	return [2]Felt{
		tr.Challenge("sbox/alpha-0"),
		tr.Challenge("sbox/alpha-1"),
	}
}

// drawSboxGammas tire les 3 coefficients de combinaison DEEP.
func drawSboxGammas(tr *Transcript) [3]Felt {
	return [3]Felt{
		tr.Challenge("sbox/gamma-0"),
		tr.Challenge("sbox/gamma-1"),
		tr.Challenge("sbox/gamma-2"),
	}
}

// ---------------------------------------------------------------------------
// Prouveur
// ---------------------------------------------------------------------------

// ProveHash construit une preuve STARK que le digest public renvoyé est bien
// sbox^(n-1)(preimage), c.-à-d. que le préimage SECRET, passé n-1 fois dans la
// S-box x^7 de Poseidon, donne ce digest — SANS révéler le préimage.
//
// Renvoie (digest, proof). Le digest est public ; le préimage ne figure NULLE
// PART dans la preuve (ni en clair, ni en valeur hors-domaine : seuls T(z),
// T(g·z) en un point z hors-domaine et la sortie t[n-1] sont exposés).
//
// Contrats (panique sinon) : n DOIT être une puissance de 2 et >= 4 (au moins une
// transition et un domaine LDE non dégénéré).
func ProveHash(preimage Felt, n int) (Felt, HashProof) {
	if !isPow2(n) || n < 4 {
		panic("stark: ProveHash (S-box): n doit être une puissance de 2 >= 4")
	}

	logN := log2(n)
	g := RootOfUnity(logN) // générateur du domaine de trace, ordre n

	bigN := sbBigN(n)
	logBigN := log2(bigN)

	// --- 1) Trace et polynôme de trace T(x) (degré < n) ---
	trace := buildSboxTrace(preimage, n)
	digest := trace[n-1] // sortie publique
	traceCoeffs := Interpolate(trace)

	// --- 2) LDE et engagement de la trace ---
	traceLDE := evalOnLDE(traceCoeffs, bigN)
	traceRoot, traceTree := commitEvals(traceLDE)

	// --- 3) Transcript : énoncé public + engagement de trace ---
	tr := NewTranscript(sbDomain)
	tr.AbsorbFelt("sbox/n", FromUint64(uint64(n)))
	tr.AbsorbFelt("sbox/blowup", FromUint64(uint64(sbBlowup)))
	tr.AbsorbFelt("sbox/num-queries", FromUint64(uint64(sbNumQueries)))
	tr.AbsorbFelt("sbox/exp", FromUint64(uint64(sbSboxExp)))
	tr.AbsorbFelt("sbox/digest", digest)
	tr.Absorb("sbox/trace-root", traceRoot[:])

	// --- 4) Défis de combinaison des contraintes ---
	alpha := drawSboxAlphas(tr)

	// --- 5) Polynôme de composition H(x) ---
	compCoeffs := buildSboxComposition(traceCoeffs, g, n, digest, alpha)

	// --- 6) LDE et engagement de la composition ---
	compLDE := evalOnLDE(compCoeffs, bigN)
	compRoot, compTree := commitEvals(compLDE)
	tr.Absorb("sbox/comp-root", compRoot[:])

	// --- 7) Point hors-domaine z et valeurs hors-domaine ---
	z := tr.Challenge("sbox/ood-z")
	gz := g.Mul(z)
	oodTz := evalNaïfPoly(traceCoeffs, z)
	oodTgz := evalNaïfPoly(traceCoeffs, gz)
	oodHz := evalNaïfPoly(compCoeffs, z)
	tr.AbsorbFelt("sbox/ood-tz", oodTz)
	tr.AbsorbFelt("sbox/ood-tgz", oodTgz)
	tr.AbsorbFelt("sbox/ood-hz", oodHz)

	// --- 8) Défis DEEP γ ---
	gamma := drawSboxGammas(tr)

	// --- 9) Quotient DEEP P(x) ---
	deepCoeffs := buildSboxDeep(traceCoeffs, compCoeffs, z, gz,
		oodTz, oodTgz, oodHz, gamma)

	// --- 10) LDE de P et preuve FRI de bas degré ---
	deepLDE := evalOnLDE(deepCoeffs, bigN)
	friProof := proveFRISbox(deepLDE)
	deepTree := buildDeepTree(deepLDE)

	// --- 11) Positions de requête (transcript STARK) et ouvertures ---
	absorbFriDigest(tr, friProof)
	positions := tr.ChallengeIndices("sbox/query", sbNumQueries, bigN)

	openings := make([]HashOpening, len(positions))
	for i, pos := range positions {
		openings[i] = HashOpening{
			Pos:       pos,
			TraceVal:  traceLDE[pos],
			TracePath: Open(traceTree, pos),
			CompVal:   compLDE[pos],
			CompPath:  Open(compTree, pos),
			DeepVal:   deepLDE[pos],
			DeepPath:  Open(deepTree, pos),
		}
	}

	_ = logBigN // implicite via les indices LDE.

	proof := HashProof{
		N:         n,
		TraceRoot: traceRoot,
		CompRoot:  compRoot,
		OodTz:     oodTz,
		OodTgz:    oodTgz,
		OodHz:     oodHz,
		Fri:       friProof,
		Openings:  openings,
	}
	return digest, proof
}

// proveFRISbox lance le prouveur FRI sur les évaluations LDE du quotient DEEP
// avec les paramètres S-box. Couche 0 = engagement de deepLDE.
func proveFRISbox(deepLDE []Felt) FriProof {
	return Prove(deepLDE, FriParams{Blowup: sbBlowup, NumQueries: sbNumQueries})
}

// ---------------------------------------------------------------------------
// Vérifieur
// ---------------------------------------------------------------------------

// VerifyHash rejoue le transcript et vérifie la preuve STARK de hachage S-box
// pour le digest public attendu. Renvoie true ssi TOUTES les vérifications
// passent (structure bien formée ; FRI prouve P de bas degré ; couche 0 de FRI =
// engagement de P ; contrôle algébrique hors-domaine en z ; ouvertures
// authentiques et cohérence DEEP à chaque position).
//
// Le digest passé en argument est l'énoncé public ; il DOIT coïncider avec celui
// absorbé par le prouveur, sans quoi le transcript diverge et la preuve est
// rejetée. n est lu dans proof.N (et recontrôlé). Ne panique JAMAIS sur preuve
// falsifiée : rejet propre (return false).
func VerifyHash(digest Felt, proof HashProof) bool {
	n := proof.N
	// --- Contrôles structurels ---
	if !isPow2(n) || n < 4 {
		return false
	}
	logN := log2(n)
	g := RootOfUnity(logN)
	bigN := sbBigN(n)
	if !isPow2(bigN) {
		return false
	}
	logBigN := log2(bigN)

	friParams := FriParams{Blowup: sbBlowup, NumQueries: sbNumQueries}

	// La preuve FRI doit prouver le bas degré sur le bon domaine.
	if proof.Fri.LogDomain != logBigN {
		return false
	}
	if len(proof.Fri.LayerRoots) == 0 {
		return false
	}
	// --- FRI : P est de bas degré ---
	if !Verify(proof.Fri, friParams) {
		return false
	}
	deepRoot := proof.Fri.LayerRoots[0]

	if len(proof.Openings) != sbNumQueries {
		return false
	}

	// --- Rejoue du transcript : mêmes absorptions, mêmes défis ---
	tr := NewTranscript(sbDomain)
	tr.AbsorbFelt("sbox/n", FromUint64(uint64(n)))
	tr.AbsorbFelt("sbox/blowup", FromUint64(uint64(sbBlowup)))
	tr.AbsorbFelt("sbox/num-queries", FromUint64(uint64(sbNumQueries)))
	tr.AbsorbFelt("sbox/exp", FromUint64(uint64(sbSboxExp)))
	tr.AbsorbFelt("sbox/digest", digest)
	tr.Absorb("sbox/trace-root", proof.TraceRoot[:])

	alpha := drawSboxAlphas(tr)

	tr.Absorb("sbox/comp-root", proof.CompRoot[:])

	z := tr.Challenge("sbox/ood-z")
	gz := g.Mul(z)
	tr.AbsorbFelt("sbox/ood-tz", proof.OodTz)
	tr.AbsorbFelt("sbox/ood-tgz", proof.OodTgz)
	tr.AbsorbFelt("sbox/ood-hz", proof.OodHz)

	gamma := drawSboxGammas(tr)

	absorbFriDigest(tr, proof.Fri)
	positions := tr.ChallengeIndices("sbox/query", sbNumQueries, bigN)

	// --- Contrôle algébrique hors-domaine (cohérence des contraintes en z) ---
	if !checkSboxConstraintsAtZ(z, g, n, digest, alpha,
		proof.OodTz, proof.OodTgz, proof.OodHz) {
		return false
	}

	// --- Requêtes : authenticité Merkle + cohérence DEEP par position ---
	omegaN := RootOfUnity(logBigN)
	for i, pos := range positions {
		op := proof.Openings[i]
		if op.Pos != pos {
			return false
		}
		if pos < 0 || pos >= bigN {
			return false
		}

		if !VerifyPath(proof.TraceRoot, pos, leafOf(op.TraceVal), op.TracePath) {
			return false
		}
		if !VerifyPath(proof.CompRoot, pos, leafOf(op.CompVal), op.CompPath) {
			return false
		}
		if !VerifyPath(deepRoot, pos, leafOf(op.DeepVal), op.DeepPath) {
			return false
		}

		x := omegaN.Exp(uint64(pos))
		expected := sboxDeepCombineAt(x, op.TraceVal, op.CompVal,
			z, gz, proof.OodTz, proof.OodTgz, proof.OodHz, gamma)
		if !expected.Equal(op.DeepVal) {
			return false
		}
	}

	return true
}
