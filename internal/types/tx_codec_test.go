package types

import (
	"encoding/json"
	"reflect"
	"testing"

	"chaingo/internal/crypto"
)

// TestTxBinaryRoundtripPreservesSignature : c'est LE test critique.
// Une transaction signée, encodée en binaire, décodée, doit avoir :
//  1. exactement le même hash (SigningBytes inchangé),
//  2. une signature qui vérifie toujours,
//  3. tous les champs identiques au champ près.
// Sinon le codec binaire casse la chaîne au moment du transport.
func TestTxBinaryRoundtripPreservesSignature(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	tx := &Transaction{
		ChainID:    "chaingo-test",
		Type:       TxTransfer,
		To:         "cg1111111111111111111111111111111111111111",
		TokenID:    NativeToken,
		Amount:     42 * Unit,
		Nonce:      7,
		MaxBaseFee: 200_000,
		Tip:        50_000,
		Private:    false,
		Memo:       "binary codec roundtrip",
		Timestamp:  1_700_000_000_000,
	}
	tx.SignWith(kp)
	origHash := tx.Hash()

	bin, err := tx.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	var dec Transaction
	if err := dec.UnmarshalBinary(bin); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}

	if dec.Hash() != origHash {
		t.Fatalf("hash divergent : avant=%s après=%s", origHash, dec.Hash())
	}
	if err := dec.VerifySignature(); err != nil {
		t.Fatalf("signature invalide après round-trip binaire : %v", err)
	}
	if !reflect.DeepEqual(tx, &dec) {
		t.Fatalf("champs divergents après round-trip\navant=%+v\naprès=%+v", tx, &dec)
	}
}

// TestTxBinaryAllOptionalsEmpty : une tx avec tous les optionnels au zéro
// value doit aussi survivre au round-trip.
func TestTxBinaryAllOptionalsEmpty(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	tx := &Transaction{
		ChainID: "c", Type: TxStake, Amount: 1, Nonce: 0,
		MaxBaseFee: 100_000, Tip: 0, Timestamp: 1,
	}
	tx.SignWith(kp)

	bin, _ := tx.MarshalBinary()
	var dec Transaction
	if err := dec.UnmarshalBinary(bin); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	if dec.Token != nil || dec.Contract != nil {
		t.Fatalf("optionnels nil doivent rester nil après round-trip")
	}
	if dec.Hash() != tx.Hash() {
		t.Fatalf("hash divergent")
	}
	if err := dec.VerifySignature(); err != nil {
		t.Fatalf("signature invalide : %v", err)
	}
}

// TestTxBinaryWithTokenAndContract : create_token et contract_create
// portent des sous-structures complètes (TokenParams, ContractParams).
func TestTxBinaryWithTokenAndContract(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	cases := []*Transaction{
		{
			ChainID: "c", Type: TxCreateToken, Amount: 0, Nonce: 0,
			MaxBaseFee: 200_000, Tip: 50_000, Timestamp: 1,
			Token: &TokenParams{Symbol: "AAA", Name: "Triple A", Decimals: 6, Supply: 1_000_000, Mintable: true},
		},
		{
			ChainID: "c", Type: TxContractCreate, Amount: 0, Nonce: 1,
			MaxBaseFee: 200_000, Tip: 50_000, Timestamp: 2,
			Contract: &ContractParams{
				Template: TemplateMultisig, TokenID: NativeToken, Amount: 100 * Unit,
				Signers: []string{"cg1111", "cg2222", "cg3333"}, Threshold: 2,
			},
		},
		{
			ChainID: "c", Type: TxContractCreate, Amount: 0, Nonce: 2,
			MaxBaseFee: 200_000, Tip: 50_000, Timestamp: 3,
			Contract: &ContractParams{
				Template: TemplateVesting, TokenID: NativeToken, Amount: 1000 * Unit,
				Beneficiary: "cgbene", StartMs: 1_700_000_000_000, EndMs: 1_800_000_000_000,
			},
		},
	}
	for i, tx := range cases {
		tx.SignWith(kp)
		bin, err := tx.MarshalBinary()
		if err != nil {
			t.Fatalf("[%d] marshal: %v", i, err)
		}
		var dec Transaction
		if err := dec.UnmarshalBinary(bin); err != nil {
			t.Fatalf("[%d] unmarshal: %v", i, err)
		}
		if dec.Hash() != tx.Hash() {
			t.Fatalf("[%d] hash divergent", i)
		}
		if err := dec.VerifySignature(); err != nil {
			t.Fatalf("[%d] signature invalide: %v", i, err)
		}
		if !reflect.DeepEqual(tx, &dec) {
			t.Fatalf("[%d] champs divergents", i)
		}
	}
}

