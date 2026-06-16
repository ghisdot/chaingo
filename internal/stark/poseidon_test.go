// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Tests du hash algébrique Poseidon sur Goldilocks :
//   - DÉTERMINISME : mêmes entrées => mêmes sorties (Permute, Hash, Hash2).
//   - VECTEURS FIXES capturés (codés en dur) : toute régression de la
//     permutation, du padding, du schéma d'éponge ou de la dérivation des
//     paramètres casse ces tests. NB : ces vecteurs valident la NON-RÉGRESSION,
//     pas la sécurité (paramètres non audités).
//   - AVALANCHE : un seul bit d'entrée renversé change ≳ la moitié des bits de
//     sortie (≈128/256 bits attendus), preuve empirique de diffusion.
//   - SENSIBILITÉ À L'ORDRE : Hash2(a,b) != Hash2(b,a) ; Hash([a,b]) != Hash([b,a]).
//   - TESTS NÉGATIFS / anti-collision : une entrée altérée d'un epsilon, un
//     padding implicite (suffixe de zéros), ou une longueur différente, NE
//     DOIVENT PAS produire le même digest.
//   - PROPRIÉTÉS structurelles : la matrice MDS est inversible (donc bien MDS au
//     sens faible vérifié ici : déterminant non nul) et la S-box est bijective.
//
// Déterministe : aucun time/math/rand ; PRNG explicite à graine fixe.
package stark

import (
	"math/bits"
	"testing"
)

// ---------------------------------------------------------------------------
// Vecteurs de test FIXES (capturés depuis l'implémentation de référence v1).
// Toute modification non intentionnelle de Permute/Hash/Hash2 ou de la
// dérivation des paramètres fera échouer ces comparaisons.
// ---------------------------------------------------------------------------

var (
	// Permute([0,1,...,11]).
	wantPermuteIdent = [poseidonWidth]uint64{
		0x7a84cad5b2f9d015, 0x6b81632303fd77bd, 0xc25c209e5db9cdf7, 0xc0987c21da391bf4,
		0x8c3f1bece8a612f2, 0x8241f79088a117a3, 0x81b164ae3fc4153c, 0xa5695b89d17e8a57,
		0x3c7c789ce92cefa0, 0x9d146888e43c36aa, 0x554f31558c20b3e9, 0xc6fd669fa31d1ecd,
	}
	// Permute([0,0,...,0]).
	wantPermuteZero = [poseidonWidth]uint64{
		0xc6dfb9202a9b4217, 0xc5bcdfd2e6362a04, 0x2e64047baf3337f0, 0x7c0a29d8c9c5db0d,
		0xafc26e13f7e13b97, 0x4d5a2622f2c4d20e, 0xb01bacb066283403, 0x74a281ab67213f41,
		0xae948276d79170e4, 0x73f4f8d5a65413f9, 0xbf48eed1ee0e8747, 0x2fe44d31ba4fd99f,
	}

	wantHashEmpty = [poseidonDigestLen]uint64{
		0x5850c97444ea225c, 0x9f2d7381db679442, 0xb62bb42f4888136e, 0x23e62d752a96e95c,
	}
	wantHash123 = [poseidonDigestLen]uint64{
		0x27f8a783724736c6, 0x5faf7af97bffaf30, 0xc1f861350e696a09, 0x2a5bc924a76cc47f,
	}
	// Hash(0..9) : 10 Felt + séparateur => 2 blocs => 2 permutations.
	wantHash0to9 = [poseidonDigestLen]uint64{
		0x7cd0770d3dbe102c, 0x68455290554284f8, 0x6972c12bc76db398, 0x4d6b640f519519bf,
	}
	// Hash2([1,2,3,4],[5,6,7,8]).
	wantHash2AB = [poseidonDigestLen]uint64{
		0xc67fa0187d51ee88, 0x6ab7e3371d4bed93, 0xe9c2c17cc59668ac, 0x84a931af8e317a79,
	}
)

// digestOf est une commodité de test pour fabriquer un [4]Felt depuis 4 uint64.
func digestOf(a, b, c, d uint64) [poseidonDigestLen]Felt {
	return [poseidonDigestLen]Felt{FromUint64(a), FromUint64(b), FromUint64(c), FromUint64(d)}
}

// stateFromU64 fabrique un état de permutation depuis 12 uint64.
func stateFromU64(v [poseidonWidth]uint64) [poseidonWidth]Felt {
	var s [poseidonWidth]Felt
	for i := range v {
		s[i] = FromUint64(v[i])
	}
	return s
}

