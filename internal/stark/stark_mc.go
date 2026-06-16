// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// STARK DEEP-ALI MULTI-COLONNES générique. C'est la généralisation du STARK
// Fibonacci mono-colonne (stark.go / stark_air.go) à un AIR à W colonnes décrit
// par une interface. Il sert de socle réutilisable pour arithmétiser n'importe
// quel calcul exprimable comme une trace rectangulaire (W colonnes × n lignes)
// soumise à des contraintes de TRANSITION (liant chaque ligne à la suivante) et
// de BORD (fixant des cellules à des valeurs publiques).
//
// ---------------------------------------------------------------------------
// Modèle d'AIR (Algebraic Intermediate Representation)
// ---------------------------------------------------------------------------
//
// Une trace est une matrice trace[step][col], step in [0,n), col in [0,W). On
// interpole CHAQUE colonne c en un polynôme Tc(x) de degré < n sur le domaine de
// trace D = {g^0, ..., g^(n-1)} où g = RootOfUnity(log2(n)) est d'ordre n. Comme
// pour le mono-colonne, le « décalage de ligne » i -> i+1 correspond à la
// multiplication du point par g : Tc(g·x) évalué en x = g^i vaut trace[i+1][c].
//
// Contraintes de TRANSITION : l'AIR fournit EvalTransition(cur, next) qui reçoit
// la ligne courante (cur[c] = trace[i][c]) et la ligne suivante
// (next[c] = trace[i+1][c]) et renvoie un vecteur de Felt, chaque composante
// devant s'ANNULER pour i in [0, n-2] (toutes les paires de lignes consécutives).
// Sur le domaine D, la k-ème contrainte définit donc un polynôme Ck(x) qui
// s'annule sur g^0, ..., g^(n-2) — c.-à-d. sur D privé du DERNIER point g^(n-1).
// Le polynôme d'annulation de D entier est Z_D(x) = x^n - 1. On forme le
// quotient :
//
//	Qk(x) = Ck(x)·(x - g^(n-1)) / (x^n - 1)
//
// qui est un POLYNÔME (division exacte) ssi la k-ème contrainte de transition
// tient sur toutes les lignes.
//
// Contraintes de BORD : chacune est (col, row, value) et impose
// trace[row][col] == value. Sur le domaine, le point de la ligne row est
// g^row, et la contrainte définit le quotient :
//
//	Qb(x) = (Tcol(x) - value) / (x - g^row)
//
// polynôme ssi la cellule prend la bonne valeur publique.
//
// COMPOSITION : on combine TOUS les quotients (transition puis bord) en un
// unique polynôme H(x) = Σ α_i · Q_i(x), avec des défis α_i tirés du transcript.
// Si UNE seule contrainte échoue, H n'est pas un polynôme de bas degré avec
// probabilité écrasante (la combinaison aléatoire ne peut « annuler » qu'une
// erreur soigneusement corrélée aux α, ce que le prouveur ne peut anticiper :
// les α sont tirés APRÈS l'engagement de la trace).
//
// ---------------------------------------------------------------------------
// DEEP-ALI multi-colonnes
// ---------------------------------------------------------------------------
//
// On engage (Merkle SHA3, via commitEvals) les évaluations LDE de CHAQUE colonne
// Tc et de la composition H sur un domaine étendu de taille bigN (Reed-Solomon).
// On tire un point hors-domaine z, puis on ouvre CHAQUE colonne en z ET en g·z
// (la ligne suivante), plus H(z). Le vérifieur recontrôle ALGÉBRIQUEMENT en z
// que H(z) == Σ α_i · Q_i(z), où chaque Q_i(z) se recalcule à partir des
// ouvertures de colonnes en z et g·z (transition via EvalTransition appliqué aux
// valeurs OOD, bords via Tcol(z)).
//
// Pour lier ces valeurs annoncées aux polynômes ENGAGÉS, on forme le quotient
// DEEP combinant TOUTES les ouvertures :
//
//	P(x) = Σ_c γ_c^z ·(Tc(x) - Tc(z))/(x - z)
//	     + Σ_c γ_c^gz·(Tc(x) - Tc(gz))/(x - gz)
//	     + γ_H       ·(H(x)  - H(z)) /(x - z)
//
// P est de bas degré ssi toutes les colonnes et H sont de bas degré et prennent
// les valeurs annoncées. On lance FRI sur P. La liaison finale (FRI couche 0 =
// engagement de P, ouvertures de chaque colonne / H / P aux positions de requête,
// recombinaison DEEP ponctuelle) est IDENTIQUE à celle du STARK Fibonacci.
//
// CHOIX DU DOMAINE : deg(Tc) < n. La k-ème contrainte de transition a un degré
// algébrique ≤ MaxDegree en les colonnes, donc deg(Ck) ≤ MaxDegree·(n-1) ; après
// ·(x - g^(n-1)) le numérateur est de degré ≤ MaxDegree·(n-1)+1 ; après /Z_D
// (degré n) on a deg(Qk) ≤ MaxDegree·(n-1)+1 - n. Les quotients de bord sont de
// degré ≤ n-2. H est donc de degré ≤ MaxDegree·n environ ; le quotient DEEP P
// retire encore 1. On prend bigN = Blowup · nextPow2(MaxDegree · NumSteps), ce
// qui garantit que la borne de bas degré prouvée par FRI (D = bigN/Blowup =
// nextPow2(MaxDegree·n)) domine deg(P). FRI replie jusqu'à Blowup et exige une
// couche finale CONSTANTE : il prouve le bas degré avec cette borne D.
//
// DÉTERMINISME ABSOLU : aucun time, aucun math/rand. Tout l'aléa provient du
// transcript Fiat-Shamir (SHAKE256). Prouveur et vérifieur reproductibles
// bit-à-bit.
package stark

