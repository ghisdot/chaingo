// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Hash algébrique Poseidon sur le corps de Goldilocks (p = 2^64 - 2^32 + 1).
// Poseidon est une permutation « STARK-friendly » : sa S-box est une simple
// puissance x^α, ce qui la rend bon marché à exprimer comme contrainte
// polynomiale dans un AIR (à la différence de SHA3/Keccak, coûteux à
// arithmétiser). On l'utilise comme primitive de hachage interne au prototype
// STARK (engagements, futurs arbres de Merkle algébriques).
//
// ┌──────────────────────────────────────────────────────────────────────────┐
// │ AVERTISSEMENT — PARAMÈTRES CHOISIS PAR NOUS, À AUDITER.                     │
// │                                                                            │
// │ La largeur, le nombre de rondes et la S-box suivent le profil Plonky2/     │
// │ Goldilocks (t=12, x^7, R_F=8, R_P=22), qui bénéficie d'une analyse de      │
// │ sécurité publique. EN REVANCHE la matrice MDS et les constantes de ronde   │
// │ sont ICI dérivées par NOUS via SHAKE256 d'une graine documentée — ce ne    │
// │ sont PAS les constantes officielles de Plonky2. Notre construction est     │
// │ déterministe et reproductible, mais elle n'a fait l'objet d'AUCUN audit    │
// │ cryptographique. Les vecteurs de test capturés ne valident que la NON-     │
// │ RÉGRESSION, pas la sécurité. Ne pas utiliser en consensus / production.    │
// └──────────────────────────────────────────────────────────────────────────┘
//
// Déterminisme ABSOLU : aucune dépendance à time / math/rand. Tout le « hasard »
// (constantes, matrice) provient d'un SHAKE256 (golang.org/x/crypto/sha3,
// seule dépendance hors stdlib autorisée) ensemencé par une chaîne fixe, avec
// rejet déterministe des tirages >= p. Deux exécutions, sur deux machines,
// produisent exactement les mêmes paramètres et donc les mêmes digests.
package stark

import (
	"encoding/binary"

	"golang.org/x/crypto/sha3"
)

// ---------------------------------------------------------------------------
// Paramètres de l'instance Poseidon (profil Plonky2/Goldilocks).
// ---------------------------------------------------------------------------

const (
	// poseidonWidth (t) est la largeur de l'état de la permutation, en nombre de
	// Felt. t=12 est le choix de Plonky2 sur Goldilocks : il offre un bon
	// compromis débit/sécurité (capacité 4 => ~256 bits, débit 8).
	poseidonWidth = 12

	// poseidonAlpha est l'exposant de la S-box x^α. α=7 est premier avec p-1
	// sur Goldilocks (p-1 = 2^32·3·5·17·257·65537, non divisible par 7), donc
	// x -> x^7 est une BIJECTION du corps (permutation), condition nécessaire à
	// l'inversibilité de la ronde.
	poseidonAlpha = 7

	// poseidonFullRounds (R_F) est le nombre TOTAL de rondes pleines, réparti
	// moitié au début, moitié à la fin (R_F/2 = 4 de chaque côté). En ronde
	// pleine, la S-box s'applique à TOUTES les cellules.
	poseidonFullRounds = 8

	// poseidonPartialRounds (R_P) est le nombre de rondes partielles, intercalées
	// entre les deux blocs de rondes pleines. En ronde partielle, la S-box ne
	// s'applique qu'à UNE seule cellule (l'indice 0). Les rondes partielles
	// donnent l'essentiel de la sécurité algébrique à bas coût d'arithmétisation.
	poseidonPartialRounds = 22

	// poseidonTotalRounds est le nombre total de rondes (pleines + partielles).
	poseidonTotalRounds = poseidonFullRounds + poseidonPartialRounds

	// poseidonRate (débit) et poseidonCapacity (capacité) partitionnent l'état de
	// l'éponge : rate + capacity == width. Capacité 4 (~256 bits) borne la
	// sécurité aux collisions/préimages à ~128 bits (demi-capacité). Le digest
	// fait 4 Felt (≈256 bits).
	poseidonRate     = 8
	poseidonCapacity = 4

	// poseidonDigestLen est la taille d'un digest en Felt (== capacité).
	poseidonDigestLen = 4
)

// poseidonSeed est la graine documentée d'où dérivent DÉTERMINISTEMENT la
// matrice MDS et toutes les constantes de ronde, via SHAKE256. Toute
// modification de cette chaîne change l'ensemble des paramètres (et donc les
// digests) : elle est versionnée (« v1 ») et NE DOIT PAS changer sans migration.
const poseidonSeed = "ChainGO-Poseidon-Goldilocks-t12-v1"

