// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Tests du corps de Goldilocks : axiomes du corps, inverse, Fermat, racines de
// l'unité, sérialisation. Déterministe : les "aléas" sont un PRNG explicite à
// graine fixe (pas de math/rand global, pas de time).
package stark

import (
	"testing"
)

// prng est un générateur pseudo-aléatoire déterministe (xorshift64*) : il
// fournit des Felt « variés » pour les tests sans introduire de hasard non
// reproductible. Graine fixe => résultats bit-à-bit identiques à chaque run.
type prng struct {
	state uint64
}

func newPRNG(seed uint64) *prng {
	if seed == 0 {
		seed = 0x9E3779B97F4A7C15 // évite l'état absorbant 0
	}
	return &prng{state: seed}
}

func (p *prng) next() uint64 {
	x := p.state
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	p.state = x
	return x * 0x2545F4914F6CDD1D
}

func (p *prng) felt() Felt {
	return FromUint64(p.next())
}

func TestAddCommutatifAssociatif(t *testing.T) {
	rng := newPRNG(1)
	for i := 0; i < 10000; i++ {
		a, b, c := rng.felt(), rng.felt(), rng.felt()
		// Commutativité.
		if !a.Add(b).Equal(b.Add(a)) {
			t.Fatalf("add non commutatif : a=%d b=%d", a, b)
		}
		// Associativité.
		lhs := a.Add(b).Add(c)
		rhs := a.Add(b.Add(c))
		if !lhs.Equal(rhs) {
			t.Fatalf("add non associatif : a=%d b=%d c=%d", a, b, c)
		}
	}
}

func TestAddNeutreEtOpposé(t *testing.T) {
	rng := newPRNG(2)
	zero := Zero()
	for i := 0; i < 10000; i++ {
		a := rng.felt()
		if !a.Add(zero).Equal(a) {
			t.Fatalf("0 n'est pas neutre additif pour a=%d", a)
		}
		if !a.Add(a.Neg()).IsZero() {
			t.Fatalf("a + (-a) != 0 pour a=%d", a)
		}
		// Neg(Neg(a)) == a.
		if !a.Neg().Neg().Equal(a) {
			t.Fatalf("--a != a pour a=%d", a)
		}
	}
	// -0 == 0.
	if !zero.Neg().IsZero() {
		t.Fatal("-0 != 0")
	}
}

func TestSubCohérenteAvecAdd(t *testing.T) {
	rng := newPRNG(3)
	for i := 0; i < 10000; i++ {
		a, b := rng.felt(), rng.felt()
		// a - b == a + (-b).
		if !a.Sub(b).Equal(a.Add(b.Neg())) {
			t.Fatalf("a-b != a+(-b) : a=%d b=%d", a, b)
		}
		// (a - b) + b == a.
		if !a.Sub(b).Add(b).Equal(a) {
			t.Fatalf("(a-b)+b != a : a=%d b=%d", a, b)
		}
		// a - a == 0.
		if !a.Sub(a).IsZero() {
			t.Fatalf("a-a != 0 pour a=%d", a)
		}
	}
}

func TestMulCommutatifAssociatifDistributif(t *testing.T) {
	rng := newPRNG(4)
	one := One()
	for i := 0; i < 10000; i++ {
		a, b, c := rng.felt(), rng.felt(), rng.felt()
		// Commutativité.
		if !a.Mul(b).Equal(b.Mul(a)) {
			t.Fatalf("mul non commutatif : a=%d b=%d", a, b)
		}
		// Associativité.
		if !a.Mul(b).Mul(c).Equal(a.Mul(b.Mul(c))) {
			t.Fatalf("mul non associatif : a=%d b=%d c=%d", a, b, c)
		}
		// Distributivité : a*(b+c) == a*b + a*c.
		lhs := a.Mul(b.Add(c))
		rhs := a.Mul(b).Add(a.Mul(c))
		if !lhs.Equal(rhs) {
			t.Fatalf("distributivité KO : a=%d b=%d c=%d", a, b, c)
		}
		// Neutre multiplicatif.
		if !a.Mul(one).Equal(a) {
			t.Fatalf("1 n'est pas neutre multiplicatif pour a=%d", a)
		}
		// Absorbant.
		if !a.Mul(Zero()).IsZero() {
			t.Fatalf("a*0 != 0 pour a=%d", a)
		}
	}
}

// TestMulRéférenceBigInt vérifie Mul contre une multiplication 128 bits de
// référence calculée « à la main » via math/bits, pour valider reduce128.
func TestMulRéférenceBigInt(t *testing.T) {
	rng := newPRNG(5)
	for i := 0; i < 10000; i++ {
		a, b := rng.felt(), rng.felt()
		got := a.Mul(b)
		want := mulRef(uint64(a), uint64(b))
		if uint64(got) != want {
			t.Fatalf("Mul KO : a=%d b=%d got=%d want=%d", a, b, got, want)
		}
	}
	// Quelques cas limites explicites.
	cases := [][2]uint64{
		{0, 0}, {1, 1}, {P - 1, P - 1}, {P - 1, 2}, {0xFFFFFFFF, 0xFFFFFFFF},
		{P - 1, P - 1}, {2, P - 1}, {0xFFFFFFFF00000000, 0xFFFFFFFF},
	}
	for _, c := range cases {
		a := FromUint64(c[0])
		b := FromUint64(c[1])
		got := a.Mul(b)
		want := mulRef(uint64(a), uint64(b))
		if uint64(got) != want {
			t.Fatalf("Mul cas limite KO : a=%d b=%d got=%d want=%d", a, b, got, want)
		}
	}
}