import "strconv"

// mcDomain est l'étiquette de domaine du transcript propre au STARK multi-
// colonnes (séparée du STARK Fibonacci, du STARK S-box, et de FRI).
const mcDomain = "stark/air-mc/v1"

// mcBlowup est le facteur d'expansion Reed-Solomon : FRI replie jusqu'à cette
// taille et exige une couche finale constante. Puissance de 2 >= 2.
const mcBlowup = 8

// mcNumQueries est le nombre de positions interrogées (cohérence colonnes /
// composition / DEEP). La soundness décroît exponentiellement avec ce nombre.
const mcNumQueries = 32

// ---------------------------------------------------------------------------
// Description d'un AIR
// ---------------------------------------------------------------------------

// Boundary décrit une contrainte de BORD : la cellule (Row, Col) de la trace
// DOIT valoir Value (une valeur publique). Row in [0, NumSteps) et
// Col in [0, NumColumns).
type Boundary struct {
	Col   int  // colonne contrainte, dans [0, NumColumns)
	Row   int  // ligne contrainte, dans [0, NumSteps)
	Value Felt // valeur publique imposée à trace[Row][Col]
}

// AIR décrit un système de contraintes algébriques sur une trace rectangulaire
// (NumColumns colonnes × NumSteps lignes). Une implémentation est PURE et
// DÉTERMINISTE : EvalTransition ne dépend que de ses arguments.
//
// Contrat de degré : EvalTransition DOIT être une fonction POLYNOMIALE des
// entrées (cur, next) de degré total ≤ MaxDegree. C'est cette borne qui
// dimensionne le domaine LDE et la borne de bas degré prouvée par FRI. Un AIR
// qui mentirait sur MaxDegree (degré réel supérieur) produirait une composition
// de degré trop élevé, que FRI rejetterait — la soundness n'est donc pas
// compromise, seule la complétude le serait.
type AIR interface {
	// NumColumns est la largeur W de la trace (nombre de colonnes), >= 1.
	NumColumns() int

	// NumSteps est la hauteur n de la trace (nombre de lignes). DOIT être une
	// puissance de 2 (le domaine de trace est un sous-groupe 2-adique).
	NumSteps() int

	// EvalTransition évalue les contraintes de transition sur une paire de lignes
	// consécutives : cur[c] = trace[i][c], next[c] = trace[i+1][c]. Renvoie un
	// vecteur de longueur FIXE (le nombre de contraintes de transition), dont
	// chaque composante DOIT être nulle pour toute paire de lignes consécutives
	// valides i in [0, NumSteps-2]. La longueur du vecteur ne dépend PAS de
	// l'entrée (même nombre de contraintes à chaque ligne).
	EvalTransition(cur, next []Felt) []Felt

	// Boundaries renvoie la liste des contraintes de bord (cellules fixées à des
	// valeurs publiques). Peut être vide.
	Boundaries() []Boundary

	// MaxDegree est le degré total maximal (en les variables cur/next) des
	// contraintes de transition renvoyées par EvalTransition. Sert à dimensionner
	// le domaine LDE. DOIT être >= 1.
	MaxDegree() int
}

// ---------------------------------------------------------------------------
// Preuve
// ---------------------------------------------------------------------------

// AirProof est la preuve STARK multi-colonnes. Elle généralise FibProof : une
// racine d'engagement PAR colonne (au lieu d'une seule), et des valeurs hors-
// domaine PAR colonne en z et en g·z.
//
// Sérialisable en mémoire (la sérialisation octets éventuelle relève d'un étage
// supérieur).
type AirProof struct {
	// ColRoots[c] engage les évaluations LDE du polynôme de la colonne c.
	ColRoots [][32]byte
	// CompRoot engage les évaluations LDE du polynôme de composition H.
	CompRoot [32]byte

	// OodColZ[c]  = Tc(z)   : ouverture hors-domaine de la colonne c en z.
	// OodColGZ[c] = Tc(g·z) : ouverture de la colonne c à la ligne suivante.
	OodColZ  []Felt
	OodColGZ []Felt
	// OodHz = H(z) : ouverture hors-domaine de la composition.
	OodHz Felt

	// Fri est la preuve de proximité au bas degré du quotient DEEP P. Sa couche 0
	// (Fri.LayerRoots[0]) EST l'engagement de P, réutilisé pour la liaison.
	Fri FriProof

	// Openings[q] regroupe, pour la q-ème position interrogée, les ouvertures de
	// CHAQUE colonne, de H et de P à cette position du domaine LDE.
	Openings []AirOpening
}