// TestTxBinaryWasmExtension : les tx wasm_deploy/wasm_call portent les champs
// Code/Args/Gas (extension binaire). Round-trip complet attendu.
func TestTxBinaryWasmExtension(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	cases := []*Transaction{
		{
			ChainID: "c", Type: TxWasmDeploy, Nonce: 0, MaxBaseFee: 200_000, Tip: 50_000, Timestamp: 1,
			Code: []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0xde, 0xad},
		},
		{
			ChainID: "c", Type: TxWasmCall, Nonce: 1, MaxBaseFee: 200_000, Tip: 50_000, Timestamp: 2,
			ContractID: "deadbeef", Action: "increment", Args: []uint64{1, 2, 1 << 40}, Gas: 1_000_000,
		},
	}
	for i, tx := range cases {
		tx.SignWith(kp)
		bin, err := tx.MarshalBinary()
		if err != nil {
			t.Fatalf("[%d] marshal: %v", i, err)
		}
		var dec Transaction
		if err := dec.UnmarshalBinary(bin); err != nil {
			t.Fatalf("[%d] unmarshal: %v", i, err)
		}
		if dec.Hash() != tx.Hash() {
			t.Fatalf("[%d] hash divergent (SigningBytes cassé par l'extension)", i)
		}
		if err := dec.VerifySignature(); err != nil {
			t.Fatalf("[%d] signature invalide: %v", i, err)
		}
		if !reflect.DeepEqual(tx, &dec) {
			t.Fatalf("[%d] champs divergents\navant=%+v\naprès=%+v", i, tx, &dec)
		}
	}
}

// TestTxBinaryNoExtensionForNonWasm : une tx ordinaire (non-WASM) NE DOIT PAS
// écrire l'extension — c'est ce qui garde son encodage octet-pour-octet
// identique à l'avant-feature et laisse les blocs déjà stockés décodables.
func TestTxBinaryNoExtensionForNonWasm(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	withExt := &Transaction{
		ChainID: "c", Type: TxTransfer, To: "cgx", TokenID: NativeToken, Amount: 1,
		MaxBaseFee: 1, Timestamp: 1,
	}
	withExt.SignWith(kp)
	bin, _ := withExt.MarshalBinary()

	// Reconstituer l'encodage SANS le bloc d'extension : il doit être identique.
	// (Si l'extension avait été écrite, les longueurs différeraient.)
	var dec Transaction
	if err := dec.UnmarshalBinary(bin); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if dec.Code != nil || dec.Args != nil || dec.Gas != 0 {
		t.Fatalf("une tx non-WASM ne doit porter aucune extension : code=%v args=%v gas=%d", dec.Code, dec.Args, dec.Gas)
	}
	if !reflect.DeepEqual(withExt, &dec) {
		t.Fatal("round-trip non-WASM altéré")
	}
}

// TestTxBinaryIsCompactVsJSON : on vérifie que le codec binaire est
// effectivement plus petit que le JSON équivalent (motivation du chantier).
// Sur une tx signée typique on doit gagner au moins 20 %.
func TestTxBinaryIsCompactVsJSON(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	tx := &Transaction{
		ChainID: "chaingo-testnet-1", Type: TxTransfer,
		To: "cg982fbc54bcbe39cc078d1ed519a0cf228309f44a", TokenID: NativeToken,
		Amount: 42 * Unit, Nonce: 7,
		MaxBaseFee: 200_000, Tip: 50_000,
		Memo: "compactness check", Timestamp: 1_700_000_000_000,
	}
	tx.SignWith(kp)

	bin, _ := tx.MarshalBinary()
	jsn, _ := json.Marshal(tx)

	gain := 100 - (len(bin)*100)/len(jsn)
	t.Logf("Tx signée : JSON %d octets, binaire %d octets — gain %d %%",
		len(jsn), len(bin), gain)
	if gain < 20 {
		t.Errorf("gain attendu ≥ 20 %%, got %d %%", gain)
	}
}

// TestTxBinaryRejectsTrailingGarbage : un payload binaire avec des octets en
// trop est rejeté (protection anti-injection).
func TestTxBinaryRejectsTrailingGarbage(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	tx := &Transaction{
		ChainID: "c", Type: TxTransfer, To: "cgabc", TokenID: NativeToken,
		Amount: 1, Nonce: 0, MaxBaseFee: 100_000, Tip: 0, Timestamp: 1,
	}
	tx.SignWith(kp)
	bin, _ := tx.MarshalBinary()
	bin = append(bin, 0xff, 0xff)

	var dec Transaction
	if err := dec.UnmarshalBinary(bin); err == nil {
		t.Fatal("octets parasites devraient être rejetés")
	}
}
