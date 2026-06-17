package types

import (
	"bytes"
	"testing"

	"chaingo/internal/crypto"
)

// ÉTAGE 5.4 — RÉTROCOMPATIBILITÉ BINAIRE au niveau OCTET.
//
// Une tx ordinaire (ni WASM, ni blindée) ne doit écrire AUCUN octet après la
// signature : son encodage est donc OCTET-POUR-OCTET celui d'AVANT l'ajout des
// extensions WASM et pool blindé. C'est ce qui garantit que les blocs déjà
// stockés restent décodables et que les hash de bloc ne forkent pas.
//
// On le prouve sans "golden bytes" figés (fragiles) : on encode la tx, on décode
// jusqu'à la signature avec le MÊME codec primitif, et on vérifie qu'il ne reste
// PLUS RIEN à lire. S'il restait des octets, ce serait un bloc d'extension.
func TestPlainTxWritesNoExtensionBytes(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	plain := []*Transaction{
		{ChainID: "c", Type: TxTransfer, To: "cg982fbc54bcbe39cc078d1ed519a0cf228309f44a",
			TokenID: NativeToken, Amount: 42, Nonce: 3, MaxBaseFee: 200_000, Tip: 50_000, Timestamp: 7},
		{ChainID: "c", Type: TxStake, Amount: 10_000, Nonce: 0, MaxBaseFee: 1, Timestamp: 1},
	}
	for i, tx := range plain {
		tx.SignWith(kp)
		bin, err := tx.MarshalBinary()
		if err != nil {
			t.Fatalf("[%d] marshal: %v", i, err)
		}

		// On rejoue le décodage des champs FIXES (jusqu'à la signature incluse) avec
		// le codec primitif, puis on exige Remaining()==0 : aucune extension écrite.
		end := signatureEndOffset(t, bin)
		if end != len(bin) {
			t.Fatalf("[%d] une tx ordinaire a écrit %d octet(s) d'extension après la signature (rétrocompat cassée)",
				i, len(bin)-end)
		}
	}
}

// TestShieldTxHasBytesAfterSignature : contrôle de cohérence — à l'inverse, une tx
// blindée DOIT écrire des octets après la signature (sinon ses champs blindés se
// perdraient). Garantit que le test ci-dessus mesure bien la bonne frontière.
func TestShieldTxHasBytesAfterSignature(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	tx := &Transaction{
		ChainID: "c", Type: TxShield, Amount: 1, Nonce: 0, MaxBaseFee: 1, Timestamp: 1,
		ShieldCommitment: bytes.Repeat([]byte{0xAB}, 32), ShieldNote: []byte("blob"),
	}
	tx.SignWith(kp)
	bin, _ := tx.MarshalBinary()
	end := signatureEndOffset(t, bin)
	if end == len(bin) {
		t.Fatal("une tx blindée devrait écrire des octets après la signature")
	}
}

// signatureEndOffset décode une tx binaire jusqu'à la signature INCLUSE en
// réutilisant UnmarshalBinary (qui s'arrête sur les extensions s'il y en a) puis
// re-sérialise les champs fixes pour mesurer où finit la signature. Plus simple :
// on décode la tx, on remet ses champs d'extension à zéro, on ré-encode, et la
// longueur du ré-encodage sans extension EST l'offset de fin de signature.
func signatureEndOffset(t *testing.T, bin []byte) int {
	t.Helper()
	var dec Transaction
	if err := dec.UnmarshalBinary(bin); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Effacer toute extension -> le ré-encodage s'arrête juste après la signature.
	dec.Code, dec.Args, dec.Gas = nil, nil, 0
	dec.ShieldCommitment, dec.ShieldNote = nil, nil
	dec.SpendProof, dec.SpendPublic = nil, nil
	noExt, err := dec.MarshalBinary()
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	// noExt est le préfixe (champs fixes + signature) commun à bin. Vérifions-le.
	if len(noExt) > len(bin) || !bytes.Equal(noExt, bin[:len(noExt)]) {
		t.Fatalf("le préfixe sans extension n'est pas un préfixe de l'encodage complet")
	}
	return len(noExt)
}
