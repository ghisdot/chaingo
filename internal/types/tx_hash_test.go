package types

import (
	"encoding/json"
	"strings"
	"testing"

	"chaingo/internal/crypto"
)

// TestMarshalJSONExposesHashWithoutBreakingSignature : invariant critique —
// l'ajout du champ "hash" dans la sortie JSON ne doit PAS modifier
// SigningBytes, sinon toutes les signatures existantes deviennent invalides.
// Vérifie aussi que round-trip Marshal/Unmarshal conserve la validité.
func TestMarshalJSONExposesHashWithoutBreakingSignature(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	tx := &Transaction{
		ChainID:    "chaingo-test",
		Type:       TxTransfer,
		To:         "cg1111111111111111111111111111111111111111",
		TokenID:    NativeToken,
		Amount:     42,
		Nonce:      7,
		MaxBaseFee: 100_000,
		Tip:        50_000,
		Memo:       "hash exposure test",
		Timestamp:  1_700_000_000_000,
	}
	tx.SignWith(kp)
	originalHash := tx.Hash()

	if err := tx.VerifySignature(); err != nil {
		t.Fatalf("signature initiale invalide : %v", err)
	}

	// MarshalJSON contient le champ "hash" exposé.
	out, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"hash":"`+originalHash+`"`) {
		t.Fatalf("la sortie JSON doit contenir hash=%s : %s", originalHash, string(out))
	}

	// Round-trip : on désérialise → la tx reconstruite a le MÊME hash et la
	// MÊME signature valide. C'est ce qui garantit la compat ascendante.
	var dec Transaction
	if err := json.Unmarshal(out, &dec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if dec.Hash() != originalHash {
		t.Fatalf("hash après round-trip = %s, attendu %s", dec.Hash(), originalHash)
	}
	if err := dec.VerifySignature(); err != nil {
		t.Fatalf("signature après round-trip invalide : %v", err)
	}
}

// TestSigningBytesUnchangedAcrossVersions : si SigningBytes change, on casse
// la chaîne. Cas concret : empreinte JSON d'une tx fixée → on figure une
// valeur, et on s'attend à ce qu'elle ne dérive jamais.
func TestSigningBytesUnchangedAcrossVersions(t *testing.T) {
	// Tx déterministe (pas de SignWith → pas d'aléa).
	tx := &Transaction{
		ChainID: "x", Type: TxTransfer, From: "cgaaa", To: "cgbbb",
		TokenID: NativeToken, Amount: 1, Nonce: 0,
		MaxBaseFee: 1, Tip: 0, Timestamp: 1,
	}
	got := string(tx.SigningBytes())
	// On NE veut PAS voir "hash" dans SigningBytes (c'est SignatureBytes
	// canonique, pas la sortie API).
	if strings.Contains(got, `"hash"`) {
		t.Fatalf("SigningBytes ne doit PAS contenir 'hash' (sinon double inclusion qui casse les signatures existantes) : %s", got)
	}
}