// mulRef calcule (a*b) mod P par réduction 128 bits naïve mais indépendante
// de reduce128 : on effectue la division euclidienne du produit 128 bits par P
// via l'algorithme scolaire bit-à-bit. Lent mais sûr — sert de référence.
func mulRef(a, b uint64) uint64 {
	// Produit 128 bits.
	hi, lo := mul64(a, b)
	// Réduction (hi:lo) mod P par double-and-add bit par bit, de gauche à
	// droite sur les 128 bits.
	var r uint64 // reste courant, toujours < P
	for bit := 127; bit >= 0; bit-- {
		// r = (r*2 + bit_courant) mod P.
		// r*2 mod P :
		r2 := mod2x(r)
		var b0 uint64
		if bit >= 64 {
			b0 = (hi >> (bit - 64)) & 1
		} else {
			b0 = (lo >> bit) & 1
		}
		r = r2 + b0
		if r >= P {
			r -= P
		}
	}
	return r
}

// mod2x renvoie (2*r) mod P pour r < P, sans débordement.
func mod2x(r uint64) uint64 {
	// 2*r peut dépasser 2^64 ? r < P < 2^64 donc 2*r < 2^65 ; on gère via Add.
	d := r // on veut 2r = r + r
	res := d + d
	// Débordement possible uniquement si d >= 2^63 ; détecté car res < d.
	if res < d {
		// res a perdu 2^64 ≡ epsilon (mod p) ... mais on veut la vraie valeur
		// mod P : res_réel = res + 2^64. (res + 2^64) mod P = (res + epsilon)
		// mod P, à condition de re-réduire.
		res += epsilon
	}
	if res >= P {
		res -= P
	}
	return res
}

// mul64 renvoie le produit 128 bits de a et b (hi, lo) sans dépendre du paquet.
func mul64(a, b uint64) (hi, lo uint64) {
	const mask = 0xFFFFFFFF
	a0, a1 := a&mask, a>>32
	b0, b1 := b&mask, b>>32
	lolo := a0 * b0
	lohi := a0 * b1
	hilo := a1 * b0
	hihi := a1 * b1
	mid := (lolo >> 32) + (lohi & mask) + (hilo & mask)
	lo = (lolo & mask) | (mid << 32)
	hi = hihi + (lohi >> 32) + (hilo >> 32) + (mid >> 32)
	return hi, lo
}

func TestInvFermat(t *testing.T) {
	rng := newPRNG(6)
	one := One()
	for i := 0; i < 5000; i++ {
		a := rng.felt()
		if a.IsZero() {
			continue
		}
		inv := a.Inv()
		if !a.Mul(inv).Equal(one) {
			t.Fatalf("a*Inv(a) != 1 : a=%d inv=%d", a, inv)
		}
	}
	// Cas explicites.
	for _, v := range []uint64{1, 2, 3, P - 1, P - 2, 0xDEADBEEF} {
		a := FromUint64(v)
		if !a.Mul(a.Inv()).Equal(one) {
			t.Fatalf("a*Inv(a) != 1 pour a=%d", a)
		}
	}
	// Inv(1) == 1.
	if !one.Inv().Equal(one) {
		t.Fatal("Inv(1) != 1")
	}
}

// TestFermatPetitThéorème : a^(p-1) == 1 pour tout a != 0.
func TestFermatPetitThéorème(t *testing.T) {
	rng := newPRNG(7)
	one := One()
	for i := 0; i < 3000; i++ {
		a := rng.felt()
		if a.IsZero() {
			continue
		}
		if !a.Exp(P - 1).Equal(one) {
			t.Fatalf("a^(p-1) != 1 : a=%d", a)
		}
	}
}

func TestExpCohérence(t *testing.T) {
	rng := newPRNG(8)
	for i := 0; i < 2000; i++ {
		a := rng.felt()
		// a^0 == 1.
		if !a.Exp(0).Equal(One()) {
			t.Fatalf("a^0 != 1 pour a=%d", a)
		}
		// a^1 == a.
		if !a.Exp(1).Equal(a) {
			t.Fatalf("a^1 != a pour a=%d", a)
		}
		// a^2 == a*a.
		if !a.Exp(2).Equal(a.Mul(a)) {
			t.Fatalf("a^2 != a*a pour a=%d", a)
		}
		// a^(m+n) == a^m * a^n pour petits m,n.
		m := rng.next() % 50
		n := rng.next() % 50
		if !a.Exp(m + n).Equal(a.Exp(m).Mul(a.Exp(n))) {
			t.Fatalf("a^(m+n) != a^m*a^n : a=%d m=%d n=%d", a, m, n)
		}
	}
}

