// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Transcript Fiat-Shamir : canal aléatoire déterministe pour rendre notre
// protocole interactif non interactif. Le prouveur et le vérifieur rejouent
// EXACTEMENT les mêmes absorptions dans le même ordre ; les défis (challenges)
// en sont dérivés de façon reproductible bit-à-bit.
//
// Construction : éponge SHAKE256 (golang.org/x/crypto/sha3). On entretient un
// état d'absorption « vivant » (le sponge dans lequel on écrit). Un défi est
// produit en CLONANT cet état, en figeant le clone (squeeze) et en repliant le
// défi obtenu dans l'état vivant. On ne lit (Read) JAMAIS le sponge vivant —
// uniquement ses clones — car l'implémentation SHAKE panique si l'on écrit
// après avoir lu. Cette discipline « clone-pour-squeezer » donne un duplex
// propre : chaque défi dépend de tout l'historique et fait avancer l'état.
//
// Cadre (framing) déterministe : chaque enregistrement absorbé est précédé de
// son étiquette (label) et de longueurs encodées sur 8 octets big-endian, afin
// qu'aucune concaténation ambiguë ne puisse produire deux transcripts égaux à
// partir d'historiques différents (résistance à l'extension/ambiguïté).
//
// Aucune dépendance à time / math/rand : tout le « hasard » provient de SHAKE.
package stark

import (
	"encoding/binary"

	"golang.org/x/crypto/sha3"
)

// Étiquettes de domaine internes : préfixes d'octets distincts garantissant
// qu'une absorption, un défi-felt et un défi-indices ne puissent jamais se
// confondre même à étiquette utilisateur identique (séparation de domaine).
var (
	domAbsorb  = []byte("stark/fs/absorb\x00")
	domFeltChl = []byte("stark/fs/challenge-felt\x00")
	domIdxChl  = []byte("stark/fs/challenge-indices\x00")
)

// Transcript est un canal Fiat-Shamir déterministe. Le zéro-valeur n'est PAS
// utilisable : passer par NewTranscript.
type Transcript struct {
	// sponge est l'état d'absorption vivant. On y écrit (Absorb*) et on le
	// clone pour produire les défis. On ne le lit jamais directement.
	sponge sha3.ShakeHash
}

// NewTranscript crée un transcript initialisé avec une étiquette de domaine
// (séparation de protocole). Deux protocoles avec des étiquettes différentes
// produiront des défis indépendants même pour des absorptions identiques.
func NewTranscript(domain string) *Transcript {
	t := &Transcript{sponge: sha3.NewShake256()}
	// Amorçage du domaine : on absorbe l'étiquette de protocole en tête. On
	// passe par writeFramed pour le cadrage longueur-préfixée.
	t.writeFramed([]byte("stark/fs/domain\x00"), []byte(domain))
	return t
}

// writeFramed écrit dans le sponge vivant un enregistrement cadré :
//
//	len(prefix) || prefix || len(data) || data
//
// avec les longueurs sur 8 octets big-endian. Le cadrage rend l'absorption
// injective : impossible de fabriquer deux suites d'octets distinctes donnant
// le même flux absorbé.
func (t *Transcript) writeFramed(prefix, data []byte) {
	var lenBuf [8]byte

	binary.BigEndian.PutUint64(lenBuf[:], uint64(len(prefix)))
	// hash.Hash.Write ne renvoie jamais d'erreur (contrat documenté) ; on
	// ignore donc explicitement le retour.
	_, _ = t.sponge.Write(lenBuf[:])
	_, _ = t.sponge.Write(prefix)

	binary.BigEndian.PutUint64(lenBuf[:], uint64(len(data)))
	_, _ = t.sponge.Write(lenBuf[:])
	_, _ = t.sponge.Write(data)
}

// labelWithDomain assemble une étiquette utilisateur avec un préfixe de domaine
// interne : domaine || 0x00 || label. Le séparateur 0x00 évite toute collision
// entre (domaine="a", label="bc") et (domaine="ab", label="c").
func labelWithDomain(domain []byte, label string) []byte {
	out := make([]byte, 0, len(domain)+1+len(label))
	out = append(out, domain...)
	out = append(out, 0x00)
	out = append(out, label...)
	return out
}

// Absorb intègre des données arbitraires au transcript sous une étiquette. Le
// même couple (label, data) absorbé dans le même ordre produit le même état.
func (t *Transcript) Absorb(label string, data []byte) {
	t.writeFramed(labelWithDomain(domAbsorb, label), data)
}

// AbsorbFelt intègre un élément du corps (8 octets big-endian, représentation
// canonique). Commodité typée par-dessus Absorb.
func (t *Transcript) AbsorbFelt(label string, x Felt) {
	t.writeFramed(labelWithDomain(domAbsorb, label), x.Bytes())
}