// AirOpening est l'ouverture, à une position du domaine LDE, de toutes les
// colonnes, de la composition et du quotient DEEP, chacune avec son chemin de
// Merkle. Généralise FibOpening (qui n'avait qu'une colonne de trace).
type AirOpening struct {
	Pos int // position interrogée dans [0, bigN)

	// ColVals[c]  = Tc(ω_N^Pos) ; ColPaths[c] = chemin contre ColRoots[c].
	ColVals  []Felt
	ColPaths [][][32]byte

	CompVal  Felt       // H(ω_N^Pos)
	CompPath [][32]byte // chemin contre CompRoot

	DeepVal  Felt       // P(ω_N^Pos)
	DeepPath [][32]byte // chemin contre Fri.LayerRoots[0]
}

// ---------------------------------------------------------------------------
// Bornes de degré et tailles de domaine
// ---------------------------------------------------------------------------

// mcDegBound est la borne de bas degré effective D (puissance de 2) que FRI
// prouvera (D == bigN / mcBlowup) : la plus petite puissance de 2 >= MaxDegree·n.
// Elle domine deg(P) (voir l'analyse de degré en tête de fichier).
func mcDegBound(maxDegree, n int) int {
	return nextPow2(maxDegree * n)
}

// mcBigN renvoie la taille du domaine LDE : bigN = mcBlowup · mcDegBound. C'est
// une puissance de 2 (produit de deux puissances de 2).
func mcBigN(maxDegree, n int) int {
	return mcBlowup * mcDegBound(maxDegree, n)
}

// ---------------------------------------------------------------------------
// Construction des quotients de composition (côté prouveur, sur le LDE)
// ---------------------------------------------------------------------------
//
// Approche GÉNÉRIQUE : comme EvalTransition est une fonction Felt -> Felt (pas
// une opération sur polynômes en forme coefficients), on évalue les contraintes
// de transition POINT À POINT sur le domaine LDE, puis on interpole. Concrètement
// pour chaque point ω^j du domaine LDE on dispose de la ligne courante
// (colsLDE[c][j]) et de la ligne suivante (colsLDE[c][j+shift] où shift = bigN/n
// car g·ω^j = ω^(j+shift)). On appelle EvalTransition, on multiplie par
// (ω^j - g^(n-1)), on divise par Z_D(ω^j) = (ω^j)^n - 1, et on combine. C'est la
// méthode standard d'arithmétisation : la composition est obtenue sur le LDE,
// puis interpolée pour engagement et pour le quotient DEEP.

// mcEvalColumnsOnLDE interpole chaque colonne (degré < n) puis l'évalue sur le
// domaine LDE de taille bigN. Renvoie colsCoeffs[c] (coefficients, longueur n) et
// colsLDE[c] (évaluations, longueur bigN). Déterministe.
func mcEvalColumnsOnLDE(trace [][]Felt, w, n, bigN int) (colsCoeffs, colsLDE [][]Felt) {
	colsCoeffs = make([][]Felt, w)
	colsLDE = make([][]Felt, w)
	for c := 0; c < w; c++ {
		// Colonne c sous forme d'évaluations sur le domaine de trace D.
		colEvals := make([]Felt, n)
		for i := 0; i < n; i++ {
			colEvals[i] = trace[i][c]
		}
		coeffs := Interpolate(colEvals) // longueur n, ordre croissant
		colsCoeffs[c] = coeffs
		colsLDE[c] = evalOnLDE(coeffs, bigN)
	}
	return colsCoeffs, colsLDE
}

