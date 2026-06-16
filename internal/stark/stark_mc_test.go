// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Tests du STARK DEEP-ALI MULTI-COLONNES générique (stark_mc.go). On y prouve un
// AIR JOUET à 2 colonnes (deux suites couplées a'=a+b, b'=a), puis on vérifie :
//   - une preuve honnête passe ;
//   - une valeur publique fausse est rejetée ;
//   - toute falsification (colonne, composition H, chemin de Merkle, valeur
//     hors-domaine) est rejetée.
//
// On recoupe systématiquement la trace prouvée avec un calcul direct.
package stark

import "testing"

// ---------------------------------------------------------------------------
// AIR jouet : deux suites couplées
// ---------------------------------------------------------------------------
//
// Deux colonnes a et b avec :
//
//	a[0] = a0, b[0] = b0                 (bords de départ, publics)
//	a[i+1] = a[i] + b[i]                 (transition, colonne a)
//	b[i+1] = a[i]                        (transition, colonne b)
//	a[n-1] = lastA                       (bord de sortie, public)
//
// Les transitions sont LINÉAIRES (degré 1). EvalTransition reçoit (cur, next) et
// renvoie les deux résidus devant s'annuler :
//
//	r0 = next[0] - (cur[0] + cur[1])     (a'=a+b)
//	r1 = next[1] - cur[0]                (b'=a)
type coupledAIR struct {
	n      int
	a0, b0 Felt
	lastA  Felt
}

func (c coupledAIR) NumColumns() int { return 2 }
func (c coupledAIR) NumSteps() int   { return c.n }
func (c coupledAIR) MaxDegree() int  { return 1 } // transitions linéaires

func (c coupledAIR) EvalTransition(cur, next []Felt) []Felt {
	r0 := next[0].Sub(cur[0].Add(cur[1])) // a' - (a + b)
	r1 := next[1].Sub(cur[0])             // b' - a
	return []Felt{r0, r1}
}

func (c coupledAIR) Boundaries() []Boundary {
	return []Boundary{
		{Col: 0, Row: 0, Value: c.a0},          // a[0] = a0
		{Col: 1, Row: 0, Value: c.b0},          // b[0] = b0
		{Col: 0, Row: c.n - 1, Value: c.lastA}, // a[n-1] = lastA
	}
}

// buildCoupledTrace calcule DIRECTEMENT la trace (référence) des deux suites
// couplées. trace[i] = {a[i], b[i]}.
func buildCoupledTrace(a0, b0 Felt, n int) [][]Felt {
	trace := make([][]Felt, n)
	a, b := a0, b0
	for i := 0; i < n; i++ {
		trace[i] = []Felt{a, b}
		na := a.Add(b) // a' = a + b
		nb := a        // b' = a
		a, b = na, nb
	}
	return trace
}

// ---------------------------------------------------------------------------
// Test positif : preuve honnête acceptée, trace recoupée au calcul direct
// ---------------------------------------------------------------------------

func TestProveVerifyAIR_Coupled(t *testing.T) {
	n := 16
	a0 := FromUint64(1)
	b0 := FromUint64(1)

	trace := buildCoupledTrace(a0, b0, n)
	lastA := trace[n-1][0]

	// Recoupe : avec a0=b0=1, la colonne a est la suite de Fibonacci décalée.
	// On vérifie quelques valeurs connues à la main.
	// a: 1,2,3,5,8,13,... ; b: 1,1,2,3,5,8,...
	if !trace[0][0].Equal(FromUint64(1)) || !trace[0][1].Equal(FromUint64(1)) {
		t.Fatalf("trace départ inattendue: %v", trace[0])
	}
	if !trace[1][0].Equal(FromUint64(2)) || !trace[1][1].Equal(FromUint64(1)) {
		t.Fatalf("trace ligne 1 inattendue: %v", trace[1])
	}
	if !trace[2][0].Equal(FromUint64(3)) || !trace[2][1].Equal(FromUint64(2)) {
		t.Fatalf("trace ligne 2 inattendue: %v", trace[2])
	}
	if !trace[3][0].Equal(FromUint64(5)) || !trace[3][1].Equal(FromUint64(3)) {
		t.Fatalf("trace ligne 3 inattendue: %v", trace[3])
	}

	air := coupledAIR{n: n, a0: a0, b0: b0, lastA: lastA}

	// Les valeurs publiques absorbées : a0, b0, lastA (cohérentes avec les bords).
	proof := ProveAIR(air, trace, a0, b0, lastA)

	if !VerifyAIR(air, proof, a0, b0, lastA) {
		t.Fatalf("preuve honnête rejetée alors qu'elle devrait passer")
	}
}

