package types

import (
	"reflect"
	"testing"

	"chaingo/internal/crypto"
)

// TestTxBinaryShieldExtension : les tx du pool blindé (shield / shielded_transfer
// / unshield) portent les champs ShieldCommitment/ShieldNote/SpendProof/SpendPublic
// (deuxième bloc d'extension binaire, écrit APRÈS le bloc WASM). Round-trip complet.
func TestTxBinaryShieldExtension(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	cases := []*Transaction{
		{
			ChainID: "c", Type: TxShield, Amount: 100 * Unit, Nonce: 0,
			MaxBaseFee: 200_000, Tip: 50_000, Timestamp: 1,
			ShieldCommitment: []byte{1, 2, 3, 4, 5, 6, 7, 8},
			ShieldNote:       []byte("blob chiffré opaque"),
		},
		{
			ChainID: "c", Type: TxShieldedTransfer, Nonce: 1,
			MaxBaseFee: 200_000, Tip: 50_000, Timestamp: 2,
			SpendProof:  []byte{0xde, 0xad, 0xbe, 0xef},
			SpendPublic: []byte{0x01, 0x02, 0x03},
			ShieldNote:  []byte("note de sortie"),
		},
		{
			ChainID: "c", Type: TxUnshield, To: "cg982fbc54bcbe39cc078d1ed519a0cf228309f44a", Nonce: 2,
			MaxBaseFee: 200_000, Tip: 50_000, Timestamp: 3,
			SpendProof:  []byte{0xca, 0xfe},
			SpendPublic: []byte{0x09, 0x08, 0x07, 0x06},
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
			t.Fatalf("[%d] hash divergent (SigningBytes cassé par l'extension blindée)", i)
		}
		if err := dec.VerifySignature(); err != nil {
			t.Fatalf("[%d] signature invalide: %v", i, err)
		}
		if !reflect.DeepEqual(tx, &dec) {
			t.Fatalf("[%d] champs divergents\navant=%+v\naprès=%+v", i, tx, &dec)
		}
	}
}

// TestTxBinaryShieldAndWasmCoexist : une tx (artificielle) portant À LA FOIS des
// champs WASM et des champs blindés doit survivre au round-trip — les deux blocs
// d'extension s'enchaînent dans l'ordre fixe (WASM puis blindé).
func TestTxBinaryShieldAndWasmCoexist(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	tx := &Transaction{
		ChainID: "c", Type: TxShield, Amount: 1, Nonce: 0,
		MaxBaseFee: 1, Timestamp: 1,
		Code:             []byte{0x00, 0x61, 0x73, 0x6d},
		Args:             []uint64{7, 9},
		Gas:              123,
		ShieldCommitment: []byte{9, 9, 9},
		ShieldNote:       []byte("x"),
		SpendProof:       []byte{0xaa},
		SpendPublic:      []byte{0xbb},
	}
	tx.SignWith(kp)
	bin, _ := tx.MarshalBinary()
	var dec Transaction
	if err := dec.UnmarshalBinary(bin); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(tx, &dec) {
		t.Fatalf("round-trip WASM+blindé altéré\navant=%+v\naprès=%+v", tx, &dec)
	}
}

// TestTxBinaryWasmPureUnaffectedByShield : une tx WASM PURE (aucun champ blindé)
// ne doit écrire AUCUN bloc d'extension blindé — donc ses champs blindés restent
// nil après round-trip. C'est ce qui garantit que son encodage reste octet-pour-
// octet identique à AVANT l'ajout du pool blindé (compat ascendante des blocs).
func TestTxBinaryWasmPureUnaffectedByShield(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	tx := &Transaction{
		ChainID: "c", Type: TxWasmCall, Nonce: 0, MaxBaseFee: 1, Timestamp: 1,
		ContractID: "deadbeef", Action: "run", Args: []uint64{1, 2, 3}, Gas: 1_000,
	}
	tx.SignWith(kp)
	bin, _ := tx.MarshalBinary()
	var dec Transaction
	if err := dec.UnmarshalBinary(bin); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if dec.ShieldCommitment != nil || dec.ShieldNote != nil || dec.SpendProof != nil || dec.SpendPublic != nil {
		t.Fatalf("une tx WASM pure ne doit porter aucun champ blindé après round-trip : %+v", dec)
	}
	if !reflect.DeepEqual(tx, &dec) {
		t.Fatal("round-trip WASM pur altéré par le pool blindé")
	}
}

// TestShieldValidateBasic : ValidateBasic exige les champs requis de chaque type
// blindé (et ne vérifie PAS la preuve — c'est à l'exécution).
func TestShieldValidateBasic(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	good := []*Transaction{
		{ChainID: "c", Type: TxShield, Amount: 1, MaxBaseFee: 1, ShieldCommitment: []byte{1}, ShieldNote: []byte{2}},
		{ChainID: "c", Type: TxShieldedTransfer, MaxBaseFee: 1, SpendProof: []byte{1}, SpendPublic: []byte{2}, ShieldNote: []byte{3}},
		{ChainID: "c", Type: TxUnshield, To: "cg982fbc54bcbe39cc078d1ed519a0cf228309f44a", MaxBaseFee: 1, SpendProof: []byte{1}, SpendPublic: []byte{2}},
	}
	for i, tx := range good {
		tx.SignWith(kp)
		if err := tx.ValidateBasic(); err != nil {
			t.Fatalf("[%d] tx blindée valide refusée: %v", i, err)
		}
	}

	bad := []*Transaction{
		{ChainID: "c", Type: TxShield, Amount: 0, MaxBaseFee: 1, ShieldCommitment: []byte{1}, ShieldNote: []byte{2}}, // amount 0
		{ChainID: "c", Type: TxShield, Amount: 1, MaxBaseFee: 1, ShieldNote: []byte{2}},                              // pas de commitment
		{ChainID: "c", Type: TxShieldedTransfer, MaxBaseFee: 1, SpendPublic: []byte{2}, ShieldNote: []byte{3}},        // pas de preuve
		{ChainID: "c", Type: TxUnshield, MaxBaseFee: 1, SpendProof: []byte{1}, SpendPublic: []byte{2}},               // pas de To
	}
	for i, tx := range bad {
		tx.SignWith(kp)
		if err := tx.ValidateBasic(); err == nil {
			t.Fatalf("[%d] tx blindée invalide acceptée", i)
		}
	}
}