// mcCompositionLDE construit les évaluations LDE du polynôme de composition H sur
// le domaine de taille bigN, par évaluation point-à-point des contraintes.
//
//	H(ω^j) = Σ_k α_trans[k] · Qk(ω^j) + Σ_b α_bound[b] · Qb(ω^j)
//
// avec, en notant x = ω^j :
//
//	Qk(x) = Ck(x)·(x - g^(n-1)) / (x^n - 1)   [transition]
//	Qb(x) = (T_{col_b}(x) - value_b)/(x - g^{row_b})   [bord]
//
// Les Ck(x) proviennent de EvalTransition(ligne courante, ligne suivante) où la
// ligne suivante au point x = ω^j est lue à l'indice j+shift (shift = bigN/n).
//
// IMPORTANT (domaine LDE vs domaine de trace) : le domaine de trace D est inclus
// dans le domaine LDE (g = ω^shift). Aux points de D, (x^n - 1) = 0 : la division
// transition serait 0/0. On choisit donc un domaine LDE qui ÉVITE D, en décalant
// par un coset : on évalue sur le coset η·{ω^0,...} avec η = Generator(). Ainsi
// aucun point d'évaluation n'appartient à D, toutes les divisions sont définies,
// et les engagements/quotients DEEP restent cohérents (le vérifieur réévalue les
// mêmes points de coset).
func mcCompositionLDE(air AIR, colsCoeffs [][]Felt, g Felt, n, bigN int,
	alphaTrans, alphaBound []Felt, eta Felt) []Felt {

	w := air.NumColumns()
	boundaries := air.Boundaries()
	one := One()
	gN1 := g.Exp(uint64(n - 1))

	logBigN := log2(bigN)
	omega := RootOfUnity(logBigN) // racine d'ordre bigN
	shift := bigN / n             // g = ω^shift => ligne suivante à l'indice +shift

	// Évaluations de chaque colonne sur le COSET LDE {η·ω^j}.
	colsCoset := make([][]Felt, w)
	for c := 0; c < w; c++ {
		colsCoset[c] = evalOnCoset(colsCoeffs[c], bigN, eta)
	}

	comp := make([]Felt, bigN)

	// cur/next réutilisés pour éviter des allocations dans la boucle chaude.
	cur := make([]Felt, w)
	next := make([]Felt, w)

	x := eta // x = η·ω^0 = η au départ ; multiplié par ω à chaque pas.
	for j := 0; j < bigN; j++ {
		// Ligne courante et ligne suivante au point x = η·ω^j.
		jn := (j + shift) % bigN
		for c := 0; c < w; c++ {
			cur[c] = colsCoset[c][j]
			next[c] = colsCoset[c][jn]
		}

		// Dénominateur de la transition : Z_D(x) = x^n - 1. Jamais nul sur le
		// coset (η n'est pas dans D), donc inversible.
		zd := x.Exp(uint64(n)).Sub(one)
		zdInv := zd.Inv()
		// Facteur de relâchement de la dernière ligne : (x - g^(n-1)).
		relax := x.Sub(gN1)

		acc := Zero()

		// Contraintes de transition.
		cons := air.EvalTransition(cur, next)
		for k := 0; k < len(cons); k++ {
			// Qk(x) = Ck(x)·(x - g^(n-1)) / (x^n - 1).
			qk := cons[k].Mul(relax).Mul(zdInv)
			acc = acc.Add(alphaTrans[k].Mul(qk))
		}

		// Contraintes de bord.
		for b, bc := range boundaries {
			// Qb(x) = (T_col(x) - value)/(x - g^row).
			gRow := g.Exp(uint64(bc.Row))
			denom := x.Sub(gRow)
			// denom est non nul sur le coset (g^row est dans D, x ne l'est pas).
			qb := colsCoset[bc.Col][j].Sub(bc.Value).Mul(denom.Inv())
			acc = acc.Add(alphaBound[b].Mul(qb))
		}

		comp[j] = acc
		x = x.Mul(omega)
	}

	return comp
}

// evalOnCoset évalue un polynôme (coefficients, longueur <= bigN) sur le coset
// {η·ω^j : j in [0,bigN)} du domaine LDE. On compose d'abord le polynôme avec la
// mise à l'échelle x -> η·x (coeff de degré i ·= η^i), puis NTT classique sur le
// domaine {ω^j}. Déterministe.
func evalOnCoset(coeffs []Felt, bigN int, eta Felt) []Felt {
	scaled := polyComposeScale(coeffs, eta) // a(η·x)
	return evalOnLDE(scaled, bigN)          // évalue a(η·ω^j) = (a∘(η·))(ω^j)
}

// ---------------------------------------------------------------------------
// Contrôle algébrique des contraintes au point hors-domaine z (vérifieur)
// ---------------------------------------------------------------------------

// mcCheckConstraintsAtZ recalcule au point hors-domaine z la combinaison des
// quotients de contrainte à partir des valeurs hors-domaine annoncées (colonnes
// en z et g·z, plus H(z)), et la compare à Hz :
//
//	Qk(z)  = Ck(z)·(z - g^(n-1)) / (z^n - 1)            [Ck via EvalTransition(colZ, colGZ)]
//	Qb(z)  = (colZ[col_b] - value_b)/(z - g^{row_b})    [bord]
//	attendu = Σ α_trans[k]·Qk(z) + Σ α_bound[b]·Qb(z)
//
// Renvoie true ssi attendu == Hz. Si un dénominateur s'annule (z tombe sur un
// point de bord ou sur D — proba négligeable), on rejette proprement (false),
// jamais de panique.
func mcCheckConstraintsAtZ(air AIR, z, g Felt, n int,
	alphaTrans, alphaBound []Felt, colZ, colGZ []Felt, Hz Felt) bool {

	one := One()
	gN1 := g.Exp(uint64(n - 1))

	zn := z.Exp(uint64(n))
	denTrans := zn.Sub(one) // z^n - 1
	if denTrans.IsZero() {
		return false
	}
	denTransInv := denTrans.Inv()
	relax := z.Sub(gN1)

	acc := Zero()

	// Transition : EvalTransition appliqué aux valeurs OOD (ligne courante = colZ,
	// ligne suivante = colGZ).
	cons := air.EvalTransition(colZ, colGZ)
	if len(cons) != len(alphaTrans) {
		return false // AIR incohérent entre prouveur et vérifieur
	}
	for k := 0; k < len(cons); k++ {
		qk := cons[k].Mul(relax).Mul(denTransInv)
		acc = acc.Add(alphaTrans[k].Mul(qk))
	}

	// Bord.
	boundaries := air.Boundaries()
	if len(boundaries) != len(alphaBound) {
		return false
	}
	for b, bc := range boundaries {
		gRow := g.Exp(uint64(bc.Row))
		denom := z.Sub(gRow)
		if denom.IsZero() {
			return false
		}
		qb := colZ[bc.Col].Sub(bc.Value).Mul(denom.Inv())
		acc = acc.Add(alphaBound[b].Mul(qb))
	}

	return acc.Equal(Hz)
}

