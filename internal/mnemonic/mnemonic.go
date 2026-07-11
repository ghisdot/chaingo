// Package mnemonic : encodage/décodage BIP39 du seed de wallet.
//
// ChainGO stocke un seed de 32 octets (256 bits) — exactement la taille d'une
// entropie BIP39 à 24 mots. On encode ce seed en 24 mots (réversible, avec
// checksum), pour offrir une phrase de récupération lisible plutôt qu'un hex de
// 64 caractères. IMPORTANT : ce n'est PAS une dérivation PBKDF2 « mnemonic →
// seed » classique — c'est un ENCODAGE réversible du seed brut, pour que toute
// seed existante ait sa phrase et qu'une phrase reproduise EXACTEMENT le seed
// (donc la même adresse). L'ordre de la wordlist EST le format.
package mnemonic

import (
	"crypto/sha256"
	"errors"
	"strings"
)

// EntropyBits est la taille (en bits) du seed encodé : 256 (32 octets) → 24 mots.
const EntropyBits = 256

// WordCount est le nombre de mots produits pour un seed de 32 octets :
// (256 bits d'entropie + 8 bits de checksum) / 11 = 24.
const WordCount = 24

const seedBytes = EntropyBits / 8 // 32

// FromSeed encode un seed de 32 octets en une phrase mnémonique de 24 mots
// (BIP39). Erreur si le seed ne fait pas 32 octets.
func FromSeed(seed []byte) (string, error) {
	if len(seed) != seedBytes {
		return "", errors.New("mnemonic: le seed doit faire 32 octets")
	}
	// checksum = premiers (EntropyBits/32) = 8 bits de SHA-256(seed).
	sum := sha256.Sum256(seed)
	checkBits := EntropyBits / 32 // 8

	// Concatène seed (256 bits) + checksum (8 bits) = 264 bits, découpés en
	// 24 groupes de 11 bits, chacun indexant un mot.
	bits := make([]byte, 0, EntropyBits+checkBits)
	for _, b := range seed {
		for i := 7; i >= 0; i-- {
			bits = append(bits, (b>>uint(i))&1)
		}
	}
	for i := 0; i < checkBits; i++ {
		bits = append(bits, (sum[0]>>uint(7-i))&1)
	}

	words := make([]string, WordCount)
	for w := 0; w < WordCount; w++ {
		idx := 0
		for i := 0; i < 11; i++ {
			idx = idx<<1 | int(bits[w*11+i])
		}
		words[w] = wordlist[idx]
	}
	return strings.Join(words, " "), nil
}

// ToSeed décode une phrase mnémonique de 24 mots en son seed de 32 octets, en
// validant le checksum. Insensible à la casse et aux espaces superflus.
func ToSeed(mnemonic string) ([]byte, error) {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(mnemonic)))
	if len(words) != WordCount {
		return nil, errors.New("mnemonic: 24 mots attendus")
	}
	checkBits := EntropyBits / 32
	bits := make([]byte, 0, EntropyBits+checkBits)
	for _, word := range words {
		idx, ok := wordIndex[word]
		if !ok {
			return nil, errors.New("mnemonic: mot inconnu « " + word + " »")
		}
		for i := 10; i >= 0; i-- {
			bits = append(bits, byte((idx>>uint(i))&1))
		}
	}

	// Reconstruit les 32 octets d'entropie.
	seed := make([]byte, seedBytes)
	for i := 0; i < EntropyBits; i++ {
		seed[i/8] |= bits[i] << uint(7-(i%8))
	}
	// Recalcule le checksum et compare.
	sum := sha256.Sum256(seed)
	for i := 0; i < checkBits; i++ {
		if bits[EntropyBits+i] != (sum[0]>>uint(7-i))&1 {
			return nil, errors.New("mnemonic: checksum invalide (phrase erronée ou mots mal recopiés)")
		}
	}
	return seed, nil
}

// Valid indique si une phrase est un mnémonique BIP39 24 mots valide (checksum ok).
func Valid(mnemonic string) bool {
	_, err := ToSeed(mnemonic)
	return err == nil
}