func TestSérialisationBytes(t *testing.T) {
	rng := newPRNG(9)
	for i := 0; i < 10000; i++ {
		a := rng.felt()
		b := a.Bytes()
		if len(b) != 8 {
			t.Fatalf("Bytes() longueur %d != 8", len(b))
		}
		got := FeltFromBytes(b)
		if !got.Equal(a) {
			t.Fatalf("aller-retour Bytes KO : a=%d got=%d", a, got)
		}
	}
	// Cas limites.
	for _, v := range []uint64{0, 1, P - 1} {
		a := Felt(v)
		if !FeltFromBytes(a.Bytes()).Equal(a) {
			t.Fatalf("aller-retour KO pour %d", v)
		}
	}
	// FeltFromBytes réduit bien une entrée hors corps (ex. P encodé brut).
	var pBytes [8]byte
	for i := 0; i < 8; i++ {
		pBytes[i] = byte(P >> (8 * (7 - i)))
	}
	if got := FeltFromBytes(pBytes[:]); !got.IsZero() {
		t.Fatalf("FeltFromBytes(P) devrait valoir 0, got=%d", got)
	}
}

func TestFromUint64Réduit(t *testing.T) {
	// Les valeurs >= P doivent être repliées.
	if got := FromUint64(P); !got.IsZero() {
		t.Fatalf("FromUint64(P) != 0, got=%d", got)
	}
	if got := FromUint64(P + 1); !got.Equal(One()) {
		t.Fatalf("FromUint64(P+1) != 1, got=%d", got)
	}
	// 2^64 - 1 = P + (2^32 - 2), donc se réduit en 2^32 - 2.
	if got := FromUint64(0xFFFFFFFFFFFFFFFF); uint64(got) != 0xFFFFFFFE {
		t.Fatalf("FromUint64(2^64-1) attendu 0xFFFFFFFE, got=%d", got)
	}
}

// ---------------------------------------------------------------------------
// Racines de l'unité
// ---------------------------------------------------------------------------

func TestGénérateurEstGénérateur(t *testing.T) {
	g := Generator()
	// g != 0, g != 1.
	if g.IsZero() || g.Equal(One()) {
		t.Fatal("le générateur ne doit être ni 0 ni 1")
	}
	// g^(p-1) == 1 (Fermat).
	if !g.Exp(P - 1).Equal(One()) {
		t.Fatal("g^(p-1) != 1")
	}
	// g est un vrai générateur ssi g^((p-1)/q) != 1 pour tout facteur premier
	// q de p-1. p-1 = 2^32 * 3 * 5 * 17 * 257 * 65537.
	primes := []uint64{2, 3, 5, 17, 257, 65537}
	for _, q := range primes {
		e := (P - 1) / q
		if g.Exp(e).Equal(One()) {
			t.Fatalf("g^((p-1)/%d) == 1 : 7 n'est pas un générateur", q)
		}
	}
}

func TestRacinesUnitéOrdreCorrect(t *testing.T) {
	one := One()
	for logN := uint32(0); logN <= 20; logN++ {
		w := RootOfUnity(logN)
		n := uint64(1) << logN
		// ω^n == 1.
		if !w.Exp(n).Equal(one) {
			t.Fatalf("ω^(2^%d) != 1", logN)
		}
		// Ordre exactement 2^logN : ω^(n/2) != 1 (sauf logN==0).
		if logN > 0 {
			if w.Exp(n / 2).Equal(one) {
				t.Fatalf("ω d'ordre 2^%d n'est pas primitive (ω^(n/2)==1)", logN)
			}
		} else {
			// logN==0 : ordre 1, donc ω == 1.
			if !w.Equal(one) {
				t.Fatalf("RootOfUnity(0) != 1, got=%d", w)
			}
		}
	}
}

// TestRacinesUnitéDistinctes : sur un sous-groupe d'ordre N, les puissances
// ω^0..ω^(N-1) sont toutes distinctes (sinon l'ordre serait < N).
func TestRacinesUnitéDistinctes(t *testing.T) {
	for _, logN := range []uint32{1, 2, 3, 8, 10} {
		w := RootOfUnity(logN)
		n := 1 << logN
		seen := make(map[uint64]bool, n)
		cur := One()
		for i := 0; i < n; i++ {
			if seen[uint64(cur)] {
				t.Fatalf("puissances de ω non distinctes pour logN=%d (collision à i=%d)", logN, i)
			}
			seen[uint64(cur)] = true
			cur = cur.Mul(w)
		}
		// Après N multiplications on revient à 1.
		if !cur.Equal(One()) {
			t.Fatalf("ω^N != 1 pour logN=%d", logN)
		}
	}
}

func TestRootOfUnityPaniqueTropGrand(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("RootOfUnity(33) aurait dû paniquer")
		}
	}()
	_ = RootOfUnity(33)
}
