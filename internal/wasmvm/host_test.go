package wasmvm

import (
	"bytes"
	"context"
	"testing"
)

// valueWasm : module qui IMPORTE env.value() -> i64 et exporte run() -> i64
// renvoyant value(). Encodé à la main — prouve qu'un contrat instrumenté peut
// APPELER une fonction hôte (le pont WASM ↔ Sandbox).
var valueWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, // \0asm v1
	0x01, 0x05, 0x01, 0x60, 0x00, 0x01, 0x7e, // type: () -> i64
	0x02, 0x0d, 0x01, 0x03, 0x65, 0x6e, 0x76, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x00, 0x00, // import env.value (func type 0)
	0x03, 0x02, 0x01, 0x00, // function section : 1 func type 0 (= run, index 1)
	0x07, 0x07, 0x01, 0x03, 0x72, 0x75, 0x6e, 0x00, 0x01, // export "run" -> func 1
	0x0a, 0x06, 0x01, 0x04, 0x00, 0x10, 0x00, 0x0b, // code: call 0 (value) ; end
}

// TestSandboxHostValueWiring : un contrat appelle env.value() et le sandbox
// lui répond — le pont hôte fonctionne, même après instrumentation du gas.
func TestSandboxHostValueWiring(t *testing.T) {
	sb := NewSandbox("cgcaller", 42)
	res, err := sb.Run(context.Background(), valueWasm, "run", 1_000_000)
	if err != nil {
		t.Fatalf("sandbox run: %v", err)
	}
	if len(res.Returns) != 1 || res.Returns[0] != 42 {
		t.Fatalf("run() = %v, want [42] (= value())", res.Returns)
	}
	if res.GasUsed <= 0 {
		t.Fatalf("gas consommé attendu > 0, got %d", res.GasUsed)
	}
}

// TestSandboxStorage : la logique de stockage clé→valeur (lecture/écriture,
// persistance, absence) — testée directement (un contrat Rust l'exercera via
// l'ABI mémoire, cf examples/contracts/counter).
func TestSandboxStorage(t *testing.T) {
	sb := NewSandbox("cgalice", 0)
	if _, ok := sb.storageRead([]byte("count")); ok {
		t.Fatal("clé absente doit renvoyer ok=false")
	}
	sb.storageWrite([]byte("count"), []byte{7})
	v, ok := sb.storageRead([]byte("count"))
	if !ok || !bytes.Equal(v, []byte{7}) {
		t.Fatalf("lecture après écriture : ok=%v v=%v", ok, v)
	}
	// écrasement
	sb.storageWrite([]byte("count"), []byte{8, 9})
	v, _ = sb.storageRead([]byte("count"))
	if !bytes.Equal(v, []byte{8, 9}) {
		t.Fatalf("écrasement raté : %v", v)
	}
}

// TestSandboxTransfer : un transfert débite le solde et est enregistré ;
// au-delà du solde, il échoue.
func TestSandboxTransfer(t *testing.T) {
	sb := NewSandbox("cgalice", 0)
	sb.Balance = 100
	if !sb.transfer("cgbob", 60) {
		t.Fatal("transfert dans le solde doit réussir")
	}
	if sb.Balance != 40 {
		t.Fatalf("solde après transfert = %d, want 40", sb.Balance)
	}
	if sb.transfer("cgbob", 50) {
		t.Fatal("transfert au-delà du solde doit échouer")
	}
	if len(sb.Transfers) != 1 || sb.Transfers[0].To != "cgbob" || sb.Transfers[0].Amount != 60 {
		t.Fatalf("transferts enregistrés incorrects : %+v", sb.Transfers)
	}
}