// ---------------------------------------------------------------------------
// Quotient DEEP P(x)
// ---------------------------------------------------------------------------

// mcBuildDeep construit le quotient DEEP (forme coefficients) :
//
//	P(x) = Σ_c γz[c] ·(Tc(x) - Tc(z)) /(x - z)
//	     + Σ_c γgz[c]·(Tc(x) - Tc(gz))/(x - gz)
//	     + γH       ·(H(x)  - H(z))   /(x - z)
//
// Chaque terme est un polynôme EXACT ssi la valeur hors-domaine annoncée est bien
// l'évaluation du polynôme engagé au point correspondant.
func mcBuildDeep(colsCoeffs [][]Felt, compCoeffs []Felt, z, gz Felt,
	colZ, colGZ []Felt, Hz Felt, gammaZ, gammaGZ []Felt, gammaH Felt) []Felt {

	acc := []Felt{}
	for c := range colsCoeffs {
		t0, _ := polyDivByLinear(polySubConst(colsCoeffs[c], colZ[c]), z)
		acc = polyAddScaled(acc, t0, gammaZ[c])
		t1, _ := polyDivByLinear(polySubConst(colsCoeffs[c], colGZ[c]), gz)
		acc = polyAddScaled(acc, t1, gammaGZ[c])
	}
	h0, _ := polyDivByLinear(polySubConst(compCoeffs, Hz), z)
	acc = polyAddScaled(acc, h0, gammaH)
	return polyTrim(acc)
}

// mcDeepCombineAt recalcule, côté vérifieur, P(x) à un point x du domaine LDE
// (coset) à partir des valeurs ouvertes des colonnes Tc(x), de H(x) et des
// valeurs hors-domaine. Même combinaison que mcBuildDeep, ponctuelle. En cas de
// dénominateur nul (x coïncide avec z ou g·z — proba négligeable), renvoie une
// valeur sentinelle non liée à l'ouverture honnête (rejet implicite).
func mcDeepCombineAt(x Felt, colVals []Felt, Hx, z, gz Felt,
	colZ, colGZ []Felt, Hz Felt, gammaZ, gammaGZ []Felt, gammaH Felt) Felt {

	d0 := x.Sub(z)
	d1 := x.Sub(gz)
	if d0.IsZero() || d1.IsZero() {
		// Sentinelle déterministe non liée à l'ouverture DEEP honnête.
		acc := Hx.Add(One())
		for _, v := range colVals {
			acc = acc.Add(v)
		}
		return acc
	}
	d0Inv := d0.Inv()
	d1Inv := d1.Inv()

	acc := Zero()
	for c := range colVals {
		t0 := gammaZ[c].Mul(colVals[c].Sub(colZ[c]).Mul(d0Inv))
		t1 := gammaGZ[c].Mul(colVals[c].Sub(colGZ[c]).Mul(d1Inv))
		acc = acc.Add(t0).Add(t1)
	}
	acc = acc.Add(gammaH.Mul(Hx.Sub(Hz).Mul(d0Inv)))
	return acc
}

// ---------------------------------------------------------------------------
// Défis du transcript (déterministes)
// ---------------------------------------------------------------------------

// mcIndexedLabel forge une étiquette de défi distincte par index :
// "<base>/<index>". Déterministe ; garantit des défis indépendants par
// contrainte/colonne (étiquettes différentes => flux SHAKE différents).
func mcIndexedLabel(base string, index int) string {
	return base + "/" + strconv.Itoa(index)
}

// mcDrawAlphas tire les coefficients de combinaison des contraintes : un par
// contrainte de transition, puis un par contrainte de bord. Étiquettes indexées
// distinctes => défis indépendants. L'ordre (transition avant bord) est partagé
// par prouveur et vérifieur.
func mcDrawAlphas(tr *Transcript, numTrans, numBound int) (alphaTrans, alphaBound []Felt) {
	alphaTrans = make([]Felt, numTrans)
	for k := 0; k < numTrans; k++ {
		alphaTrans[k] = tr.Challenge(mcIndexedLabel("mc/alpha-trans", k))
	}
	alphaBound = make([]Felt, numBound)
	for b := 0; b < numBound; b++ {
		alphaBound[b] = tr.Challenge(mcIndexedLabel("mc/alpha-bound", b))
	}
	return alphaTrans, alphaBound
}

// mcDrawGammas tire les coefficients de combinaison DEEP : deux par colonne
// (terme en z et terme en g·z) plus un pour H.
func mcDrawGammas(tr *Transcript, w int) (gammaZ, gammaGZ []Felt, gammaH Felt) {
	gammaZ = make([]Felt, w)
	gammaGZ = make([]Felt, w)
	for c := 0; c < w; c++ {
		gammaZ[c] = tr.Challenge(mcIndexedLabel("mc/gamma-z", c))
		gammaGZ[c] = tr.Challenge(mcIndexedLabel("mc/gamma-gz", c))
	}
	gammaH = tr.Challenge("mc/gamma-h")
	return gammaZ, gammaGZ, gammaH
}

