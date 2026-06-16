// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Corps premier de Goldilocks p = 2^64 - 2^32 + 1.
//
// Ce paquet implémente l'arithmétique du corps fini utilisée par notre
// prototype de STARK. Aucune dépendance hors bibliothèque standard +
// golang.org/x/crypto/sha3 (ce dernier n'est pas utilisé ici, mais dans les
// étages supérieurs). PAS de CGO, AUCUN appel à time / math/rand : tout est
// déterministe.
//
// Le module Goldilocks a deux propriétés très commodes :
//   - p = 2^64 - 2^32 + 1 tient sur 64 bits, donc un Felt = un uint64.
//   - p - 1 = 2^32 * 3 * 5 * 17 * 257 * 65537, donc le groupe multiplicatif
//     contient un sous-groupe 2-adique d'ordre 2^32 : idéal pour la NTT.
package stark

import (
	"encoding/binary"
	"math/bits"
)

// P est le module de Goldilocks : 2^64 - 2^32 + 1.
const P uint64 = 0xFFFFFFFF00000001

// epsilon = 2^32 - 1 = 2^64 mod p. Sert à la réduction rapide :
// puisque 2^64 ≡ 2^32 - 1 (mod p), on peut replier la partie haute d'un
// produit 128 bits en ajoutant des multiples de epsilon.
const epsilon uint64 = 0xFFFFFFFF // = 2^32 - 1

// Felt est un élément du corps de Goldilocks, représenté par son résidu
// canonique dans [0, P). Toutes les opérations exportées préservent cet
// invariant (la valeur retournée est toujours réduite).
type Felt uint64

// FromUint64 réduit un uint64 quelconque dans [0, P) et renvoie le Felt
// correspondant. Comme un uint64 peut valoir au plus 2^64 - 1 et que
// P = 2^64 - 2^32 + 1, un seul repli suffit.
func FromUint64(x uint64) Felt {
	if x >= P {
		x -= P
	}
	return Felt(x)
}

// Uint64 renvoie la représentation canonique (déjà réduite) du Felt.
func (a Felt) Uint64() uint64 {
	return uint64(a)
}

// Zero et One sont les constantes neutres du corps.
func Zero() Felt { return Felt(0) }
func One() Felt  { return Felt(1) }

// IsZero indique si le Felt est l'élément neutre additif.
func (a Felt) IsZero() bool {
	return uint64(a) == 0
}

// Equal compare deux Felt. Comme la représentation est canonique, une simple
// égalité de uint64 suffit.
func (a Felt) Equal(b Felt) bool {
	return uint64(a) == uint64(b)
}

// Add additionne deux éléments du corps avec réduction.
func (a Felt) Add(b Felt) Felt {
	// a, b < P < 2^64. La somme peut dépasser 2^64 : on capte la retenue.
	sum, carry := bits.Add64(uint64(a), uint64(b), 0)
	// Si retenue, le vrai total est sum + 2^64 ≡ sum + (2^32 - 1) (mod p).
	if carry != 0 {
		// sum + 2^64 - p = sum + epsilon, sans débordement supplémentaire
		// possible car sum < 2^64 et epsilon est petit ; on réduit ensuite.
		sum += epsilon
	}
	if sum >= P {
		sum -= P
	}
	return Felt(sum)
}

// Sub soustrait b à a avec réduction.
func (a Felt) Sub(b Felt) Felt {
	diff, borrow := bits.Sub64(uint64(a), uint64(b), 0)
	// Si emprunt, le résultat « réel » est diff - 2^64 ≡ diff - (2^32 - 1)
	// (mod p) ; on retire epsilon pour revenir dans [0, P).
	if borrow != 0 {
		diff -= epsilon
	}
	return Felt(diff)
}

// Neg renvoie l'opposé additif.
func (a Felt) Neg() Felt {
	if a.IsZero() {
		return Felt(0)
	}
	return Felt(P - uint64(a))
}

// reduce128 réduit un entier 128 bits (hi:lo) modulo P et renvoie un uint64
// dans [0, P). C'est le cœur de la multiplication Goldilocks.
//
// Décomposons hi en deux moitiés de 32 bits : hi = hiHi*2^32 + hiLo.
// On a, modulo p :
//
//	2^64 ≡ 2^32 - 1
//	2^96 ≡ 2^64 * 2^32 ≡ (2^32 - 1) * 2^32 ≡ 2^64 - 2^32 ≡ -1
//
// Donc :
//
//	hi*2^64 = hiLo*2^64 + hiHi*2^96
//	        ≡ hiLo*(2^32 - 1) - hiHi   (mod p)
//
// On combine avec lo en gérant retenues/emprunts sur 64 bits.
func reduce128(lo, hi uint64) uint64 {
	hiHi := hi >> 32        // 32 bits de poids fort
	hiLo := hi & 0xFFFFFFFF // 32 bits de poids faible

	// t0 = lo - hiHi (≡ lo + hiHi*(-1))
	t0, borrow := bits.Sub64(lo, hiHi, 0)
	if borrow != 0 {
		// Emprunt : on a retiré 2^64, soit ≡ -(2^32 - 1) = -epsilon (mod p),
		// donc pour compenser on retire epsilon.
		t0 -= epsilon
	}

	// t1 = hiLo*(2^32 - 1) = hiLo*2^32 - hiLo. Comme hiLo < 2^32, hiLo*2^32
	// tient sur 64 bits.
	t1 := hiLo * epsilon // epsilon = 2^32 - 1, donc hiLo*epsilon = hiLo*2^32 - hiLo

	// res = t0 + t1, avec gestion de retenue (repli +epsilon).
	res, carry := bits.Add64(t0, t1, 0)
	if carry != 0 {
		res += epsilon
	}
	if res >= P {
		res -= P
	}
	return res
}

