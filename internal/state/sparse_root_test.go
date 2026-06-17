package state

import (
	"testing"

	"chaingo/internal/types"
)

// populatedState construit un état avec TOUS les composants peuplés.
func populatedState(sparse bool) *State {
	st := New()
	p := testParams()
	p.SparseMerkleRoot = sparse
	st.SetParams(p)
	st.Accounts["acc1"] = &Account{Address: "acc1", Balances: map[string]uint64{"CGO": 10}}
	st.Validators["v1"] = &Validator{Address: "v1", Stake: 5}
	st.Tokens["TK"] = &Token{TokenParams: types.TokenParams{Symbol: "TK", Supply: 1}, Creator: "acc1", TotalSupply: 1}
	st.Contracts["c1"] = &Contract{ID: "c1", Status: "active"}
	st.WasmContracts["w1"] = &WasmContract{Address: "w1", Storage: map[string][]byte{}}
	st.Slashed["s1"] = true
	st.Supply = Supply{Total: 100}
	st.BaseFee = 7
	st.Unbonding = []*Unbonding{{Address: "u1", Amount: 1, ReleaseAt: 2}}
	st.Shielded = &ShieldedPool{Balance: 9, Nullifiers: map[string]bool{}}
	return st
}

// TestSparseRootCoversAllComponents : la racine SMT doit changer si N'IMPORTE
// QUEL composant d'état change. Garantit qu'aucun composant n'est oublié dans
// sparseRootLocked — une omission casserait le consensus (la racine ne refléterait
// pas un changement d'état).
func TestSparseRootCoversAllComponents(t *testing.T) {
	base := populatedState(true).Root()
	mods := map[string]func(*State){
		"account (modif)":  func(s *State) { s.Accounts["acc1"].Balances["CGO"] = 11 },
		"account (ajout)":  func(s *State) { s.Accounts["acc2"] = &Account{Address: "acc2"} },
		"validator":        func(s *State) { s.Validators["v1"].Stake = 6 },
		"token":            func(s *State) { s.Tokens["TK"].TotalSupply = 2 },
		"contract":         func(s *State) { s.Contracts["c1"].Status = "completed" },
		"wasm contract":    func(s *State) { s.WasmContracts["w1"].Calls = 1 },
		"slashed":          func(s *State) { s.Slashed["s2"] = true },
		"supply":           func(s *State) { s.Supply.Total = 101 },
		"params":           func(s *State) { p := s.Params; p.MinBaseFee = 999; s.Params = p },
		"base_fee":         func(s *State) { s.BaseFee = 8 },
		"unbonding":        func(s *State) { s.Unbonding[0].Amount = 2 },
		"shielded pool":    func(s *State) { s.Shielded.Balance = 10 },
	}
	for name, mod := range mods {
		st := populatedState(true)
		mod(st)
		if st.Root() == base {
			t.Fatalf("composant %q : la racine SMT n'a PAS changé (composant non couvert → risque de fork)", name)
		}
	}
}

// TestSparseRootGate : le param SparseMerkleRoot bascule entre racine JSON et SMT,
// et la racine SMT est déterministe.
func TestSparseRootGate(t *testing.T) {
	jsonRoot := populatedState(false).Root()
	smtRoot := populatedState(true).Root()
	if jsonRoot == smtRoot {
		t.Fatal("racine JSON et racine SMT devraient différer")
	}
	if populatedState(true).Root() != smtRoot {
		t.Fatal("racine SMT non déterministe (deux états identiques, racines différentes)")
	}
}
