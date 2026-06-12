// Package wallet : keystore local — la seed ML-DSA est chiffrée
// AES-256-GCM avec une clé dérivée par scrypt du mot de passe.
package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/scrypt"

	"chaingo/internal/crypto"
)

type StoredKey struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Crypto  struct {
		KDF        string `json:"kdf"` // scrypt
		Salt       []byte `json:"salt"`
		N          int    `json:"n"`
		R          int    `json:"r"`
		P          int    `json:"p"`
		Cipher     string `json:"cipher"` // aes-256-gcm
		Nonce      []byte `json:"nonce"`
		CipherText []byte `json:"ciphertext"` // seed chiffrée
	} `json:"crypto"`
}

func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".chaingo", "keystore")
}

func pathFor(name string) string { return filepath.Join(Dir(), name+".json") }

func deriveKey(pass string, salt []byte, n, r, p int) ([]byte, error) {
	return scrypt.Key([]byte(pass), salt, n, r, p, 32)
}

// Create generates a new ML-DSA-65 key pair and stores the encrypted seed.
func Create(name, pass string) (*crypto.KeyPair, string, error) {
	if !validName(name) {
		return nil, "", errors.New("wallet name: letters, digits, - and _ only")
	}
	if _, err := os.Stat(pathFor(name)); err == nil {
		return nil, "", fmt.Errorf("wallet %q already exists", name)
	}
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, "", err
	}
	sk := StoredKey{Name: name, Address: kp.Address()}
	sk.Crypto.KDF = "scrypt"
	sk.Crypto.N, sk.Crypto.R, sk.Crypto.P = 1<<15, 8, 1
	sk.Crypto.Salt = make([]byte, 16)
	rand.Read(sk.Crypto.Salt)
	key, err := deriveKey(pass, sk.Crypto.Salt, sk.Crypto.N, sk.Crypto.R, sk.Crypto.P)
	if err != nil {
		return nil, "", err
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	sk.Crypto.Cipher = "aes-256-gcm"
	sk.Crypto.Nonce = make([]byte, gcm.NonceSize())
	rand.Read(sk.Crypto.Nonce)
	sk.Crypto.CipherText = gcm.Seal(nil, sk.Crypto.Nonce, kp.Seed, nil)

	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return nil, "", err
	}
	data, _ := json.MarshalIndent(sk, "", "  ")
	if err := os.WriteFile(pathFor(name), data, 0o600); err != nil {
		return nil, "", err
	}
	return kp, pathFor(name), nil
}

func Load(name, pass string) (*crypto.KeyPair, error) {
	data, err := os.ReadFile(pathFor(name))
	if err != nil {
		return nil, fmt.Errorf("wallet %q not found (chaingo wallet new %s)", name, name)
	}
	var sk StoredKey
	if err := json.Unmarshal(data, &sk); err != nil {
		return nil, err
	}
	key, err := deriveKey(pass, sk.Crypto.Salt, sk.Crypto.N, sk.Crypto.R, sk.Crypto.P)
	if err != nil {
		return nil, err
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	seed, err := gcm.Open(nil, sk.Crypto.Nonce, sk.Crypto.CipherText, nil)
	if err != nil {
		return nil, errors.New("wrong password")
	}
	return crypto.FromSeed(seed), nil
}

func List() ([]StoredKey, error) {
	entries, err := os.ReadDir(Dir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []StoredKey
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(Dir(), e.Name()))
		if err != nil {
			continue
		}
		var sk StoredKey
		if json.Unmarshal(data, &sk) == nil {
			out = append(out, sk)
		}
	}
	return out, nil
}

func validName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, c := range name {
		ok := c == '-' || c == '_' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !ok {
			return false
		}
	}
	return true
}