// Mul multiplie deux éléments du corps avec réduction Goldilocks.
func (a Felt) Mul(b Felt) Felt {
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	return Felt(reduce128(lo, hi))
}

// Exp élève a à la puissance e (exponentiation rapide, square-and-multiply).
// e est un uint64 ; pour des exposants plus grands utiliser ExpFelt non
// fourni (inutile ici).
func (a Felt) Exp(e uint64) Felt {
	result := One()
	base := a
	for e > 0 {
		if e&1 == 1 {
			result = result.Mul(base)
		}
		base = base.Mul(base)
		e >>= 1
	}
	return result
}

// Inv renvoie l'inverse multiplicatif via le petit théorème de Fermat :
// a^(p-2) ≡ a^{-1} (mod p) pour a != 0. L'inverse de 0 n'est pas défini ;
// on renvoie 0 par convention (le code appelant ne doit jamais inverser 0).
func (a Felt) Inv() Felt {
	if a.IsZero() {
		return Felt(0)
	}
	// p - 2 = 2^64 - 2^32 - 1.
	return a.Exp(P - 2)
}

// Div divise a par b (a * b^{-1}). Commodité pour les étages supérieurs.
func (a Felt) Div(b Felt) Felt {
	return a.Mul(b.Inv())
}

// Bytes sérialise le Felt en 8 octets big-endian (représentation canonique).
// Le big-endian est choisi pour un transcript Fiat-Shamir lisible et stable.
func (a Felt) Bytes() []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(a))
	return buf[:]
}

// FeltFromBytes lit 8 octets big-endian et renvoie le Felt réduit. Les
// entrées hors [0, P) sont repliées (réduction), ce qui garantit que tout
// flux d'octets décode vers un élément valide du corps.
func FeltFromBytes(b []byte) Felt {
	var buf [8]byte
	// Copie défensive : on tolère un slice plus court (complété par des zéros
	// à gauche) ou plus long (on ne lit que les 8 premiers octets).
	copy(buf[8-min(len(b), 8):], b[:min(len(b), 8)])
	return FromUint64(binary.BigEndian.Uint64(buf[:]))
}

// ---------------------------------------------------------------------------
// Racines de l'unité (sous-groupe 2-adique)
// ---------------------------------------------------------------------------

// twoAdicity est la 2-adicité de p-1 : p - 1 = 2^32 * cofactor.
const twoAdicity uint32 = 32

// generator est un générateur du groupe multiplicatif F_p^* (ordre p-1).
// La valeur 7 est un générateur connu et largement utilisé pour Goldilocks
// (Polygon/Plonky2 emploient la même). On la vérifie dans les tests.
const generator uint64 = 7

// twoAdicRootOfUnity est une racine primitive 2^32-ième de l'unité, obtenue
// par g^((p-1)/2^32). C'est le générateur du plus grand sous-groupe 2-adique.
//
// On la calcule à l'initialisation du paquet (déterministe, pas de hasard) :
// (p-1)/2^32 = cofactor.
func twoAdicRoot() Felt {
	// cofactor = (p-1) >> 32.
	cofactor := (P - 1) >> twoAdicity
	return Felt(generator).Exp(cofactor)
}

// Generator renvoie le générateur du groupe multiplicatif sous forme de Felt.
// Exposé surtout pour les tests et le débogage.
func Generator() Felt {
	return Felt(generator)
}

// TwoAdicity renvoie la 2-adicité de p-1 (ici 32) : logN maximal supporté par
// RootOfUnity.
func TwoAdicity() uint32 {
	return twoAdicity
}

// RootOfUnity renvoie une racine primitive 2^logN-ième de l'unité, c.-à-d. un
// élément ω tel que ω^(2^logN) = 1 et ω^(2^(logN-1)) != 1.
//
// logN doit être dans [0, 32]. Pour logN == 0 on renvoie 1 (racine d'ordre 1).
// Panique si logN > 32 : aucun sous-groupe 2-adique aussi grand n'existe dans
// Goldilocks, c'est une erreur de programmation côté appelant.
func RootOfUnity(logN uint32) Felt {
	if logN > twoAdicity {
		panic("stark: RootOfUnity: logN dépasse la 2-adicité (32) de Goldilocks")
	}
	// On part de la racine 2^32-ième et on l'élève au carré (32 - logN) fois
	// pour descendre à l'ordre 2^logN : (ω_{2^32})^(2^(32-logN)) = ω_{2^logN}.
	root := twoAdicRoot()
	for i := logN; i < twoAdicity; i++ {
		root = root.Mul(root)
	}
	return root
}