// ---------------------------------------------------------------------------
// Prouveur
// ---------------------------------------------------------------------------

// ProveAIR construit une preuve STARK multi-colonnes pour l'AIR fourni, la trace
// donnée (trace[step][col]) et les valeurs publiques (public...).
//
// Les valeurs publiques sont absorbées dans le transcript (elles font partie de
// l'énoncé) ; elles servent typiquement à fixer/recouper les valeurs des
// contraintes de bord. Le vérifieur DOIT les rejouer à l'identique.
//
// Contrats (panique sinon) : air.NumSteps() puissance de 2 >= 4 ; air.NumColumns()
// >= 1 ; air.MaxDegree() >= 1 ; trace de dimensions exactes NumSteps × NumColumns.
//
// Si la trace ne satisfait PAS les contraintes (transition ou bord), ProveAIR
// construit tout de même une preuve, mais le polynôme de composition n'est pas de
// bas degré et la vérification ÉCHOUERA (objet des tests de soundness). ProveAIR
// ne « ment » jamais : il engage ce qu'il calcule.
func ProveAIR(air AIR, trace [][]Felt, public ...Felt) AirProof {
	w := air.NumColumns()
	n := air.NumSteps()
	maxDeg := air.MaxDegree()

	if !isPow2(n) || n < 4 {
		panic("stark: ProveAIR: NumSteps doit être une puissance de 2 >= 4")
	}
	if w < 1 {
		panic("stark: ProveAIR: NumColumns doit être >= 1")
	}
	if maxDeg < 1 {
		panic("stark: ProveAIR: MaxDegree doit être >= 1")
	}
	if len(trace) != n {
		panic("stark: ProveAIR: la trace doit avoir NumSteps lignes")
	}
	for i := range trace {
		if len(trace[i]) != w {
			panic("stark: ProveAIR: chaque ligne de trace doit avoir NumColumns colonnes")
		}
	}

	logN := log2(n)
	g := RootOfUnity(logN) // générateur du domaine de trace, ordre n
	bigN := mcBigN(maxDeg, n)
	logBigN := log2(bigN)
	eta := Generator() // décalage de coset : évite le domaine de trace D

	boundaries := air.Boundaries()
	numBound := len(boundaries)

	// --- 1) Interpolation des colonnes + LDE (sur le domaine {ω^j}) ---
	colsCoeffs, colsLDE := mcEvalColumnsOnLDE(trace, w, n, bigN)

	// --- 2) Engagement de chaque colonne ---
	colRoots := make([][32]byte, w)
	colTrees := make([]*MerkleTree, w)
	for c := 0; c < w; c++ {
		root, tree := commitEvals(colsLDE[c])
		colRoots[c] = root
		colTrees[c] = tree
	}

	// --- 3) Transcript : énoncé public + engagements de colonnes ---
	tr := NewTranscript(mcDomain)
	tr.AbsorbFelt("mc/num-columns", FromUint64(uint64(w)))
	tr.AbsorbFelt("mc/num-steps", FromUint64(uint64(n)))
	tr.AbsorbFelt("mc/max-degree", FromUint64(uint64(maxDeg)))
	tr.AbsorbFelt("mc/blowup", FromUint64(uint64(mcBlowup)))
	tr.AbsorbFelt("mc/num-queries", FromUint64(uint64(mcNumQueries)))
	tr.AbsorbFelt("mc/num-bound", FromUint64(uint64(numBound)))
	mcAbsorbBoundaries(tr, boundaries)
	tr.AbsorbFelt("mc/num-public", FromUint64(uint64(len(public))))
	for i, p := range public {
		tr.AbsorbFelt("mc/public", p)
		_ = i
	}
	for c := 0; c < w; c++ {
		tr.Absorb("mc/col-root", colRoots[c][:])
	}

	// On a besoin du nombre de contraintes de transition pour tirer les α : on
	// l'obtient en évaluant EvalTransition sur la première paire de lignes (sa
	// longueur est fixe par contrat). La trace a au moins 2 lignes (n >= 4).
	numTrans := len(air.EvalTransition(trace[0], trace[1]))

	// --- 4) Défis de combinaison des contraintes ---
	alphaTrans, alphaBound := mcDrawAlphas(tr, numTrans, numBound)

	// --- 5) Composition H sur le coset LDE, puis coefficients ---
	compCoset := mcCompositionLDE(air, colsCoeffs, g, n, bigN, alphaTrans, alphaBound, eta)
	// Interpolation depuis le coset : H(η·ω^j) = compCoset[j]. On interpole sur
	// {ω^j} pour obtenir H(η·x), puis on « dé-scale » par η pour récupérer H(x).
	compScaled := Interpolate(compCoset)                  // coefficients de H(η·x)
	compCoeffs := polyComposeScale(compScaled, eta.Inv()) // H(x) = H(η·x) avec x->x/η

	// --- 6) LDE (sur {ω^j}) et engagement de la composition ---
	compLDE := evalOnLDE(compCoeffs, bigN)
	compRoot, compTree := commitEvals(compLDE)
	tr.Absorb("mc/comp-root", compRoot[:])

	// --- 7) Point hors-domaine z et valeurs hors-domaine ---
	z := tr.Challenge("mc/ood-z")
	gz := g.Mul(z)
	oodColZ := make([]Felt, w)
	oodColGZ := make([]Felt, w)
	for c := 0; c < w; c++ {
		oodColZ[c] = evalNaïfPoly(colsCoeffs[c], z)
		oodColGZ[c] = evalNaïfPoly(colsCoeffs[c], gz)
	}
	oodHz := evalNaïfPoly(compCoeffs, z)
	for c := 0; c < w; c++ {
		tr.AbsorbFelt("mc/ood-col-z", oodColZ[c])
		tr.AbsorbFelt("mc/ood-col-gz", oodColGZ[c])
	}
	tr.AbsorbFelt("mc/ood-hz", oodHz)

	// --- 8) Défis DEEP γ ---
	gammaZ, gammaGZ, gammaH := mcDrawGammas(tr, w)

	// --- 9) Quotient DEEP P(x) ---
	deepCoeffs := mcBuildDeep(colsCoeffs, compCoeffs, z, gz,
		oodColZ, oodColGZ, oodHz, gammaZ, gammaGZ, gammaH)

	// --- 10) LDE de P et preuve FRI de bas degré ---
	deepLDE := evalOnLDE(deepCoeffs, bigN)
	friProof := proveFRImc(deepLDE)
	deepTree := buildDeepTree(deepLDE)

	// --- 11) Positions de requête (transcript STARK) et ouvertures ---
	absorbFriDigest(tr, friProof)
	positions := tr.ChallengeIndices("mc/query", mcNumQueries, bigN)

	openings := make([]AirOpening, len(positions))
	for i, pos := range positions {
		colVals := make([]Felt, w)
		colPaths := make([][][32]byte, w)
		for c := 0; c < w; c++ {
			colVals[c] = colsLDE[c][pos]
			colPaths[c] = Open(colTrees[c], pos)
		}
		openings[i] = AirOpening{
			Pos:      pos,
			ColVals:  colVals,
			ColPaths: colPaths,
			CompVal:  compLDE[pos],
			CompPath: Open(compTree, pos),
			DeepVal:  deepLDE[pos],
			DeepPath: Open(deepTree, pos),
		}
	}

	_ = logBigN // implicite via les indices LDE.

	return AirProof{
		ColRoots: colRoots,
		CompRoot: compRoot,
		OodColZ:  oodColZ,
		OodColGZ: oodColGZ,
		OodHz:    oodHz,
		Fri:      friProof,
		Openings: openings,
	}
}