// ---------------------------------------------------------------------------
// Dérivation déterministe des paramètres (matrice MDS + constantes de ronde).
// ---------------------------------------------------------------------------

// poseidonParams regroupe les paramètres figés de l'instance. Ils sont calculés
// UNE fois à l'initialisation du paquet (déterministe) et jamais mutés ensuite.
type poseidonParams struct {
	// mds est la matrice MDS t×t (Maximum Distance Separable) appliquée à la fin
	// de chaque ronde : couche de diffusion linéaire. Elle est construite comme
	// une matrice de Cauchy, MDS par construction (voir buildMDS).
	mds [poseidonWidth][poseidonWidth]Felt

	// roundConstants[r] est le vecteur de constantes ajouté (addRoundConstants)
	// au début de la ronde r, pour r dans [0, poseidonTotalRounds).
	roundConstants [poseidonTotalRounds][poseidonWidth]Felt
}

// params est l'instance unique, initialisée au chargement du paquet. L'init est
// purement déterministe : aucune horloge, aucun hasard externe.
var params = deriveParams()

// newXOF ouvre un flux SHAKE256 ensemencé par la graine documentée suivie d'une
// étiquette de domaine (séparation de domaine entre les différents usages :
// constantes vs lignes/colonnes de la matrice). Le cadrage longueur-préfixé
// rend l'amorçage injectif.
func newXOF(domain string) sha3.ShakeHash {
	xof := sha3.NewShake256()
	var lenBuf [8]byte

	binary.BigEndian.PutUint64(lenBuf[:], uint64(len(poseidonSeed)))
	_, _ = xof.Write(lenBuf[:])
	_, _ = xof.Write([]byte(poseidonSeed))

	binary.BigEndian.PutUint64(lenBuf[:], uint64(len(domain)))
	_, _ = xof.Write(lenBuf[:])
	_, _ = xof.Write([]byte(domain))

	return xof
}

// nextFelt tire le prochain Felt d'un flux SHAKE256 par rejet de biais : on lit
// des blocs de 8 octets (big-endian) et on rejette ceux >= P, jusqu'à obtenir
// un résidu uniformément distribué dans [0, P). Le rejet est nécessaire car
// 2^64 n'est pas un multiple de P (sinon biais en faveur des petites valeurs).
//
// Déterministe : pour un flux donné, la suite des Felt acceptés est fixe. La
// probabilité de rejet d'un bloc est (2^64 - P)/2^64 = (2^32 - 1)/2^64 ≈ 2^-32,
// donc on accepte quasiment toujours au premier bloc.
func nextFelt(xof sha3.ShakeHash) Felt {
	var buf [8]byte
	for {
		_, _ = xof.Read(buf[:])
		x := binary.BigEndian.Uint64(buf[:])
		if x < P { // rejet déterministe des tirages hors corps
			return Felt(x)
		}
	}
}

// buildRoundConstants dérive les R·t constantes de ronde depuis un flux SHAKE256
// dédié (domaine « round-constants »). Les constantes sont tirées dans l'ordre
// ronde par ronde, cellule par cellule, par rejet de biais.
func buildRoundConstants() [poseidonTotalRounds][poseidonWidth]Felt {
	xof := newXOF("round-constants")
	var rc [poseidonTotalRounds][poseidonWidth]Felt
	for r := 0; r < poseidonTotalRounds; r++ {
		for i := 0; i < poseidonWidth; i++ {
			rc[r][i] = nextFelt(xof)
		}
	}
	return rc
}

