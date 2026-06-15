package state

import (
	"testing"

	"chaingo/internal/crypto"
	"chaingo/internal/types"
)

// TestSlashDoubleSign : une preuve d'équivocation réduit le stake du
// validateur de SlashDoubleSignBps, brûle le montant, et est idempotente.
func TestSlashDoubleSign(t *testing.T) {
	st := New()
	p := types.DefaultParams() // SlashDoubleSignBps = 500 (5 %)
	st.SetParams(p)

	val, _ := crypto.GenerateKeyPair()
	const stake = 1_000_000 * types.Unit
	st.BootstrapStake(val.Address(), stake)

	supplyBefore := st.GetSupply()

	// Deux votes conflictuels du même validateur à la hauteur 5.
	mkVote := func(hash string) *types.Vote {
		v := &types.Vote{ChainID: "c", Height: 5, Kind: types.PrecommitKind, BlockHash: hash}
		v.SignWith(val)
		return v
	}
	ev := &types.DoubleSignEvidence{Height: 5, Voter: val.Address(), VoteA: mkVote("A"), VoteB: mkVote("B")}
	if err := ev.Verify("c"); err != nil {
		t.Fatalf("evidence valide rejetée: %v", err)
	}

	// Exécution d'un bloc portant la preuve.
	if _, _, _, err := st.Execute(nil, []*types.DoubleSignEvidence{ev}, nil, "", 1000, true); err != nil {
		t.Fatalf("execute: %v", err)
	}

	wantCut := types.MulDiv(stake, p.SlashDoubleSignBps, 10_000) // 5 %
	gotStake := st.PowerOf(val.Address())
	if gotStake != stake-wantCut {
		t.Fatalf("stake après slash = %d, want %d", gotStake, stake-wantCut)
	}
	burned := st.GetSupply().Burned - supplyBefore.Burned
	if burned != wantCut {
		t.Fatalf("brûlé = %d, want %d", burned, wantCut)
	}
	if !st.IsSlashed(val.Address(), 5) {
		t.Fatal("la faute devrait être marquée slashée")
	}

	// Idempotence : rejouer la même preuve ne re-slashe pas.
	if _, _, _, err := st.Execute(nil, []*types.DoubleSignEvidence{ev}, nil, "", 2000, true); err != nil {
		t.Fatalf("execute 2: %v", err)
	}
	if st.PowerOf(val.Address()) != stake-wantCut {
		t.Fatal("double slash : le stake a baissé une 2e fois")
	}
}

// TestSlashHitsDelegations : le slash entame aussi les délégations, au
// même taux, et les brûle.
func TestSlashHitsDelegations(t *testing.T) {
	st := New()
	st.SetParams(types.DefaultParams())
	val, _ := crypto.GenerateKeyPair()
	st.BootstrapStake(val.Address(), 1_000_000*types.Unit)

	// Un délégateur financé qui délègue 100 000 CGO.
	del, _ := crypto.GenerateKeyPair()
	st.Mint(del.Address(), 200_000*types.Unit)
	dtx := &types.Transaction{Type: types.TxDelegate, To: val.Address(), TokenID: types.NativeToken, Amount: 100_000 * types.Unit, MaxBaseFee: 1 * types.Unit}
	dtx.SignWith(del)
	if _, _, _, err := st.Execute([]*types.Transaction{dtx}, nil, nil, "", 1000, true); err != nil {
		t.Fatalf("delegate: %v", err)
	}

	acct := st.GetAccount(del.Address())
	deleg := acct.Delegations[val.Address()]
	if deleg != 100_000*types.Unit {
		t.Fatalf("délégation attendue 100000 CGO, got %d", deleg)
	}

	v1 := &types.Vote{ChainID: "c", Height: 9, Kind: types.PrecommitKind, BlockHash: "X"}
	v1.SignWith(val)
	v2 := &types.Vote{ChainID: "c", Height: 9, Kind: types.PrecommitKind, BlockHash: "Y"}
	v2.SignWith(val)
	ev := &types.DoubleSignEvidence{Height: 9, Voter: val.Address(), VoteA: v1, VoteB: v2}
	if _, _, _, err := st.Execute(nil, []*types.DoubleSignEvidence{ev}, nil, "", 2000, true); err != nil {
		t.Fatalf("slash: %v", err)
	}

	want := 100_000*types.Unit - types.MulDiv(100_000*types.Unit, 500, 10_000) // -5 %
	got := st.GetAccount(del.Address()).Delegations[val.Address()]
	if got != want {
		t.Fatalf("délégation après slash = %d, want %d", got, want)
	}
}