// proveFRImc lance le prouveur FRI sur les évaluations LDE du quotient DEEP avec
// les paramètres multi-colonnes. Couche 0 = engagement de deepLDE.
func proveFRImc(deepLDE []Felt) FriProof {
	return Prove(deepLDE, FriParams{Blowup: mcBlowup, NumQueries: mcNumQueries})
}

// mcAbsorbBoundaries absorbe la liste des contraintes de bord dans le transcript
// (elles font partie de l'énoncé public). Ordre et contenu fixés => prouveur et
// vérifieur produisent les mêmes défis.
func mcAbsorbBoundaries(tr *Transcript, boundaries []Boundary) {
	for _, bc := range boundaries {
		tr.AbsorbFelt("mc/bound-col", FromUint64(uint64(bc.Col)))
		tr.AbsorbFelt("mc/bound-row", FromUint64(uint64(bc.Row)))
		tr.AbsorbFelt("mc/bound-val", bc.Value)
	}
}

// ---------------------------------------------------------------------------
// Vérifieur
// ---------------------------------------------------------------------------

// VerifyAIR rejoue le transcript et vérifie la preuve STARK multi-colonnes pour
// l'AIR et les valeurs publiques fournies. Renvoie true ssi TOUTES les
// vérifications passent :
//
//   - structure bien formée (dimensions, tailles cohérentes) ;
//   - la preuve FRI vérifie (P est de bas degré) ;
//   - la couche 0 de FRI est l'engagement de P (liaison) ;
//   - le contrôle algébrique hors-domaine en z lie H aux contraintes sur les
//     colonnes (transition + bord) ;
//   - à chaque position interrogée, les ouvertures de chaque colonne, de H et de
//     P sont authentiques (Merkle) et P(x_pos) est exactement la combinaison
//     DEEP des valeurs ouvertes.
//
// Les valeurs publiques DOIVENT coïncider avec celles utilisées par le prouveur,
// sans quoi le transcript diverge et la preuve est rejetée. Ne panique JAMAIS sur
// preuve falsifiée : rejet propre (return false).
func VerifyAIR(air AIR, proof AirProof, public ...Felt) bool {
	w := air.NumColumns()
	n := air.NumSteps()
	maxDeg := air.MaxDegree()

	// --- Contrôles structurels ---
	if !isPow2(n) || n < 4 || w < 1 || maxDeg < 1 {
		return false
	}
	logN := log2(n)
	g := RootOfUnity(logN)
	bigN := mcBigN(maxDeg, n)
	if !isPow2(bigN) {
		return false
	}
	logBigN := log2(bigN)
	// Note : eta (décalage de coset) n'intervient PAS côté vérifieur. Le coset est
	// un artefact PUREMENT prouveur pour éviter la division 0/0 lors du calcul de
	// H ; les engagements (colonnes, H, P) sont sur le domaine {ω^j}, et le
	// vérifieur ne réévalue que des ouvertures sur ce domaine et l'identité en z.

	boundaries := air.Boundaries()
	numBound := len(boundaries)

	// Tailles des slices de la preuve.
	if len(proof.ColRoots) != w {
		return false
	}
	if len(proof.OodColZ) != w || len(proof.OodColGZ) != w {
		return false
	}

	friParams := FriParams{Blowup: mcBlowup, NumQueries: mcNumQueries}

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

	if len(proof.Openings) != mcNumQueries {
		return false
	}

	// --- Rejoue du transcript : mêmes absorptions, mêmes défis ---
	tr := NewTranscript(mcDomain)
	tr.AbsorbFelt("mc/num-columns", FromUint64(uint64(w)))
	tr.AbsorbFelt("mc/num-steps", FromUint64(uint64(n)))
	tr.AbsorbFelt("mc/max-degree", FromUint64(uint64(maxDeg)))
	tr.AbsorbFelt("mc/blowup", FromUint64(uint64(mcBlowup)))
	tr.AbsorbFelt("mc/num-queries", FromUint64(uint64(mcNumQueries)))
	tr.AbsorbFelt("mc/num-bound", FromUint64(uint64(numBound)))
	mcAbsorbBoundaries(tr, boundaries)
	tr.AbsorbFelt("mc/num-public", FromUint64(uint64(len(public))))
	for _, p := range public {
		tr.AbsorbFelt("mc/public", p)
	}
	for c := 0; c < w; c++ {
		tr.Absorb("mc/col-root", proof.ColRoots[c][:])
	}

	// Nombre de contraintes de transition : déduit du vecteur OOD via une
	// évaluation factice ? Non : on l'obtient de la longueur du résultat de
	// EvalTransition sur des entrées arbitraires (la longueur est fixe par
	// contrat). On utilise des zéros (déterministe, indépendant de la preuve).
	zeroRow := make([]Felt, w)
	numTrans := len(air.EvalTransition(zeroRow, zeroRow))

	// --- Défis de combinaison ---
	alphaTrans, alphaBound := mcDrawAlphas(tr, numTrans, numBound)

	tr.Absorb("mc/comp-root", proof.CompRoot[:])

	z := tr.Challenge("mc/ood-z")
	gz := g.Mul(z)
	for c := 0; c < w; c++ {
		tr.AbsorbFelt("mc/ood-col-z", proof.OodColZ[c])
		tr.AbsorbFelt("mc/ood-col-gz", proof.OodColGZ[c])
	}
	tr.AbsorbFelt("mc/ood-hz", proof.OodHz)

	gammaZ, gammaGZ, gammaH := mcDrawGammas(tr, w)

	absorbFriDigest(tr, proof.Fri)
	positions := tr.ChallengeIndices("mc/query", mcNumQueries, bigN)

	// --- Contrôle algébrique hors-domaine (cohérence des contraintes en z) ---
	if !mcCheckConstraintsAtZ(air, z, g, n, alphaTrans, alphaBound,
		proof.OodColZ, proof.OodColGZ, proof.OodHz) {
		return false
	}

	// --- Requêtes : authenticité Merkle + cohérence DEEP par position ---
	// Le domaine d'engagement est {ω^j} ; les ouvertures DEEP/colonnes/H sont des
	// évaluations sur ce domaine (PAS le coset). Le quotient DEEP P a été engagé
	// sur {ω^j}, et mcDeepCombineAt recalcule P(ω^pos) à partir des ouvertures.
	omegaN := RootOfUnity(logBigN)
	for i, pos := range positions {
		op := proof.Openings[i]
		if op.Pos != pos {
			return false
		}
		if pos < 0 || pos >= bigN {
			return false
		}
		if len(op.ColVals) != w || len(op.ColPaths) != w {
			return false
		}

		// 1) Authenticité des ouvertures de chaque colonne contre sa racine.
		for c := 0; c < w; c++ {
			if !VerifyPath(proof.ColRoots[c], pos, leafOf(op.ColVals[c]), op.ColPaths[c]) {
				return false
			}
		}
		// Authenticité de H et de P.
		if !VerifyPath(proof.CompRoot, pos, leafOf(op.CompVal), op.CompPath) {
			return false
		}
		if !VerifyPath(deepRoot, pos, leafOf(op.DeepVal), op.DeepPath) {
			return false
		}

		// 2) Cohérence DEEP : P(ω^pos) doit égaler la combinaison DEEP des valeurs
		//    ouvertes (colonnes, H) et des valeurs hors-domaine annoncées.
		x := omegaN.Exp(uint64(pos))
		expected := mcDeepCombineAt(x, op.ColVals, op.CompVal, z, gz,
			proof.OodColZ, proof.OodColGZ, proof.OodHz, gammaZ, gammaGZ, gammaH)
		if !expected.Equal(op.DeepVal) {
			return false
		}
	}

	return true
}