// Déterminisme : deux preuves de la même instance sont bit-à-bit identiques
// (aléa = transcript uniquement).
func TestProveAIR_Deterministe(t *testing.T) {
	n := 16
	a0, b0 := FromUint64(3), FromUint64(7)
	trace := buildCoupledTrace(a0, b0, n)
	lastA := trace[n-1][0]
	air := coupledAIR{n: n, a0: a0, b0: b0, lastA: lastA}

	p1 := ProveAIR(air, trace, a0, b0, lastA)
	p2 := ProveAIR(air, trace, a0, b0, lastA)

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
	if !p1.OodHz.Equal(p2.OodHz) {
		t.Fatalf("OodHz non déterministe")
	}
}

// ---------------------------------------------------------------------------
// Tests négatifs
// ---------------------------------------------------------------------------

// Valeur publique fausse (lastA erroné) : le transcript diverge ET la contrainte
// de bord ne tient pas => rejet.
func TestVerifyAIR_MauvaisPublic(t *testing.T) {
	n := 16
	a0, b0 := FromUint64(1), FromUint64(1)
	trace := buildCoupledTrace(a0, b0, n)
	lastA := trace[n-1][0]
	air := coupledAIR{n: n, a0: a0, b0: b0, lastA: lastA}
	proof := ProveAIR(air, trace, a0, b0, lastA)

	// On vérifie avec un lastA faux dans l'AIR ET dans les valeurs publiques.
	wrongLast := lastA.Add(One())
	wrongAir := coupledAIR{n: n, a0: a0, b0: b0, lastA: wrongLast}
	if VerifyAIR(wrongAir, proof, a0, b0, wrongLast) {
		t.Fatalf("preuve acceptée avec une valeur publique fausse (lastA)")
	}

	// On vérifie avec un a0 public faux (transcript diverge) => rejet.
	if VerifyAIR(air, proof, a0.Add(One()), b0, lastA) {
		t.Fatalf("preuve acceptée avec a0 public faux")
	}
}

// Trace incorrecte : on fabrique une preuve à partir d'une trace qui VIOLE la
// transition (on casse une ligne). La composition n'est pas de bas degré => FRI
// et/ou OOD rejettent.
func TestProveVerifyAIR_TraceInvalide(t *testing.T) {
	n := 16
	a0, b0 := FromUint64(1), FromUint64(1)
	trace := buildCoupledTrace(a0, b0, n)
	lastA := trace[n-1][0]

	// Casse la transition au milieu : trace[8][0] devient n'importe quoi.
	trace[8][0] = trace[8][0].Add(FromUint64(12345))

	air := coupledAIR{n: n, a0: a0, b0: b0, lastA: lastA}
	proof := ProveAIR(air, trace, a0, b0, lastA)
	if VerifyAIR(air, proof, a0, b0, lastA) {
		t.Fatalf("preuve acceptée pour une trace violant la transition")
	}
}

// Falsification d'une valeur de colonne ouverte : l'ouverture Merkle ne
// correspond plus à la racine OU la combinaison DEEP diverge => rejet.
func TestVerifyAIR_FalsifieColonne(t *testing.T) {
	n := 16
	a0, b0 := FromUint64(2), FromUint64(5)
	trace := buildCoupledTrace(a0, b0, n)
	lastA := trace[n-1][0]
	air := coupledAIR{n: n, a0: a0, b0: b0, lastA: lastA}
	proof := ProveAIR(air, trace, a0, b0, lastA)

	// Sanity : honnête passe.
	if !VerifyAIR(air, proof, a0, b0, lastA) {
		t.Fatalf("préparation: preuve honnête rejetée")
	}

	// On corrompt la valeur de colonne 0 dans la première ouverture.
	bad := clonePoof(proof)
	bad.Openings[0].ColVals[0] = bad.Openings[0].ColVals[0].Add(One())
	if VerifyAIR(air, bad, a0, b0, lastA) {
		t.Fatalf("preuve acceptée avec une valeur de colonne falsifiée")
	}
}

