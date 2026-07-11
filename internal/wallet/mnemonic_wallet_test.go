package wallet

import (
	"encoding/hex"
	"testing"

	"chaingo/internal/crypto"
	"chaingo/internal/mnemonic"
)

// Importer un wallet via sa phrase mnémonique reproduit EXACTEMENT la même
// adresse qu'un import via la seed hex — la phrase est une récupération fidèle.
func TestImportMnemonicMatchesHex(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	seedHex := hex.EncodeToString(kp.Seed)
	phrase, err := mnemonic.FromSeed(kp.Seed)
	if err != nil {
		t.Fatal(err)
	}

	viaHex, _, err := Import("via-hex", "pass1234", seedHex)
	if err != nil {
		t.Fatalf("import hex: %v", err)
	}
	viaPhrase, _, err := Import("via-phrase", "pass1234", phrase)
	if err != nil {
		t.Fatalf("import mnémonique: %v", err)
	}

	if viaHex.Address() != kp.Address() || viaPhrase.Address() != kp.Address() {
		t.Fatalf("adresses divergentes:\n orig=%s\n hex =%s\n phr =%s",
			kp.Address(), viaHex.Address(), viaPhrase.Address())
	}
}

// Une phrase au checksum invalide est refusée par l'import.
func TestImportBadMnemonicRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bad := "zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo"
	if _, _, err := Import("bad", "pass1234", bad); err == nil {
		t.Fatal("phrase au checksum invalide acceptée")
	}
}
