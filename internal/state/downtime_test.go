package state

import (
	"testing"

	"chaingo/internal/crypto"
	"chaingo/internal/types"
)

func getVal(st *State, addr string) *Validator {
	for _, v := range st.ListValidators() {
		if v.Address == addr {
			return v
		}
	}
	return nil
}

// TestDowntimeJailAndUnjail : un validateur élu mais absent au seuil est
// jailé (exclu du pouvoir) et légèrement slashé ; il rejoint via unjail une
// fois le délai écoulé.
func TestDowntimeJailAndUnjail(t *testing.T) {
	st := New()
	p := types.DefaultParams()
	p.DowntimeJailThreshold = 3 // seuil bas pour le test
	p.JailSeconds = 60
	st.SetParams(p)

	a, _ := crypto.GenerateKeyPair() // proposeur actif
	b, _ := crypto.GenerateKeyPair() // validateur absent
	const stake = 1_000_000 * types.Unit
	st.BootstrapStake(a.Address(), stake)
	st.BootstrapStake(b.Address(), stake)
	st.Mint(b.Address(), 1*types.Unit) // pour payer le futur unjail

	// 3 blocs où b est élu d'un round sauté (absent) ; a produit.
	for i := int64(1); i <= 3; i++ {
		if _, _, _, err := st.Execute(nil, nil, []string{b.Address()}, a.Address(), i*1000, true); err != nil {
			t.Fatalf("execute bloc %d: %v", i, err)
		}
	}

	vb := getVal(st, b.Address())
	if vb == nil || !vb.Jailed {
		t.Fatalf("b devrait être jailé après %d absences", p.DowntimeJailThreshold)
	}
	if st.PowerOf(b.Address()) != 0 {
		t.Fatal("un validateur jailé ne doit plus avoir de pouvoir de finalité")
	}
	wantStake := stake - types.MulDiv(stake, p.SlashDowntimeBps, 10_000) // -0,1 %
	if vb.Stake != wantStake {
		t.Fatalf("stake après slash downtime = %d, want %d", vb.Stake, wantStake)
	}
	jailedUntil := int64(3)*1000 + p.JailSeconds*1000

	// unjail AVANT la fin du jail → échoue.
	unjail := &types.Transaction{Type: types.TxUnjail, MaxBaseFee: 1 * types.Unit}
	unjail.SignWith(b)
	if _, _, _, err := st.Execute([]*types.Transaction{unjail}, nil, nil, "", jailedUntil-1, true); err == nil {
		t.Fatal("unjail avant la fin du délai devrait échouer")
	}

	// unjail APRÈS la fin du jail → succès, b redevient actif.
	unjail2 := &types.Transaction{Type: types.TxUnjail, MaxBaseFee: 1 * types.Unit}
	unjail2.SignWith(b)
	if _, _, _, err := st.Execute([]*types.Transaction{unjail2}, nil, nil, "", jailedUntil+1, true); err != nil {
		t.Fatalf("unjail après le délai: %v", err)
	}
	if getVal(st, b.Address()).Jailed {
		t.Fatal("b devrait être dé-jailé")
	}
	if st.PowerOf(b.Address()) != wantStake {
		t.Fatal("b devrait retrouver son pouvoir actif après unjail")
	}
}

// TestActiveProposerResetsMisses : produire remet le compteur d'absences à 0.
func TestActiveProposerResetsMisses(t *testing.T) {
	st := New()
	p := types.DefaultParams()
	p.DowntimeJailThreshold = 3
	st.SetParams(p)
	a, _ := crypto.GenerateKeyPair()
	b, _ := crypto.GenerateKeyPair()
	st.BootstrapStake(a.Address(), 1_000_000*types.Unit)
	st.BootstrapStake(b.Address(), 1_000_000*types.Unit)

	// b absent 2 fois (sous le seuil)...
	st.Execute(nil, nil, []string{b.Address()}, a.Address(), 1000, true)
	st.Execute(nil, nil, []string{b.Address()}, a.Address(), 2000, true)
	// ...puis b produit (proposeur) → reset.
	st.Execute(nil, nil, nil, b.Address(), 3000, true)
	// b absent encore 2 fois : ne doit PAS être jailé (compteur reparti de 0).
	st.Execute(nil, nil, []string{b.Address()}, a.Address(), 4000, true)
	st.Execute(nil, nil, []string{b.Address()}, a.Address(), 5000, true)
	if getVal(st, b.Address()).Jailed {
		t.Fatal("b ne devrait pas être jailé : produire a remis ses absences à 0")
	}
}