// buildMDS construit une matrice MDS t×t de type Cauchy. Une matrice de Cauchy
// M[i][j] = 1/(x_i - y_j) est MDS (tous ses mineurs carrés sont inversibles)
// dès que les x_i et y_j sont 2t éléments DISTINCTS du corps — propriété
// classique des matrices de Cauchy. Le caractère MDS garantit une diffusion
// linéaire maximale (distance de branchement t+1), exigence de sécurité de la
// couche linéaire de Poseidon.
//
// Les 2t éléments x_i, y_j sont tirés DÉTERMINISTEMENT d'un flux SHAKE256 dédié
// (domaine « mds-cauchy »), par rejet de biais, AVEC rejet supplémentaire de
// tout élément déjà tiré (pour garantir la distinction des 2t valeurs et donc
// l'inversibilité des dénominateurs x_i - y_j). La construction est
// reproductible bit-à-bit.
func buildMDS() [poseidonWidth][poseidonWidth]Felt {
	xof := newXOF("mds-cauchy")

	// Tirage de 2t éléments DISTINCTS : les t premiers sont les x_i, les t
	// suivants les y_j. On rejette tout doublon pour assurer x_i != x_k,
	// y_j != y_l ET x_i != y_j (sans quoi un dénominateur s'annulerait).
	const total = 2 * poseidonWidth
	var pts [total]Felt
	count := 0
	for count < total {
		cand := nextFelt(xof)
		dup := false
		for k := 0; k < count; k++ {
			if pts[k].Equal(cand) {
				dup = true
				break
			}
		}
		if !dup {
			pts[count] = cand
			count++
		}
	}

	var xs, ys [poseidonWidth]Felt
	for i := 0; i < poseidonWidth; i++ {
		xs[i] = pts[i]
		ys[i] = pts[poseidonWidth+i]
	}

	var mds [poseidonWidth][poseidonWidth]Felt
	for i := 0; i < poseidonWidth; i++ {
		for j := 0; j < poseidonWidth; j++ {
			// M[i][j] = (x_i - y_j)^{-1}. Le dénominateur est non nul car tous
			// les points sont distincts (voir tirage ci-dessus).
			mds[i][j] = xs[i].Sub(ys[j]).Inv()
		}
	}
	return mds
}

// deriveParams assemble l'instance Poseidon complète de façon déterministe.
func deriveParams() poseidonParams {
	return poseidonParams{
		mds:            buildMDS(),
		roundConstants: buildRoundConstants(),
	}
}

// ---------------------------------------------------------------------------
// Permutation.
// ---------------------------------------------------------------------------

// sbox applique la S-box x^α (α=7) à un Felt. Via Exp (square-and-multiply) :
// x^7 = x^4 · x^2 · x. C'est une bijection du corps (α premier avec p-1).
func sbox(x Felt) Felt {
	return x.Exp(poseidonAlpha)
}

// applyMDS applique la couche de diffusion linéaire : out = M · state. Chaque
// composante de sortie est le produit scalaire d'une ligne de M par l'état.
// Renvoie un NOUVEL état (ne mute pas l'entrée).
func applyMDS(state [poseidonWidth]Felt) [poseidonWidth]Felt {
	var out [poseidonWidth]Felt
	for i := 0; i < poseidonWidth; i++ {
		acc := Zero()
		row := &params.mds[i]
		for j := 0; j < poseidonWidth; j++ {
			acc = acc.Add(row[j].Mul(state[j]))
		}
		out[i] = acc
	}
	return out
}

// addRoundConstants ajoute (dans le corps) le vecteur de constantes de la ronde
// r à l'état. Mute l'état en place.
func addRoundConstants(state *[poseidonWidth]Felt, round int) {
	rc := &params.roundConstants[round]
	for i := 0; i < poseidonWidth; i++ {
		state[i] = state[i].Add(rc[i])
	}
}

// Permute applique la permutation Poseidon complète à un état de 12 Felt et
// renvoie le nouvel état. La structure des rondes est :
//
//	R_F/2 rondes PLEINES  (4)  : ARC -> S-box(toutes cellules) -> MDS
//	R_P   rondes PARTIELLES(22): ARC -> S-box(cellule 0 seule) -> MDS
//	R_F/2 rondes PLEINES  (4)  : ARC -> S-box(toutes cellules) -> MDS
//
// (ARC = addRoundConstants.) L'entrée n'est pas modifiée (passage par valeur du
// tableau). Déterministe.
func Permute(state [poseidonWidth]Felt) [poseidonWidth]Felt {
	s := state // copie locale (les tableaux Go se copient par valeur)

	const halfFull = poseidonFullRounds / 2 // 4
	round := 0

	// Premier bloc de rondes pleines.
	for i := 0; i < halfFull; i++ {
		addRoundConstants(&s, round)
		for k := 0; k < poseidonWidth; k++ {
			s[k] = sbox(s[k])
		}
		s = applyMDS(s)
		round++
	}

	// Rondes partielles : S-box sur la seule cellule 0.
	for i := 0; i < poseidonPartialRounds; i++ {
		addRoundConstants(&s, round)
		s[0] = sbox(s[0])
		s = applyMDS(s)
		round++
	}

	// Second bloc de rondes pleines.
	for i := 0; i < halfFull; i++ {
		addRoundConstants(&s, round)
		for k := 0; k < poseidonWidth; k++ {
			s[k] = sbox(s[k])
		}
		s = applyMDS(s)
		round++
	}

	return s
}