// Falsification de la composition H ouverte.
func TestVerifyAIR_FalsifieComposition(t *testing.T) {
	n := 16
	a0, b0 := FromUint64(4), FromUint64(9)
	trace := buildCoupledTrace(a0, b0, n)
	lastA := trace[n-1][0]
	air := coupledAIR{n: n, a0: a0, b0: b0, lastA: lastA}
	proof := ProveAIR(air, trace, a0, b0, lastA)

	bad := clonePoof(proof)
	bad.Openings[0].CompVal = bad.Openings[0].CompVal.Add(One())
	if VerifyAIR(air, bad, a0, b0, lastA) {
		t.Fatalf("preuve acceptée avec une valeur de composition falsifiée")
	}
}

// Falsification d'un chemin de Merkle (on casse un nœud du chemin DEEP).
func TestVerifyAIR_FalsifieChemin(t *testing.T) {
	n := 16
	a0, b0 := FromUint64(6), FromUint64(1)
	trace := buildCoupledTrace(a0, b0, n)
	lastA := trace[n-1][0]
	air := coupledAIR{n: n, a0: a0, b0: b0, lastA: lastA}
	proof := ProveAIR(air, trace, a0, b0, lastA)

	bad := clonePoof(proof)
	if len(bad.Openings[0].DeepPath) == 0 {
		t.Fatalf("chemin DEEP vide, test inopérant")
	}
	bad.Openings[0].DeepPath[0][0] ^= 0xFF // bascule un octet
	if VerifyAIR(air, bad, a0, b0, lastA) {
		t.Fatalf("preuve acceptée avec un chemin de Merkle falsifié")
	}

	// Idem sur le chemin d'une colonne.
	bad2 := clonePoof(proof)
	if len(bad2.Openings[0].ColPaths[0]) == 0 {
		t.Fatalf("chemin colonne vide, test inopérant")
	}
	bad2.Openings[0].ColPaths[0][0][0] ^= 0xFF
	if VerifyAIR(air, bad2, a0, b0, lastA) {
		t.Fatalf("preuve acceptée avec un chemin de colonne falsifié")
	}
}

// Falsification d'une valeur hors-domaine (OOD) : le contrôle algébrique en z
// échoue (et/ou le transcript diverge) => rejet.
func TestVerifyAIR_FalsifieOOD(t *testing.T) {
	n := 16
	a0, b0 := FromUint64(1), FromUint64(2)
	trace := buildCoupledTrace(a0, b0, n)
	lastA := trace[n-1][0]
	air := coupledAIR{n: n, a0: a0, b0: b0, lastA: lastA}
	proof := ProveAIR(air, trace, a0, b0, lastA)

	// OOD de colonne en z.
	bad := clonePoof(proof)
	bad.OodColZ[0] = bad.OodColZ[0].Add(One())
	if VerifyAIR(air, bad, a0, b0, lastA) {
		t.Fatalf("preuve acceptée avec OodColZ falsifié")
	}

	// OOD de H en z.
	bad2 := clonePoof(proof)
	bad2.OodHz = bad2.OodHz.Add(One())
	if VerifyAIR(air, bad2, a0, b0, lastA) {
		t.Fatalf("preuve acceptée avec OodHz falsifié")
	}

	// OOD de colonne en g·z.
	bad3 := clonePoof(proof)
	bad3.OodColGZ[1] = bad3.OodColGZ[1].Add(One())
	if VerifyAIR(air, bad3, a0, b0, lastA) {
		t.Fatalf("preuve acceptée avec OodColGZ falsifié")
	}
}

// Une seule colonne (W=1) doit aussi fonctionner : AIR « identité décalée »
// b'=b (constante) avec bord de départ. Vérifie la généralité du cadre.
func TestProveVerifyAIR_UneColonne(t *testing.T) {
	n := 8
	// Colonne unique constante : x[i+1] = x[i]. Transition r0 = next[0]-cur[0].
	air := constAIR{n: n, start: FromUint64(42)}
	trace := make([][]Felt, n)
	for i := 0; i < n; i++ {
		trace[i] = []Felt{FromUint64(42)}
	}
	proof := ProveAIR(air, trace, FromUint64(42))
	if !VerifyAIR(air, proof, FromUint64(42)) {
		t.Fatalf("preuve mono-colonne honnête rejetée")
	}
	// Mauvais départ public => rejet.
	if VerifyAIR(constAIR{n: n, start: FromUint64(43)}, proof, FromUint64(43)) {
		t.Fatalf("preuve mono-colonne acceptée avec mauvais départ")
	}
}