// squeeze produit `n` octets dérivés de l'état courant SOUS une étiquette de
// défi donnée, PUIS replie ces octets dans l'état vivant pour le faire avancer
// (deux défis successifs sans absorption intercalaire diffèrent).
//
// On clone le sponge vivant, on absorbe dans le clone l'étiquette de défi (avec
// sa longueur de sortie) afin de séparer les domaines de squeeze, puis on lit n
// octets du clone. Le sponge vivant, lui, n'est jamais lu : on ne fait qu'y
// réabsorber l'étiquette et le résultat.
func (t *Transcript) squeeze(challengeDomain []byte, label string, n int) []byte {
	prefix := labelWithDomain(challengeDomain, label)

	// 1) Clone de l'état vivant pour figer un instantané reproductible.
	clone := t.sponge.Clone()

	// 2) Cadrage de la requête de défi dans le clone : préfixe + nombre
	//    d'octets demandés. Ainsi un défi de 8 octets et un défi de 16 octets
	//    sous la même étiquette ne partagent pas de préfixe de flux.
	var hdr [8]byte
	binary.BigEndian.PutUint64(hdr[:], uint64(len(prefix)))
	_, _ = clone.Write(hdr[:])
	_, _ = clone.Write(prefix)
	binary.BigEndian.PutUint64(hdr[:], uint64(n))
	_, _ = clone.Write(hdr[:])

	// 3) Squeeze des n octets depuis le clone (jamais depuis le sponge vivant).
	out := make([]byte, n)
	_, _ = clone.Read(out)

	// 4) On fait avancer l'état vivant en y absorbant l'étiquette de défi puis
	//    la sortie produite. Le prochain défi dépendra donc de celui-ci.
	t.writeFramed(prefix, out)

	return out
}

// Challenge renvoie un défi uniformément distribué dans le corps [0, P).
//
// Méthode du rejet de biais (rejection sampling) : SHAKE produit des blocs de
// 8 octets interprétés en big-endian comme un uint64 ; on accepte le premier
// bloc strictement inférieur à floor(2^64 / P) * P. Comme 2^64 n'est pas un
// multiple de P, prendre x mod P sur tout l'intervalle [0, 2^64) introduirait
// un biais en faveur des petites valeurs ; le rejet l'élimine exactement.
//
// La borne d'acceptation maxAccept est le plus grand multiple de P qui tient
// sur 64 bits. La probabilité de rejet est (2^64 mod P)/2^64 ≈ 2^-32 :
// quasiment toujours un seul bloc, et toujours un nombre déterministe de blocs
// puisque les octets sont déterministes.
func (t *Transcript) Challenge(label string) Felt {
	// Borne d'acceptation du rejet de biais : le plus grand multiple de P qui
	// tient sur 64 bits. Comme P = 2^64 - 2^32 + 1 > 2^63, le SEUL multiple de
	// P dans [0, 2^64) est P lui-même (q = floor(2^64/P) = 1). On a en effet
	// 2^64 - epsilon = 2^64 - (2^32 - 1) = 2^64 - 2^32 + 1 = P. Donc accepter
	// x ssi x < P élimine exactement le biais de x mod P sur [0, 2^64).
	const maxAccept = P

	// On dérive un flux d'octets dédié à ce défi-felt, puis on consomme des
	// blocs de 8 octets jusqu'à acceptation. Pour rester déterministe et borné,
	// on demande un gros lot d'octets en une fois (16 blocs = 128 octets), ce
	// qui couvre le rejet avec une marge astronomique ; si jamais épuisé, on
	// re-dérive avec un compteur.
	var counter uint64
	for {
		// Étiquette enrichie d'un compteur pour re-dériver si (très) rares
		// rejets épuisent le lot — garde le déterminisme.
		buf := t.squeezeForFelt(label, counter)
		for off := 0; off+8 <= len(buf); off += 8 {
			x := binary.BigEndian.Uint64(buf[off : off+8])
			if x < maxAccept {
				return FromUint64(x % P)
			}
		}
		counter++
	}
}

