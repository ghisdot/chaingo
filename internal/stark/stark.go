// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// STARK complet sur un AIR jouet : la suite de Fibonacci. C'est l'étage qui
// assemble toutes les briques précédentes (corps de Goldilocks, NTT, Merkle,
// transcript Fiat-Shamir, FRI) en un protocole de bout en bout, afin de prouver
// que le pipeline tient.
//
// ---------------------------------------------------------------------------
// L'AIR (Algebraic Intermediate Representation) de Fibonacci
// ---------------------------------------------------------------------------
//
// La trace d'exécution est une seule colonne t[0..n-1] avec :
//
//	t[0] = 1                       (contrainte de bord, début)
//	t[1] = 1                       (contrainte de bord, début)
//	t[i+2] = t[i+1] + t[i]         (contrainte de transition, i in [0, n-3])
//	t[n-1] = publicOutput          (contrainte de bord, sortie publique)
//
// On interpole la trace en un polynôme T(x) de degré < n sur le domaine de
// trace D = {g^0, g^1, ..., g^(n-1)} où g = RootOfUnity(log2(n)) engendre le
// sous-groupe multiplicatif d'ordre n. Comme g a l'ordre n, le « décalage de
// ligne » i -> i+1 correspond exactement à la multiplication du point par g :
// T évaluée en g^i vaut t[i], donc T(g·x) en x = g^i vaut t[i+1].
//
// Les contraintes s'expriment alors comme des identités polynomiales devant
// s'annuler sur certains points du domaine :
//
//	Transition : C(x) = T(g²·x) - T(g·x) - T(x) s'annule sur tous les g^i pour
//	  i in [0, n-3], c.-à-d. sur D privé des deux derniers points g^(n-2),
//	  g^(n-1). Le polynôme d'annulation de D entier est Z_D(x) = x^n - 1 ; en le
//	  divisant on garde l'annulation seulement sur les n-2 premiers points :
//	      Q_trans(x) = C(x) · (x - g^(n-2)) · (x - g^(n-1)) / (x^n - 1)
//	  Q_trans est un POLYNÔME (division exacte) ssi la transition est respectée.
//
//	Bords : (T(x) - 1)/(x - g^0), (T(x) - 1)/(x - g^1) et
//	  (T(x) - publicOutput)/(x - g^(n-1)) sont des polynômes ssi T prend les
//	  bonnes valeurs aux points de bord.
//
// On combine ces quotients en un polynôme de composition unique H(x) avec des
// coefficients α_i tirés du transcript (combinaison aléatoire : si UNE seule
// contrainte échoue, la combinaison n'est pas un polynôme avec proba écrasante).
//
// ---------------------------------------------------------------------------
// DEEP-ALI : lier la trace, la composition et FRI
// ---------------------------------------------------------------------------
//
// On engage (Merkle) la trace T et la composition H sur un domaine étendu LDE
// de taille N = Blowup·n (Reed-Solomon). On tire ensuite un point HORS domaine
// z (out-of-domain). Le prouveur révèle T(z), T(g·z), T(g²·z) et H(z), absorbés
// dans le transcript. Le vérifieur recontrôle ALGÉBRIQUEMENT, en z seul, que
//
//	H(z) = α0·Q_trans(z) + α1·Q_b0(z) + α2·Q_b1(z) + α3·Q_bn(z)
//
// où chaque Q_•(z) se calcule à partir des valeurs hors-domaine et des
// polynômes publics (Z_D, facteurs de bord) évalués en z. C'est l'astuce DEEP :
// la cohérence des contraintes est testée en un point aléatoire hors du domaine
// d'évaluation, donc sans division par zéro et sans interaction.
//
// Pour prouver que les valeurs annoncées T(z), H(z) sont bien celles de
// polynômes de bas degré ENGAGÉS, on forme le quotient DEEP :
//
//	P(x) = γ0·(T(x) - T(z))/(x - z)
//	     + γ1·(T(x) - T(g·z))/(x - g·z)
//	     + γ2·(T(x) - T(g²·z))/(x - g²·z)
//	     + γ3·(H(x) - H(z))/(x - z)
//
// P est de bas degré ssi T et H sont des polynômes de bas degré prenant les
// valeurs annoncées aux points correspondants. On lance FRI sur P : sa preuve
// de proximité au bas degré est la garantie centrale.
//
// LIAISON FINALE (soundness) : FRI engage P comme sa couche 0
// (proof.Fri.LayerRoots[0]). Le vérifieur, à des positions de requête tirées de
// SON propre transcript, ouvre T(x_pos), H(x_pos) (contre traceRoot/compRoot) et
// P(x_pos) (contre la racine de couche 0 de FRI), puis vérifie que P(x_pos) est
// EXACTEMENT la combinaison DEEP des valeurs ouvertes. Cela soude la fonction
// dont FRI prouve le bas degré aux engagements de trace et de composition.
//
// DÉTERMINISME ABSOLU : aucun time, aucun math/rand. Tout le « hasard » provient
// du transcript Fiat-Shamir (SHAKE256). Prouveur et vérifieur reproductibles
// bit-à-bit.
package stark