// AIR de degré 2 : chaîne de mise au carré x[i+1] = x[i]^2. Exerce la logique de
// borne de degré (MaxDegree=2 => bigN plus grand) et la composition sur coset
// pour un degré non trivial. Deux colonnes : x (carré itéré) et y (copie décalée
// y'=x), pour rester multi-colonnes avec un degré mixte (la colonne y est
// linéaire, la colonne x est quadratique : MaxDegree global = 2).
type squareAIR struct {
	n     int
	start Felt
	last  Felt
}

func (s squareAIR) NumColumns() int { return 2 }
func (s squareAIR) NumSteps() int   { return s.n }
func (s squareAIR) MaxDegree() int  { return 2 } // x'=x^2 est de degré 2

func (s squareAIR) EvalTransition(cur, next []Felt) []Felt {
	r0 := next[0].Sub(cur[0].Mul(cur[0])) // x' - x^2
	r1 := next[1].Sub(cur[0])             // y' - x
	return []Felt{r0, r1}
}
func (s squareAIR) Boundaries() []Boundary {
	return []Boundary{
		{Col: 0, Row: 0, Value: s.start},
		{Col: 0, Row: s.n - 1, Value: s.last},
	}
}

func buildSquareTrace(start Felt, n int) [][]Felt {
	trace := make([][]Felt, n)
	x := start
	var prevX Felt
	for i := 0; i < n; i++ {
		var y Felt
		if i == 0 {
			y = Zero() // y[0] libre (pas de bord) ; on met 0
		} else {
			y = prevX
		}
		trace[i] = []Felt{x, y}
		prevX = x
		x = x.Mul(x) // x' = x^2
	}
	return trace
}

func TestProveVerifyAIR_Degre2(t *testing.T) {
	n := 8
	start := FromUint64(3)
	trace := buildSquareTrace(start, n)
	last := trace[n-1][0]

	// Recoupe directe : 3, 9, 81, ... (mod p).
	if !trace[1][0].Equal(FromUint64(9)) {
		t.Fatalf("x[1] attendu 9, obtenu %d", trace[1][0].Uint64())
	}
	if !trace[2][0].Equal(FromUint64(81)) {
		t.Fatalf("x[2] attendu 81, obtenu %d", trace[2][0].Uint64())
	}

	air := squareAIR{n: n, start: start, last: last}
	proof := ProveAIR(air, trace, start, last)
	if !VerifyAIR(air, proof, start, last) {
		t.Fatalf("preuve degré-2 honnête rejetée")
	}
	// Mauvaise sortie publique => rejet.
	if VerifyAIR(squareAIR{n: n, start: start, last: last.Add(One())}, proof, start, last.Add(One())) {
		t.Fatalf("preuve degré-2 acceptée avec mauvaise sortie")
	}
	// Trace violant le carré => rejet.
	badTrace := buildSquareTrace(start, n)
	badTrace[3][0] = badTrace[3][0].Add(One())
	badProof := ProveAIR(air, badTrace, start, last)
	if VerifyAIR(air, badProof, start, last) {
		t.Fatalf("preuve degré-2 acceptée pour une trace invalide")
	}
}

type constAIR struct {
	n     int
	start Felt
}

func (c constAIR) NumColumns() int { return 1 }
func (c constAIR) NumSteps() int   { return c.n }
func (c constAIR) MaxDegree() int  { return 1 }
func (c constAIR) EvalTransition(cur, next []Felt) []Felt {
	return []Felt{next[0].Sub(cur[0])}
}
func (c constAIR) Boundaries() []Boundary {
	return []Boundary{{Col: 0, Row: 0, Value: c.start}}
}

// ---------------------------------------------------------------------------
// Utilitaire de test : copie profonde d'une AirProof pour falsifier sans
// contaminer l'original.
// ---------------------------------------------------------------------------

func clonePoof(p AirProof) AirProof {
	out := p // copie superficielle
	out.ColRoots = append([][32]byte(nil), p.ColRoots...)
	out.OodColZ = append([]Felt(nil), p.OodColZ...)
	out.OodColGZ = append([]Felt(nil), p.OodColGZ...)
	out.Openings = make([]AirOpening, len(p.Openings))
	for i, op := range p.Openings {
		no := op
		no.ColVals = append([]Felt(nil), op.ColVals...)
		no.ColPaths = make([][][32]byte, len(op.ColPaths))
		for c, path := range op.ColPaths {
			no.ColPaths[c] = append([][32]byte(nil), path...)
		}
		no.CompPath = append([][32]byte(nil), op.CompPath...)
		no.DeepPath = append([][32]byte(nil), op.DeepPath...)
		out.Openings[i] = no
	}
	return out
}
