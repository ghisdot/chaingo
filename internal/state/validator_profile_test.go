package state

import (
	"testing"

	"chaingo/internal/types"
)

// TestValidatorProfileSetByValidator : un validateur staké publie son profil ;
// il est stocké sur le validateur.
func TestValidatorProfileSetByValidator(t *testing.T) {
	st := New()
	st.SetParams(testParams())
	v := mustKey(t)
	st.BootstrapStake(v.Address(), 100_000)
	st.Mint(v.Address(), 1_000)

	const info = "MonPool — https://monpool.io — validateur post-quantique"
	tx := &types.Transaction{Type: types.TxValidatorProfile, Memo: info, MaxBaseFee: 1}
	tx.SignWith(v)
	executeStateBlock(t, st, "", 1_000, tx)

	if got := st.Validators[v.Address()].Profile; got != info {
		t.Fatalf("profil = %q, want %q", got, info)
	}
}

// TestValidatorProfileRejectedForNonValidator : un compte non-validateur ne peut
// pas publier de profil.
func TestValidatorProfileRejectedForNonValidator(t *testing.T) {
	st := New()
	st.SetParams(testParams())
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000)

	tx := &types.Transaction{Type: types.TxValidatorProfile, Memo: "pas un validateur", MaxBaseFee: 1}
	tx.SignWith(alice)
	if _, _, _, err := st.Execute([]*types.Transaction{tx}, nil, nil, "", 1_000, true); err == nil {
		t.Fatal("un non-validateur ne doit pas pouvoir publier un profil")
	}
}