// starkDomain est l'étiquette de domaine du transcript propre au STARK Fibonacci
// (séparé du transcript interne de FRI, qui utilise friDomain).
const starkDomain = "stark/fib/v1"

// starkBlowup est le facteur d'expansion Reed-Solomon du domaine LDE. Puissance
// de 2 >= 2. Choisi fixe (8) : il fait partie de l'énoncé, absorbé dans le
// transcript.
const starkBlowup = 8

// starkNumQueries est le nombre de positions interrogées en phase de requêtes du
// STARK (cohérence trace/composition/DEEP). La soundness décroît exponentielle-
// ment avec ce nombre. FRI utilise le même budget de requêtes.
const starkNumQueries = 32

// FibProof est la preuve STARK pour une instance Fibonacci. Sérialisable en
// mémoire (la sérialisation octets éventuelle relève d'un étage supérieur).
type FibProof struct {
	// TraceRoot engage les évaluations LDE du polynôme de trace T.
	TraceRoot [32]byte
	// CompRoot engage les évaluations LDE du polynôme de composition H.
	CompRoot [32]byte

	// Valeurs hors-domaine (out-of-domain) au point z et à ses décalages de
	// ligne g·z, g²·z, absorbées dans le transcript et vérifiées algébriquement.
	OodTz   Felt // T(z)
	OodTgz  Felt // T(g·z)
	OodTg2z Felt // T(g²·z)
	OodHz   Felt // H(z)

	// Fri est la preuve de proximité au bas degré du quotient DEEP P. Sa couche 0
	// (Fri.LayerRoots[0]) EST l'engagement de P, réutilisé pour la liaison.
	Fri FriProof

	// Openings[q] regroupe, pour la q-ème position interrogée par le transcript
	// STARK, les ouvertures de T, H et P à cette position du domaine LDE.
	Openings []FibOpening
}

// FibOpening est l'ouverture, à une position du domaine LDE, des trois polynômes
// engagés (trace, composition, quotient DEEP), chacun avec son chemin de Merkle.
type FibOpening struct {
	Pos       int        // position interrogée dans [0, N)
	TraceVal  Felt       // T(ω_N^Pos)
	TracePath [][32]byte // chemin de Merkle de TraceVal contre TraceRoot
	CompVal   Felt       // H(ω_N^Pos)
	CompPath  [][32]byte // chemin de Merkle de CompVal contre CompRoot
	DeepVal   Felt       // P(ω_N^Pos)
	DeepPath  [][32]byte // chemin de Merkle de DeepVal contre Fri.LayerRoots[0]
}

// ---------------------------------------------------------------------------
// Construction de la trace et des polynômes
// ---------------------------------------------------------------------------

