package consensus

import (
	"bytes"
	"testing"

	"chaingo/internal/crypto"
	"chaingo/internal/types"
)

// storeWasmCons : même module que internal/state (écrit storage["k"]="v"),
// recopié ici car les fixtures de test ne traversent pas les paquets.
var storeWasmCons = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x0b, 0x02, 0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x00, 0x60, 0x00, 0x00,
	0x02, 0x15, 0x01, 0x03, 0x65, 0x6e, 0x76, 0x0d,
	0x73, 0x74, 0x6f, 0x72, 0x61, 0x67, 0x65, 0x5f, 0x77, 0x72, 0x69, 0x74, 0x65, 0x00, 0x00,
	0x03, 0x02, 0x01, 0x01,
	0x05, 0x03, 0x01, 0x00, 0x01,
	0x07, 0x07, 0x01, 0x03, 0x72, 0x75, 0x6e, 0x00, 0x01,
	0x0a, 0x0e, 0x01, 0x0c, 0x00,
	0x41, 0x00, 0x41, 0x01, 0x41, 0x01, 0x41, 0x01, 0x10, 0x00, 0x0b,
	0x0b, 0x08, 0x01, 0x00, 0x41, 0x00, 0x0b, 0x02, 0x6b, 0x76,
}

// TestMultiValidatorWasmDeployAndCall : un contrat WASM déployé puis appelé via
// le VRAI chemin de consensus (mempool → bloc produit par le proposeur → validé
// et rejoué par les autres → commit). On vérifie qu'à CHAQUE étape les 4 nœuds
// s'accordent sur la même racine d'état — c'est la preuve que l'exécution WASM
// est DÉTERMINISTE en consensus (sinon les racines divergeraient et la chaîne
// se scinderait).
func TestMultiValidatorWasmDeployAndCall(t *testing.T) {
	net := newSimNet(t, 4)

	// Active les contrats WASM sur TOUS les nœuds (params identiques = racine
	// identique) et finance un déployeur non-validateur.
	p := types.DefaultParams()
	p.WasmEnabled = true
	deployer, _ := crypto.GenerateKeyPair()
	for _, st := range net.states {
		st.SetParams(p)
		st.Mint(deployer.Address(), 1_000*types.Unit)
	}

	submit := func(tx *types.Transaction) {
		t.Helper()
		for _, pool := range net.pools {
			if _, err := pool.Add(tx); err != nil {
				t.Fatalf("mempool.Add: %v", err)
			}
		}
	}
	assertConverged := func(label string) {
		t.Helper()
		root0 := net.states[0].Root()
		for i, st := range net.states {
			if st.Root() != root0 {
				t.Fatalf("%s : racine d'état divergente au nœud %d (WASM non déterministe ?)", label, i)
			}
		}
	}

	// --- Déploiement ---
	dep := &types.Transaction{
		ChainID: "sim", Type: types.TxWasmDeploy, Code: storeWasmCons,
		MaxBaseFee: p.MinBaseFee * 4, Tip: types.SuggestedTip, Nonce: 0,
	}
	dep.SignWith(deployer)
	addr := dep.Hash()
	submit(dep)
	net.step()
	assertConverged("après déploiement")

	for i, st := range net.states {
		wc := st.GetWasmContract(addr)
		if wc == nil {
			t.Fatalf("nœud %d : contrat WASM absent après déploiement", i)
		}
		if !bytes.Equal(wc.Code, storeWasmCons) {
			t.Fatalf("nœud %d : bytecode stocké incorrect", i)
		}
	}

	// --- Appel ---
	call := &types.Transaction{
		ChainID: "sim", Type: types.TxWasmCall, ContractID: addr, Action: "run",
		MaxBaseFee: p.MinBaseFee * 4, Tip: types.SuggestedTip, Nonce: 1,
	}
	call.SignWith(deployer)
	submit(call)
	net.step()
	assertConverged("après appel")

	for i, st := range net.states {
		wc := st.GetWasmContract(addr)
		if wc == nil {
			t.Fatalf("nœud %d : contrat disparu après l'appel", i)
		}
		if got, ok := wc.Storage["k"]; !ok || !bytes.Equal(got, []byte("v")) {
			t.Fatalf("nœud %d : storage[\"k\"]=%v (ok=%v), want \"v\"", i, got, ok)
		}
		if wc.Calls != 1 {
			t.Fatalf("nœud %d : appels=%d, want 1", i, wc.Calls)
		}
	}
}
