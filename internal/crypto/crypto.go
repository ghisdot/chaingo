// Package crypto centralise toute la cryptographie de ChainGO.
// Signatures : ML-DSA-65 (FIPS 204, niveau NIST 3) — résistant au quantique.
// Hachage : SHA3-256.
package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/cloudflare/circl/sign"
	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
	"golang.org/x/crypto/sha3"
)

// Scheme is the post-quantum signature scheme used for every signature
// on the chain (transactions, blocks, validators).
var Scheme sign.Scheme = mldsa65.Scheme()

const AddressPrefix = "cg"

type KeyPair struct {
	Pub  sign.PublicKey
	Priv sign.PrivateKey
	Seed []byte
}

func GenerateKeyPair() (*KeyPair, error) {
	seed := make([]byte, Scheme.SeedSize())
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	return FromSeed(seed), nil
}

// FromSeed derives a key pair deterministically from a seed
// (the seed is what wallets store, encrypted).
func FromSeed(seed []byte) *KeyPair {
	pub, priv := Scheme.DeriveKey(seed)
	return &KeyPair{Pub: pub, Priv: priv, Seed: seed}
}

func Hash(parts ...[]byte) []byte {
	h := sha3.New256()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

func HashHex(parts ...[]byte) string { return hex.EncodeToString(Hash(parts...)) }

func (kp *KeyPair) PubBytes() []byte {
	b, _ := kp.Pub.MarshalBinary()
	return b
}

func (kp *KeyPair) Address() string { return AddressFromPubBytes(kp.PubBytes()) }

func (kp *KeyPair) Sign(msg []byte) []byte { return Scheme.Sign(kp.Priv, msg, nil) }

// AddressFromPubBytes derives a chain address: "cg" + hex of the last
// 20 bytes of SHA3-256(pubkey). The full public key travels in each tx
// because it cannot be recovered from the hash.
func AddressFromPubBytes(pub []byte) string {
	h := Hash(pub)
	return AddressPrefix + hex.EncodeToString(h[12:])
}

func ValidAddress(addr string) bool {
	if !strings.HasPrefix(addr, AddressPrefix) || len(addr) != len(AddressPrefix)+40 {
		return false
	}
	_, err := hex.DecodeString(addr[len(AddressPrefix):])
	return err == nil
}

func Verify(pubBytes, msg, sig []byte) error {
	pub, err := Scheme.UnmarshalBinaryPublicKey(pubBytes)
	if err != nil {
		return errors.New("invalid public key")
	}
	if !Scheme.Verify(pub, msg, sig, nil) {
		return errors.New("invalid signature")
	}
	return nil
}