// buildTrace construit la trace Fibonacci t[0..n-1] avec t[0]=t[1]=1 et la
// récurrence. n DOIT valoir >= 2 (garanti par l'appelant). Tout est calculé
// dans le corps (additions modulaires) — c'est l'« exécution » prouvée.
func buildTrace(n int) []Felt {
	t := make([]Felt, n)
	t[0] = One()
	if n > 1 {
		t[1] = One()
	}
	for i := 2; i < n; i++ {
		t[i] = t[i-1].Add(t[i-2])
	}
	return t
}

// evalOnLDE évalue un polynôme (coefficients en ordre croissant, longueur <= N)
// sur le domaine LDE {ω_N^0, ..., ω_N^(N-1)} de taille N (puissance de 2). On
// passe par un buffer de longueur exactement N suivi de NTT, car Evaluate
// renverrait un slice de taille nextPow2(len(coeffs)) potentiellement < N.
func evalOnLDE(coeffs []Felt, n int) []Felt {
	buf := make([]Felt, n)
	copy(buf, coeffs) // coeffs plus court => complété par des zéros (degré padding)
	NTT(buf)
	return buf
}

// ---------------------------------------------------------------------------
// Prouveur
// ---------------------------------------------------------------------------

// ProveFib construit une preuve STARK que la suite de Fibonacci de longueur n se
// termine par publicOutput (c.-à-d. t[n-1] == publicOutput), avec t[0]=t[1]=1.
//
// NOTE DE NOMMAGE : le paquet stark expose déjà Prove/Verify pour le protocole
// FRI (test de bas degré générique). Go n'autorisant pas la surcharge, le STARK
// Fibonacci de bout en bout est exposé sous ProveFib/VerifyFib. La sémantique
// demandée — un prouveur prenant (n, publicOutput) et un vérifieur prenant
// (proof, n, publicOutput) — est respectée à l'identité du nom près.
//
// Contrats (panique sinon) : n DOIT être une puissance de 2 et >= 4 (il faut au
// moins une transition et trois points de bord distincts, et n·Blowup doit
// dépasser Blowup pour FRI).
//
// Si publicOutput n'est PAS la vraie valeur t[n-1], le polynôme de composition
// n'est pas bien défini (division non exacte) : ProveFib le construit quand même
// par division polynomiale (qui produit un reste non nul, donc un H erroné), et
// la vérification ÉCHOUERA — c'est l'objet des tests de soundness. ProveFib ne
// « ment » jamais sur la cohérence : il engage ce qu'il calcule.
func ProveFib(n int, publicOutput Felt) FibProof {
	if !isPow2(n) || n < 4 {
		panic("stark: ProveFib (Fibonacci): n doit être une puissance de 2 >= 4")
	}

	logN := log2(n)
	g := RootOfUnity(logN) // générateur du domaine de trace, ordre n

	// Domaine LDE étendu : taille N = Blowup·n.
	bigN := starkBlowup * n
	logBigN := log2(bigN)
	omegaN := RootOfUnity(logBigN) // générateur du domaine LDE, ordre N

	// --- 1) Trace et polynôme de trace T(x) (degré < n) ---
	trace := buildTrace(n)
	traceCoeffs := Interpolate(trace) // longueur n, ordre croissant

	// --- 2) LDE et engagement de la trace ---
	traceLDE := evalOnLDE(traceCoeffs, bigN)
	traceRoot, traceTree := commitEvals(traceLDE)

	// --- 3) Transcript : énoncé public + engagement de trace ---
	tr := NewTranscript(starkDomain)
	tr.AbsorbFelt("fib/n", FromUint64(uint64(n)))
	tr.AbsorbFelt("fib/blowup", FromUint64(uint64(starkBlowup)))
	tr.AbsorbFelt("fib/num-queries", FromUint64(uint64(starkNumQueries)))
	tr.AbsorbFelt("fib/public-output", publicOutput)
	tr.Absorb("fib/trace-root", traceRoot[:])

	// --- 4) Défis de combinaison des contraintes (un par contrainte) ---
	alpha := drawAlphas(tr)

	// --- 5) Polynôme de composition H(x) (coefficients) ---
	compCoeffs := buildComposition(traceCoeffs, g, n, publicOutput, alpha)

	// --- 6) LDE et engagement de la composition ---
	compLDE := evalOnLDE(compCoeffs, bigN)
	compRoot, compTree := commitEvals(compLDE)
	tr.Absorb("fib/comp-root", compRoot[:])

	// --- 7) Point hors-domaine z et valeurs hors-domaine ---
	z := tr.Challenge("fib/ood-z")
	gz := g.Mul(z)
	g2z := g.Mul(gz)
	oodTz := evalNaïfPoly(traceCoeffs, z)
	oodTgz := evalNaïfPoly(traceCoeffs, gz)
	oodTg2z := evalNaïfPoly(traceCoeffs, g2z)
	oodHz := evalNaïfPoly(compCoeffs, z)
	tr.AbsorbFelt("fib/ood-tz", oodTz)
	tr.AbsorbFelt("fib/ood-tgz", oodTgz)
	tr.AbsorbFelt("fib/ood-tg2z", oodTg2z)
	tr.AbsorbFelt("fib/ood-hz", oodHz)

	// --- 8) Défis DEEP γ (un par terme du quotient) ---
	gamma := drawGammas(tr)

	// --- 9) Quotient DEEP P(x) (coefficients) ---
	deepCoeffs := buildDeep(traceCoeffs, compCoeffs, z, gz, g2z,
		oodTz, oodTgz, oodTg2z, oodHz, gamma)

	// --- 10) LDE de P et preuve FRI de bas degré ---
	deepLDE := evalOnLDE(deepCoeffs, bigN)
	friProof := proveFRI(deepLDE)

	// La couche 0 de FRI engage deepLDE : on l'utilise comme racine d'ouverture
	// de P (liaison entre « FRI prouve P de bas degré » et « P est cohérent avec
	// les engagements de trace/composition »).
	deepRoot := friProof.LayerRoots[0]
	_ = deepRoot // documenté ; les ouvertures vérifieront contre cette racine.
	deepTree := buildDeepTree(deepLDE)

	// --- 11) Positions de requête (transcript STARK) et ouvertures ---
	// On absorbe l'énoncé de FRI dans le transcript STARK pour lier les requêtes
	// à la preuve FRI produite (un changement de FRI change les positions).
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

	_ = omegaN // ω_N sert au vérifieur ; ici implicite via les indices LDE.

	return FibProof{
		TraceRoot: traceRoot,
		CompRoot:  compRoot,
		OodTz:     oodTz,
		OodTgz:    oodTgz,
		OodTg2z:   oodTg2z,
		OodHz:     oodHz,
		Fri:       friProof,
		Openings:  openings,
	}
}