// squeezeForFelt produit un lot d'octets pour Challenge, indexé par un compteur
// de re-dérivation. Le premier appel (counter==0) fait aussi avancer l'état
// vivant ; les re-dérivations (counter>0, extrêmement rares) ne le refont pas
// avancer une seconde fois afin de garder une sémantique d'« un défi = un pas
// d'état », tout en restant déterministes.
func (t *Transcript) squeezeForFelt(label string, counter uint64) []byte {
	const lot = 128 // 16 blocs de 8 octets
	if counter == 0 {
		return t.squeeze(domFeltChl, label, lot)
	}
	// Re-dérivation déterministe sans muter l'état vivant : on clone, on cadre
	// l'étiquette + le compteur, on squeeze. Branche quasi jamais atteinte
	// (probabilité ≈ 2^-32 par lot de 16 blocs => négligeable), présente pour
	// la complétude formelle du rejet de biais.
	clone := t.sponge.Clone()
	prefix := labelWithDomain(domFeltChl, label)
	var hdr [8]byte
	binary.BigEndian.PutUint64(hdr[:], uint64(len(prefix)))
	_, _ = clone.Write(hdr[:])
	_, _ = clone.Write(prefix)
	binary.BigEndian.PutUint64(hdr[:], counter)
	_, _ = clone.Write(hdr[:])
	out := make([]byte, lot)
	_, _ = clone.Read(out)
	return out
}

// ChallengeIndices renvoie `count` indices dans [0, max), dérivés du transcript.
// Utilisé pour le tirage des positions d'interrogation (query phase) de FRI.
//
// Les indices sont produits par rejet de biais sur la puissance de 2 immédiate
// >= max : on lit suffisamment d'octets, on masque sur le nombre de bits utile,
// et on rejette les valeurs >= max. Cela donne une distribution exactement
// uniforme sur [0, max). Les indices PEUVENT se répéter (échantillonnage avec
// remise) : c'est le comportement attendu pour des requêtes indépendantes.
//
// Panique si max <= 0 ou count < 0 (erreur de programmation côté appelant).
// count == 0 renvoie un slice vide non nil.
func (t *Transcript) ChallengeIndices(label string, count, max int) []int {
	if max <= 0 {
		panic("stark: ChallengeIndices: max doit être strictement positif")
	}
	if count < 0 {
		panic("stark: ChallengeIndices: count doit être >= 0")
	}
	out := make([]int, 0, count)
	if count == 0 {
		return out
	}

	// Nombre de bits nécessaires pour représenter max-1, et masque associé.
	bitsNeeded := 0
	for (uint64(1) << bitsNeeded) < uint64(max) {
		bitsNeeded++
	}
	// max==1 => bitsNeeded==0 => tous les indices valent 0 ; on lit quand même
	// des octets pour faire avancer l'état de façon cohérente.
	var mask uint64
	if bitsNeeded > 0 {
		mask = (uint64(1) << bitsNeeded) - 1
	}

	// On dérive un flux dédié et on le re-dérive par lots indexés tant qu'on
	// n'a pas collecté assez d'indices acceptés (rejet de biais).
	var counter uint64
	buf := t.squeeze(domIdxChl, label, indicesLotSize(count))
	off := 0
	for len(out) < count {
		if off+8 > len(buf) {
			// Lot épuisé : re-dérivation déterministe via un compteur, sans
			// re-muter l'état vivant (déjà muté au premier squeeze).
			counter++
			buf = t.reSqueezeIndices(label, counter, indicesLotSize(count))
			off = 0
		}
		x := binary.BigEndian.Uint64(buf[off:off+8]) & mask
		off += 8
		if x < uint64(max) {
			out = append(out, int(x))
		}
	}
	return out
}

// indicesLotSize estime un lot d'octets « généreux » pour `count` indices :
// 8 octets par indice, multiplié par 2 pour absorber les rejets, minimum 64.
// Déterministe (aucune dépendance à l'aléa) : même count => même taille.
func indicesLotSize(count int) int {
	n := count * 8 * 2
	if n < 64 {
		n = 64
	}
	return n
}

// reSqueezeIndices re-dérive un lot d'octets pour ChallengeIndices sans faire
// avancer l'état vivant (déjà avancé au premier squeeze de la phase). Branche
// rare, présente pour garantir la terminaison du rejet de biais. Déterministe.
func (t *Transcript) reSqueezeIndices(label string, counter uint64, n int) []byte {
	clone := t.sponge.Clone()
	prefix := labelWithDomain(domIdxChl, label)
	var hdr [8]byte
	binary.BigEndian.PutUint64(hdr[:], uint64(len(prefix)))
	_, _ = clone.Write(hdr[:])
	_, _ = clone.Write(prefix)
	binary.BigEndian.PutUint64(hdr[:], counter)
	_, _ = clone.Write(hdr[:])
	out := make([]byte, n)
	_, _ = clone.Read(out)
	return out
}

// Clone renvoie une copie profonde et indépendante du transcript dans son état
// courant. Pratique pour explorer plusieurs branches de défis sans rejouer
// toutes les absorptions. La copie et l'original divergeront dès la première
// opération mutante (Absorb*/Challenge*) appliquée à l'un d'eux.
func (t *Transcript) Clone() *Transcript {
	return &Transcript{sponge: t.sponge.Clone()}
}