// ---------------------------------------------------------------------------
// Éponge (sponge) : Hash et Hash2.
// ---------------------------------------------------------------------------

// poseidonDomainSep est la constante de séparation de domaine injectée dans la
// première cellule de capacité avant absorption. Elle distingue notre usage
// « éponge à débit fixe » d'une éventuelle autre construction sur la même
// permutation, et rend l'état initial non trivial.
const poseidonDomainSep uint64 = 0x506f73656964306e // "Posid0n" en ASCII

// Hash absorbe une suite arbitraire de Felt et renvoie un digest de 4 Felt
// (≈256 bits), via une éponge à débit 8 / capacité 4.
//
// Padding documenté (style « 10* » adapté au corps) : on ajoute toujours un Felt
// séparateur = 1, puis on complète avec des Felt = 0 jusqu'à un multiple du
// débit (8). Le padding est INCONDITIONNEL (toujours au moins un Felt « 1 »
// ajouté), ce qui garantit l'injectivité : deux entrées de longueurs
// différentes ne peuvent pas produire le même flux absorbé (notamment, une
// entrée se terminant par des zéros ne collisionne pas avec une entrée plus
// courte). La longueur est par ailleurs ENCODÉE dans l'état initial (cellule de
// capacité), seconde barrière anti-collision sur la longueur.
//
// Phase d'absorption : on XOR/ajoute (ici on REMPLACE le débit, schéma
// « overwrite » déterministe) les blocs de 8 Felt dans les 8 premières cellules
// puis on permute. Phase de pressage : on renvoie les 4 premières cellules de
// l'état final.
func Hash(in []Felt) [poseidonDigestLen]Felt {
	// État initial : capacité ensemencée par la séparation de domaine et par la
	// longueur d'entrée (anti-collision de longueur). Le débit (cellules 0..7)
	// démarre à zéro.
	var state [poseidonWidth]Felt
	state[poseidonRate] = FromUint64(poseidonDomainSep)
	state[poseidonRate+1] = FromUint64(uint64(len(in)))

	// Construction du message rembourré : in || 1 || 0...0 jusqu'à un multiple
	// du débit. Le « 1 » séparateur est toujours présent.
	padded := make([]Felt, 0, len(in)+poseidonRate)
	padded = append(padded, in...)
	padded = append(padded, One()) // séparateur de padding
	for len(padded)%poseidonRate != 0 {
		padded = append(padded, Zero())
	}

	// Absorption bloc par bloc (schéma overwrite : on écrase le débit).
	for off := 0; off < len(padded); off += poseidonRate {
		for j := 0; j < poseidonRate; j++ {
			state[j] = padded[off+j]
		}
		state = Permute(state)
	}

	// Pressage : les poseidonDigestLen premières cellules forment le digest.
	var out [poseidonDigestLen]Felt
	copy(out[:], state[:poseidonDigestLen])
	return out
}

// Hash2 compresse deux digests (gauche, droite) de 4 Felt en un seul digest de
// 4 Felt. C'est la fonction de compression 2->1 utilisée pour les arbres de
// Merkle algébriques : un appel = un nœud interne.
//
// Les 8 Felt (4 de gauche + 4 de droite) remplissent exactement le débit (8),
// donc une seule permutation suffit, sans padding. La capacité est ensemencée
// par la séparation de domaine (distincte de Hash via une longueur d'entrée
// fixée à 8). L'ordre gauche/droite est SIGNIFICATIF : Hash2(a,b) != Hash2(b,a)
// en général (la couche MDS et les constantes de ronde brisent toute symétrie).
func Hash2(left, right [poseidonDigestLen]Felt) [poseidonDigestLen]Felt {
	var state [poseidonWidth]Felt
	// Débit : left (cellules 0..3) puis right (cellules 4..7).
	for j := 0; j < poseidonDigestLen; j++ {
		state[j] = left[j]
		state[poseidonDigestLen+j] = right[j]
	}
	// Capacité : même schéma d'ensemencement que Hash, longueur fixée à 8 (un
	// bloc plein), de sorte que Hash2(l,r) coïncide avec l'absorption d'un unique
	// bloc de 8 Felt par Hash si et seulement si les paddings le permettaient —
	// ils diffèrent ici car Hash ajoute toujours un séparateur, donc Hash et
	// Hash2 restent dans des domaines disjoints.
	state[poseidonRate] = FromUint64(poseidonDomainSep)
	state[poseidonRate+1] = FromUint64(uint64(poseidonRate))

	state = Permute(state)

	var out [poseidonDigestLen]Felt
	copy(out[:], state[:poseidonDigestLen])
	return out
}
