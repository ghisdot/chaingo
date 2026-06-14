package genesis

import (
	"testing"

	"chaingo/internal/crypto"
	"chaingo/internal/state"
	"chaingo/internal/types"
)

func mkGenesis(t *testing.T) (*Genesis, *crypto.KeyPair, *crypto.KeyPair) {
	t.Helper()
	val, _ := crypto.GenerateKeyPair()
	team, _ := crypto.GenerateKeyPair()
	p := types.DefaultParams()
	g := &Genesis{
		ChainID:   "chaingo-1",
		Timestamp: 1_700_000_000_000,
		Params:    &p,
		Alloc:     map[string]uint64{val.Address(): 1_000 * types.Unit},
		Stakes:    map[string]uint64{val.Address(): 1_000_000 * types.Unit},
		Vesting:   []VestingGrant{{Beneficiary: team.Address(), Amount: 150_000_000 * types.Unit, StartMs: 1_700_000_000_000, EndMs: 1_800_000_000_000}},
	}
	return g, val, team
}

// TestGenesisDeterministic : deux applications de la MÊME genèse donnent le
// même hash de bloc et la même racine d'état (exigence multi-nœuds).
func TestGenesisDeterministic(t *testing.T) {
	g, _, _ := mkGenesis(t)
	b1 := g.Apply(state.New())
	b2 := g.Apply(state.New())
	if b1.Hash != b2.Hash {
		t.Fatalf("hash de genèse non déterministe: %s != %s", b1.Hash, b2.Hash)
	}
	if b1.Header.StateRoot != b2.Header.StateRoot {
		t.Fatal("racine d'état de genèse non déterministe")
	}
}

// TestGenesisSupplyAndVesting : la supply totale = liquide + staké + vesting,
// et les fonds vestés sont verrouillés (pas dans un solde) puis réclamables.
func TestGenesisSupplyAndVesting(t *testing.T) {
	g, _, team := mkGenesis(t)
	st := state.New()
	g.Apply(st)

	sum, err := g.Validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	want := uint64(1_000*types.Unit) + uint64(1_000_000*types.Unit) + uint64(150_000_000*types.Unit)
	if sum.TotalSupply != want {
		t.Fatalf("supply totale = %d, want %d", sum.TotalSupply, want)
	}
	if st.GetSupply().Total != want {
		t.Fatalf("supply on-chain = %d, want %d", st.GetSupply().Total, want)
	}
	// Le bénéficiaire n'a encore aucun solde liquide (tout est verrouillé).
	if bal := st.GetAccount(team.Address()).Balances[types.NativeToken]; bal != 0 {
		t.Fatalf("le vesting ne doit pas créditer de solde liquide à la genèse, got %d", bal)
	}
	if len(st.ListContracts()) != 1 {
		t.Fatalf("1 contrat de vesting attendu, got %d", len(st.ListContracts()))
	}

	// Le bénéficiaire a besoin d'un peu de CGO liquide pour payer les frais
	// de claim (à prévoir dans la distribution mainnet).
	st.Mint(team.Address(), 1*types.Unit)

	// À mi-parcours, la moitié du vesting est réclamable.
	mid := (g.Vesting[0].StartMs + g.Vesting[0].EndMs) / 2
	cid := st.ListContracts()[0].ID
	claim := &types.Transaction{Type: types.TxContractExec, ContractID: cid, Action: types.ActionClaim, MaxBaseFee: 1 * types.Unit}
	claim.SignWith(team)
	if _, _, _, err := st.Execute([]*types.Transaction{claim}, nil, nil, "", mid, true); err != nil {
		t.Fatalf("claim: %v", err)
	}
	got := st.GetAccount(team.Address()).Balances[types.NativeToken]
	half := uint64(150_000_000 * types.Unit / 2)
	// got ≈ moitié vestée + 1 CGO de départ − frais (négligeables).
	if got < half || got > half+1*types.Unit {
		t.Fatalf("réclamé à mi-parcours = %d, attendu entre %d et %d", got, half, half+1*types.Unit)
	}
}

// TestGenesisValidateRejects : stake sous le minimum, adresse invalide.
func TestGenesisValidateRejects(t *testing.T) {
	g, _, _ := mkGenesis(t)
	g.Stakes[firstKey(g.Stakes)] = 5_000 * types.Unit // sous les 10 000
	if _, err := g.Validate(); err == nil {
		t.Fatal("stake sous le minimum devrait être rejeté")
	}

	g2, _, _ := mkGenesis(t)
	g2.Alloc["pas-une-adresse"] = 100
	if _, err := g2.Validate(); err == nil {
		t.Fatal("adresse invalide devrait être rejetée")
	}
}

func firstKey(m map[string]uint64) string {
	for k := range m {
		return k
	}
	return ""
}
