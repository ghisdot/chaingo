package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"

	"github.com/cloudflare/circl/kem"
	"github.com/cloudflare/circl/kem/mlkem/mlkem768"
	"golang.org/x/crypto/sha3"
)

// view.go — CONFIDENTIALITÉ (Phase 3, EXPÉRIMENTAL, hors-consensus).
//
// Le chiffrement des notes utilise ML-KEM-768 (FIPS 203), le mécanisme
// d'encapsulation de clé post-quantique standardisé (≠ ML-DSA, qui ne sert qu'à
// SIGNER). C'est la brique « livraison chiffrée d'une note à un destinataire »
// d'un pool blindé — voir docs/design/phase3-privacy.md.
//
// ⚠️ GARANTIE : CONFIDENTIALITÉ du contenu (seul le détenteur de la clé de vue
// privée déchiffre). L'ANONYMAT (non-liaison d'une note à une clé de vue connue)
// dépend de la key-privacy de ML-KEM — propriété distincte, À AUDITER. Ne pas
// confondre les deux. Rien ici n'est câblé en consensus.

// ViewScheme : KEM post-quantique pour la couche de confidentialité.
var ViewScheme kem.Scheme = mlkem768.Scheme()

// ViewKeyPair : clé de visualisation/chiffrement, dérivée du MÊME seed que la
// clé de signature du wallet → un seul seed à sauvegarder pour tout.
type ViewKeyPair struct {
	Pub  kem.PublicKey
	Priv kem.PrivateKey
}

// DeriveViewKey dérive une paire ML-KEM déterministe depuis le seed du wallet.
// Le seed ML-DSA (32 o) est étendu à la taille de seed du KEM via SHAKE256 avec
// séparation de domaine (la clé de vue ne doit jamais coïncider avec la clé de
// signature ni quoi que ce soit d'autre).
func DeriveViewKey(seed []byte) *ViewKeyPair {
	ks := make([]byte, ViewScheme.SeedSize())
	sha3.ShakeSum256(ks, append([]byte("chaingo-view-key-v1\x00"), seed...))
	pub, priv := ViewScheme.DeriveKeyPair(ks)
	return &ViewKeyPair{Pub: pub, Priv: priv}
}

// ViewPubBytes : encodage binaire de la clé de vue publique (publiable on-chain).
func (v *ViewKeyPair) ViewPubBytes() []byte { b, _ := v.Pub.MarshalBinary(); return b }

// SealTo chiffre `plaintext` vers la clé de vue publique `viewPub` :
// encapsulation ML-KEM → secret partagé → AES-256-GCM. Sortie = ct‖nonce‖chiffré.
// Le ciphertext KEM sert d'AAD (lie le chiffré à son encapsulation).
func SealTo(viewPub, plaintext []byte) ([]byte, error) {
	pk, err := ViewScheme.UnmarshalBinaryPublicKey(viewPub)
	if err != nil {
		return nil, fmt.Errorf("clé de vue invalide: %w", err)
	}
	ct, ss, err := ViewScheme.Encapsulate(pk)
	if err != nil {
		return nil, err
	}
	gcm, err := noteAEAD(ss)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(ct)+len(nonce)+len(plaintext)+gcm.Overhead())
	out = append(out, ct...)
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, plaintext, ct), nil
}

// OpenWith tente de déchiffrer `blob` avec la clé de vue privée. ok=false si le
// blob n'est PAS destiné à cette clé (échec d'authentification GCM) : c'est le
// mécanisme de SCAN — un wallet essaie d'ouvrir chaque note publiée, ok=true
// signifie « cette note est pour moi ». (ML-KEM rejette implicitement : un
// mauvais ct produit un secret pseudo-aléatoire → GCM échoue → ok=false.)
func (v *ViewKeyPair) OpenWith(blob []byte) (plaintext []byte, ok bool) {
	ctSize := ViewScheme.CiphertextSize()
	if len(blob) < ctSize {
		return nil, false
	}
	ct := blob[:ctSize]
	ss, err := ViewScheme.Decapsulate(v.Priv, ct)
	if err != nil {
		return nil, false
	}
	gcm, err := noteAEAD(ss)
	if err != nil {
		return nil, false
	}
	ns := gcm.NonceSize()
	if len(blob) < ctSize+ns {
		return nil, false
	}
	nonce := blob[ctSize : ctSize+ns]
	pt, err := gcm.Open(nil, nonce, blob[ctSize+ns:], ct)
	if err != nil {
		return nil, false
	}
	return pt, true
}

func noteAEAD(ss []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(Hash(ss, []byte("chaingo-note-enc"))) // clé AES-256 dérivée du secret KEM
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
