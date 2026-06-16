package state

import (
	"bytes"
	"testing"

	"chaingo/internal/types"
)

// storeWasm : module WASM encodé à la main qui importe env.storage_write et,
// dans run(), écrit storage["k"] = "v" (clé à l'offset 0, valeur à l'offset 1,
// posées par un segment de données "kv"). Sert à prouver que le COMMIT du
// stockage d'un contrat sur la VRAIE machine d'état fonctionne (wasm_call).
var storeWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, // \0asm v1
	// type : (0) storage_write(i32,i32,i32,i32)->() ; (1) run()->()
	0x01, 0x0b, 0x02, 0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x00, 0x60, 0x00, 0x00,
	// import env.storage_write (type 0)
	0x02, 0x15, 0x01, 0x03, 0x65, 0x6e, 0x76, 0x0d,
	0x73, 0x74, 0x6f, 0x72, 0x61, 0x67, 0x65, 0x5f, 0x77, 0x72, 0x69, 0x74, 0x65, 0x00, 0x00,
	0x03, 0x02, 0x01, 0x01, // function : 1 func de type 1 (run = index 1)
	0x05, 0x03, 0x01, 0x00, 0x01, // memory : 1 page
	0x07, 0x07, 0x01, 0x03, 0x72, 0x75, 0x6e, 0x00, 0x01, // export "run" -> func 1
	// code de run : storage_write(0,1, 1,1) ; end
	0x0a, 0x0e, 0x01, 0x0c, 0x00,
	0x41, 0x00, 0x41, 0x01, 0x41, 0x01, 0x41, 0x01, 0x10, 0x00, 0x0b,
	// data : "kv" à l'offset 0
	0x0b, 0x08, 0x01, 0x00, 0x41, 0x00, 0x0b, 0x02, 0x6b, 0x76,
}

func wasmParams() types.Params {
	p := testParams()
	p.WasmEnabled = true
	p.WasmDeployFee = 5
	p.WasmCallFee = 1
	p.WasmMaxCodeLen = 256 * 1024
	p.WasmGasLimit = 10_000_000
	return p
}

// TestWasmDeployStoresValidatedCode : un wasm_deploy valide stocke le bytecode
// à l'adresse = hash de la tx, débite les frais (brûlés), et crée le contrat.
func TestWasmDeployStoresValidatedCode(t *testing.T) {
	st := New()
	st.SetParams(wasmParams())
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000)

	dep := &types.Transaction{Type: types.TxWasmDeploy, Code: storeWasm, MaxBaseFee: 1}
	dep.SignWith(alice)
	burnedBefore := st.GetSupply().Burned
	executeStateBlock(t, st, "", 1_000, dep)

	addr := dep.Hash()
	wc := st.GetWasmContract(addr)
	if wc == nil {
		t.Fatalf("contrat WASM absent à l'adresse %s", addr)
	}
	if !bytes.Equal(wc.Code, storeWasm) {
		t.Fatal("bytecode stocké != bytecode déployé")
	}
	if wc.Creator != alice.Address() {
		t.Fatalf("créateur = %s, want %s", wc.Creator, alice.Address())
	}
	if got := st.GetSupply().Burned - burnedBefore; got != 5 {
		t.Fatalf("frais de déploiement brûlés = %d, want 5", got)
	}
}

// TestWasmCallCommitsStorage : appeler run() écrit storage["k"]="v" et ce
// changement est COMMIT sur l'état réel (le cœur du câblage consensus-grade).
func TestWasmCallCommitsStorage(t *testing.T) {
	st := New()
	st.SetParams(wasmParams())
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000)

	dep := &types.Transaction{Type: types.TxWasmDeploy, Code: storeWasm, MaxBaseFee: 1}
	dep.SignWith(alice)
	executeStateBlock(t, st, "", 1_000, dep)
	addr := dep.Hash()

	call := &types.Transaction{Type: types.TxWasmCall, ContractID: addr, Action: "run", Nonce: 1, MaxBaseFee: 1}
	call.SignWith(alice)
	executeStateBlock(t, st, "", 2_000, call)

	wc := st.GetWasmContract(addr)
	if wc == nil {
		t.Fatal("contrat disparu après l'appel")
	}
	if got, ok := wc.Storage["k"]; !ok || !bytes.Equal(got, []byte("v")) {
		t.Fatalf("storage[\"k\"] = %v (ok=%v), want \"v\" — commit du stockage raté", got, ok)
	}
	if wc.Calls != 1 {
		t.Fatalf("compteur d'appels = %d, want 1", wc.Calls)
	}
}

// TestWasmDisabledRejectsTxs : avec WasmEnabled=false (posture mainnet), les tx
// wasm_deploy/wasm_call sont refusées — le verrou de sûreté tient.
func TestWasmDisabledRejectsTxs(t *testing.T) {
	st := New()
	p := wasmParams()
	p.WasmEnabled = false
	st.SetParams(p)
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000)

	dep := &types.Transaction{Type: types.TxWasmDeploy, Code: storeWasm, MaxBaseFee: 1}
	dep.SignWith(alice)
	if _, _, _, err := st.Execute([]*types.Transaction{dep}, nil, nil, "", 1_000, true); err == nil {
		t.Fatal("wasm_deploy devrait être refusé quand WasmEnabled=false")
	}
}

// TestWasmDeployRejectsGarbage : un bytecode non instrumentable (donc dont on ne
// garantit pas l'arrêt) est refusé au déploiement.
func TestWasmDeployRejectsGarbage(t *testing.T) {
	st := New()
	st.SetParams(wasmParams())
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000)

	dep := &types.Transaction{Type: types.TxWasmDeploy, Code: []byte("pas du wasm"), MaxBaseFee: 1}
	dep.SignWith(alice)
	if _, _, _, err := st.Execute([]*types.Transaction{dep}, nil, nil, "", 1_000, true); err == nil {
		t.Fatal("un bytecode invalide devrait être refusé au déploiement")
	}
}