// ---------------------------------------------------------------------------
// Vérifieur
// ---------------------------------------------------------------------------

// VerifyFib rejoue le transcript et vérifie la preuve STARK Fibonacci pour les
// paramètres publics (n, publicOutput). Renvoie true ssi TOUTES les vérifications
// passent :
//
//   - structure bien formée (n puissance de 2 >= 4, tailles cohérentes) ;
//   - la preuve FRI vérifie (P est de bas degré) ;
//   - la couche 0 de FRI est l'engagement de P (liaison) ;
//   - le contrôle algébrique hors-domaine en z lie H aux contraintes sur T ;
//   - à chaque position interrogée, les ouvertures T,H,P sont authentiques et
//     P(x_pos) est exactement la combinaison DEEP des valeurs ouvertes.
//
// Ne panique JAMAIS sur preuve falsifiée : rejet propre (return false). Le
// vérifieur n'a accès qu'aux racines, aux valeurs hors-domaine et aux ouvertures.
// (Voir la note de nommage de ProveFib pour l'usage de VerifyFib plutôt que
// Verify, ce dernier étant réservé au protocole FRI.)
func VerifyFib(proof FibProof, n int, publicOutput Felt) bool {
	// --- Contrôles structurels ---
	if !isPow2(n) || n < 4 {
		return false
	}
	logN := log2(n)
	g := RootOfUnity(logN)
	bigN := starkBlowup * n
	if !isPow2(bigN) {
		return false
	}
	logBigN := log2(bigN)
	omegaN := RootOfUnity(logBigN)

	friParams := FriParams{Blowup: starkBlowup, NumQueries: starkNumQueries}

	// La preuve FRI doit prouver le bas degré sur le bon domaine.
	if proof.Fri.LogDomain != logBigN {
		return false
	}
	if len(proof.Fri.LayerRoots) == 0 {
		return false
	}
	// --- FRI : P est de bas degré ---
	// Verify(FriProof, FriParams) est le vérifieur FRI du paquet (fri.go) ; la
	// résolution se fait par la signature des arguments.
	if !Verify(proof.Fri, friParams) {
		return false
	}
	deepRoot := proof.Fri.LayerRoots[0]

	if len(proof.Openings) != starkNumQueries {
		return false
	}

	// --- Rejoue du transcript : mêmes absorptions, mêmes défis ---
	tr := NewTranscript(starkDomain)
	tr.AbsorbFelt("fib/n", FromUint64(uint64(n)))
	tr.AbsorbFelt("fib/blowup", FromUint64(uint64(starkBlowup)))
	tr.AbsorbFelt("fib/num-queries", FromUint64(uint64(starkNumQueries)))
	tr.AbsorbFelt("fib/public-output", publicOutput)
	tr.Absorb("fib/trace-root", proof.TraceRoot[:])

	alpha := drawAlphas(tr)

	tr.Absorb("fib/comp-root", proof.CompRoot[:])

	z := tr.Challenge("fib/ood-z")
	gz := g.Mul(z)
	g2z := g.Mul(gz)
	tr.AbsorbFelt("fib/ood-tz", proof.OodTz)
	tr.AbsorbFelt("fib/ood-tgz", proof.OodTgz)
	tr.AbsorbFelt("fib/ood-tg2z", proof.OodTg2z)
	tr.AbsorbFelt("fib/ood-hz", proof.OodHz)

	gamma := drawGammas(tr)

	absorbFriDigest(tr, proof.Fri)
	positions := tr.ChallengeIndices("fib/query", starkNumQueries, bigN)

	// --- Contrôle algébrique hors-domaine (cohérence des contraintes en z) ---
	if !checkConstraintsAtZ(z, g, n, publicOutput, alpha,
		proof.OodTz, proof.OodTgz, proof.OodTg2z, proof.OodHz) {
		return false
	}

	// --- Requêtes : authenticité Merkle + cohérence DEEP par position ---
	for i, pos := range positions {
		op := proof.Openings[i]
		if op.Pos != pos {
			return false
		}
		if pos < 0 || pos >= bigN {
			return false
		}

		// 1) Authenticité des trois ouvertures contre leurs racines respectives.
		if !VerifyPath(proof.TraceRoot, pos, leafOf(op.TraceVal), op.TracePath) {
			return false
		}
		if !VerifyPath(proof.CompRoot, pos, leafOf(op.CompVal), op.CompPath) {
			return false
		}
		if !VerifyPath(deepRoot, pos, leafOf(op.DeepVal), op.DeepPath) {
			return false
		}

		// 2) Cohérence DEEP : P(x_pos) doit égaler la combinaison des quotients
		//    DEEP calculés à partir des valeurs ouvertes T(x_pos), H(x_pos) et des
		//    valeurs hors-domaine annoncées.
		x := omegaN.Exp(uint64(pos))
		expected := deepCombineAt(x, op.TraceVal, op.CompVal,
			z, gz, g2z, proof.OodTz, proof.OodTgz, proof.OodTg2z, proof.OodHz, gamma)
		if !expected.Equal(op.DeepVal) {
			return false
		}
	}

	return true
}