func eqStateU64(got [poseidonWidth]Felt, want [poseidonWidth]uint64) bool {
	for i := range got {
		if got[i].Uint64() != want[i] {
			return false
		}
	}
	return true
}

func eqDigestU64(got [poseidonDigestLen]Felt, want [poseidonDigestLen]uint64) bool {
	for i := range got {
		if got[i].Uint64() != want[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Vecteurs fixes.
// ---------------------------------------------------------------------------

func TestPoseidonVecteursFixes(t *testing.T) {
	identIn := stateFromU64([poseidonWidth]uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11})
	if got := Permute(identIn); !eqStateU64(got, wantPermuteIdent) {
		t.Fatalf("Permute(0..11) régression : got %v", got)
	}

	var zero [poseidonWidth]Felt
	if got := Permute(zero); !eqStateU64(got, wantPermuteZero) {
		t.Fatalf("Permute(0...0) régression : got %v", got)
	}

	if got := Hash(nil); !eqDigestU64(got, wantHashEmpty) {
		t.Fatalf("Hash(nil) régression : got %v", got)
	}
	if got := Hash([]Felt{FromUint64(1), FromUint64(2), FromUint64(3)}); !eqDigestU64(got, wantHash123) {
		t.Fatalf("Hash([1,2,3]) régression : got %v", got)
	}

	in10 := make([]Felt, 10)
	for i := range in10 {
		in10[i] = FromUint64(uint64(i))
	}
	if got := Hash(in10); !eqDigestU64(got, wantHash0to9) {
		t.Fatalf("Hash(0..9) régression : got %v", got)
	}

	a := digestOf(1, 2, 3, 4)
	b := digestOf(5, 6, 7, 8)
	if got := Hash2(a, b); !eqDigestU64(got, wantHash2AB) {
		t.Fatalf("Hash2(a,b) régression : got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Déterminisme.
// ---------------------------------------------------------------------------

func TestPoseidonDeterminisme(t *testing.T) {
	rng := newPRNG(0x05E1D000) // graine fixe arbitraire
	for iter := 0; iter < 200; iter++ {
		// Permute : deux appels identiques => même sortie.
		var st [poseidonWidth]Felt
		for i := range st {
			st[i] = rng.felt()
		}
		if Permute(st) != Permute(st) {
			t.Fatalf("Permute non déterministe à l'itération %d", iter)
		}

		// Hash : deux appels identiques => même digest.
		n := int(rng.next() % 20)
		in := make([]Felt, n)
		for i := range in {
			in[i] = rng.felt()
		}
		if Hash(in) != Hash(in) {
			t.Fatalf("Hash non déterministe à l'itération %d (n=%d)", iter, n)
		}

		// Hash2 : idem.
		var l, r [poseidonDigestLen]Felt
		for i := range l {
			l[i] = rng.felt()
			r[i] = rng.felt()
		}
		if Hash2(l, r) != Hash2(l, r) {
			t.Fatalf("Hash2 non déterministe à l'itération %d", iter)
		}
	}
}

// TestPoseidonParamsDeterministes vérifie qu'une re-dérivation des paramètres
// (matrice + constantes) reproduit EXACTEMENT l'instance du paquet : la
// dérivation SHAKE256 + rejet de biais est donc reproductible.
func TestPoseidonParamsDeterministes(t *testing.T) {
	re := deriveParams()
	if re.mds != params.mds {
		t.Fatal("matrice MDS non reproductible (re-dérivation divergente)")
	}
	if re.roundConstants != params.roundConstants {
		t.Fatal("constantes de ronde non reproductibles (re-dérivation divergente)")
	}
}

// ---------------------------------------------------------------------------
// Avalanche : un bit d'entrée renversé change ≳ la moitié des bits de sortie.
// ---------------------------------------------------------------------------

// hammingDigest compte les bits différents entre deux digests (256 bits).
func hammingDigest(a, b [poseidonDigestLen]Felt) int {
	d := 0
	for i := range a {
		d += bits.OnesCount64(a[i].Uint64() ^ b[i].Uint64())
	}
	return d
}

func TestPoseidonAvalanche(t *testing.T) {
	rng := newPRNG(0xA7A1A4)
	const totalBits = poseidonDigestLen * 64 // 256
	const samples = 64

	sum := 0
	count := 0
	minSeen := totalBits + 1
	for s := 0; s < samples; s++ {
		// Entrée de 4 Felt aléatoires.
		in := []Felt{rng.felt(), rng.felt(), rng.felt(), rng.felt()}
		base := Hash(in)

		// On renverse un bit aléatoire d'une cellule aléatoire de l'entrée.
		cell := int(rng.next() % uint64(len(in)))
		bit := uint(rng.next() % 63) // bit dans [0,62] : reste < 2^63 < P, donc
		// le Felt résultant est toujours un résidu canonique valide.
		flipped := make([]Felt, len(in))
		copy(flipped, in)
		flipped[cell] = FromUint64(flipped[cell].Uint64() ^ (uint64(1) << bit))

		h := hammingDigest(base, Hash(flipped))
		// Garde-fou strict : un seul bit d'entrée ne doit JAMAIS laisser la
		// sortie quasi inchangée (rejette une diffusion défaillante).
		if h < totalBits/4 { // < 64 bits sur 256
			t.Fatalf("avalanche faible : %d bits changés sur %d (cell=%d bit=%d)", h, totalBits, cell, bit)
		}
		if h < minSeen {
			minSeen = h
		}
		sum += h
		count++
	}

	avg := float64(sum) / float64(count)
	// On attend une moyenne proche de 128/256. Bande large pour rester robuste
	// au bruit d'échantillonnage tout en détectant une diffusion cassée.
	if avg < 96 || avg > 160 {
		t.Fatalf("avalanche moyenne hors plage : %.1f bits (attendu ~128/256)", avg)
	}
	t.Logf("avalanche : moyenne %.1f bits / 256 (min observé %d) sur %d échantillons", avg, minSeen, count)
}

// ---------------------------------------------------------------------------
// Sensibilité à l'ordre.
// ---------------------------------------------------------------------------

func TestPoseidonHash2Ordre(t *testing.T) {
	a := digestOf(11, 22, 33, 44)
	b := digestOf(55, 66, 77, 88)
	if Hash2(a, b) == Hash2(b, a) {
		t.Fatal("Hash2(a,b) == Hash2(b,a) : la compression doit être sensible à l'ordre")
	}

	// L'égalité doit toutefois tenir pour des opérandes identiques (sanity).
	if Hash2(a, b) != Hash2(a, b) {
		t.Fatal("Hash2(a,b) doit être stable")
	}
}

func TestPoseidonHashOrdre(t *testing.T) {
	x := FromUint64(123456789)
	y := FromUint64(987654321)
	if Hash([]Felt{x, y}) == Hash([]Felt{y, x}) {
		t.Fatal("Hash([x,y]) == Hash([y,x]) : l'éponge doit être sensible à l'ordre")
	}
}

// ---------------------------------------------------------------------------
// Tests NÉGATIFS / anti-collision.
// ---------------------------------------------------------------------------

// TestPoseidonAntiCollisionPadding vérifie que le padding (séparateur « 1 » +
// encodage de longueur) empêche deux entrées de longueurs différentes mais de
// même préfixe de produire le même digest : en particulier, [a] et [a,0] ne
// doivent PAS collisionner (un schéma sans padding/longueur le ferait).
func TestPoseidonAntiCollisionPadding(t *testing.T) {
	a := FromUint64(42)

	cases := [][]Felt{
		{a},
		{a, Zero()},
		{a, Zero(), Zero()},
		{},       // vide
		{Zero()}, // un seul zéro
	}
	seen := map[[poseidonDigestLen]uint64]int{}
	for i, in := range cases {
		d := Hash(in)
		key := [poseidonDigestLen]uint64{d[0].Uint64(), d[1].Uint64(), d[2].Uint64(), d[3].Uint64()}
		if j, ok := seen[key]; ok {
			t.Fatalf("collision de padding entre les cas %d et %d (%v et %v)", j, i, cases[j], in)
		}
		seen[key] = i
	}
}

// TestPoseidonEntreeAlteree vérifie qu'altérer une seule cellule d'entrée d'un
// epsilon change le digest (rejet d'une entrée falsifiée).
func TestPoseidonEntreeAlteree(t *testing.T) {
	rng := newPRNG(0xBEEF)
	for iter := 0; iter < 100; iter++ {
		n := 1 + int(rng.next()%8)
		in := make([]Felt, n)
		for i := range in {
			in[i] = rng.felt()
		}
		base := Hash(in)

		// Altération : +1 sur une cellule aléatoire.
		k := int(rng.next() % uint64(n))
		tampered := make([]Felt, n)
		copy(tampered, in)
		tampered[k] = tampered[k].Add(One())

		if Hash(tampered) == base {
			t.Fatalf("itération %d : une entrée altérée produit le même digest", iter)
		}
	}
}

// TestPoseidonHash2EntreeAlteree : altérer un seul Felt d'un opérande de Hash2
// change le résultat.
func TestPoseidonHash2EntreeAlteree(t *testing.T) {
	a := digestOf(1, 2, 3, 4)
	b := digestOf(5, 6, 7, 8)
	base := Hash2(a, b)

	a2 := a
	a2[2] = a2[2].Add(One())
	if Hash2(a2, b) == base {
		t.Fatal("altération de l'opérande gauche de Hash2 non détectée")
	}

	b2 := b
	b2[0] = b2[0].Add(One())
	if Hash2(a, b2) == base {
		t.Fatal("altération de l'opérande droit de Hash2 non détectée")
	}
}

// ---------------------------------------------------------------------------
// Propriétés structurelles : S-box bijective, matrice MDS inversible.
// ---------------------------------------------------------------------------

// TestPoseidonSboxBijective vérifie sur un échantillon que x -> x^7 est injective
// (donc bijective sur un corps fini), condition d'inversibilité de la ronde.
func TestPoseidonSboxBijective(t *testing.T) {
	rng := newPRNG(0x5B0C)
	seen := map[uint64]uint64{}
	for i := 0; i < 5000; i++ {
		x := rng.felt()
		y := sbox(x).Uint64()
		if prev, ok := seen[y]; ok && prev != x.Uint64() {
			t.Fatalf("S-box non injective : %d et %d ont même image %d", prev, x.Uint64(), y)
		}
		seen[y] = x.Uint64()
	}
	// Vérifie aussi quelques valeurs explicites : 0->0, 1->1.
	if !sbox(Zero()).IsZero() {
		t.Fatal("sbox(0) != 0")
	}
	if sbox(One()).Uint64() != 1 {
		t.Fatal("sbox(1) != 1")
	}
}

// TestPoseidonMDSInversible vérifie que la matrice MDS est inversible (déterminant
// non nul) par élimination de Gauss dans le corps. Une matrice MDS de Cauchy
// l'est par construction ; ce test attrape une éventuelle erreur de dérivation.
func TestPoseidonMDSInversible(t *testing.T) {
	// Copie de travail de la matrice.
	var m [poseidonWidth][poseidonWidth]Felt = params.mds

	det := One()
	for col := 0; col < poseidonWidth; col++ {
		// Recherche d'un pivot non nul dans la colonne courante.
		pivot := -1
		for r := col; r < poseidonWidth; r++ {
			if !m[r][col].IsZero() {
				pivot = r
				break
			}
		}
		if pivot == -1 {
			t.Fatal("matrice MDS singulière : pivot nul (déterminant = 0)")
		}
		if pivot != col {
			m[pivot], m[col] = m[col], m[pivot]
			det = det.Neg() // un échange de lignes change le signe du déterminant
		}
		det = det.Mul(m[col][col])

		// Élimination sous le pivot.
		inv := m[col][col].Inv()
		for r := col + 1; r < poseidonWidth; r++ {
			factor := m[r][col].Mul(inv)
			if factor.IsZero() {
				continue
			}
			for c := col; c < poseidonWidth; c++ {
				m[r][c] = m[r][c].Sub(factor.Mul(m[col][c]))
			}
		}
	}
	if det.IsZero() {
		t.Fatal("déterminant de la matrice MDS nul : couche linéaire dégénérée")
	}
}

// TestPoseidonParamsDansLeCorps vérifie l'invariant que TOUTES les constantes et
// tous les coefficients MDS sont des résidus canoniques < P (le rejet de biais a
// bien fonctionné).
func TestPoseidonParamsDansLeCorps(t *testing.T) {
	for i := 0; i < poseidonWidth; i++ {
		for j := 0; j < poseidonWidth; j++ {
			if params.mds[i][j].Uint64() >= P {
				t.Fatalf("MDS[%d][%d] hors corps", i, j)
			}
		}
	}
	for r := 0; r < poseidonTotalRounds; r++ {
		for i := 0; i < poseidonWidth; i++ {
			if params.roundConstants[r][i].Uint64() >= P {
				t.Fatalf("RC[%d][%d] hors corps", r, i)
			}
		}
	}
}
