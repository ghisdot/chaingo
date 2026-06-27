package state

import (
	"testing"

	"chaingo/internal/types"
)

// Crée un token mintable plafonné, puis vérifie : le mint sous le plafond passe,
// le mint qui dépasserait le plafond est refusé (et n'altère rien).
func TestTokenMaxSupplyCapEnforced(t *testing.T) {
	st := New()
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000*types.Unit) // pour les frais

	create := &types.Transaction{
		Type: types.TxCreateToken, MaxBaseFee: 1, Nonce: 0,
		Token: &types.TokenParams{Symbol: "CAP", Name: "Capped", Decimals: 0,
			Supply: 100, Mintable: true, MaxSupply: 150},
	}
	create.SignWith(alice)
	executeStateBlock(t, st, "", 1_000, create)

	// Mint 40 -> total 140 <= 150 : OK.
	mintOK := &types.Transaction{Type: types.TxMint, TokenID: "CAP", Amount: 40, MaxBaseFee: 1, Nonce: 1}
	mintOK.SignWith(alice)
	executeStateBlock(t, st, "", 1_000, mintOK)
	if got := st.GetToken("CAP").TotalSupply; got != 140 {
		t.Fatalf("supply après mint = %d, want 140", got)
	}

	// Mint 20 -> total 160 > 150 : refusé, supply inchangé.
	mintKO := &types.Transaction{Type: types.TxMint, TokenID: "CAP", Amount: 20, MaxBaseFee: 1, Nonce: 2}
	mintKO.SignWith(alice)
	if _, _, _, err := st.Execute([]*types.Transaction{mintKO}, nil, nil, "", 1_000, true); err == nil {
		t.Fatal("mint au-delà du plafond devrait être refusé")
	}
	if got := st.GetToken("CAP").TotalSupply; got != 140 {
		t.Fatalf("supply après mint refusé = %d, want 140 (inchangé)", got)
	}
}

// max_supply sans mintable est rejeté à la validation.
func TestTokenMaxSupplyRequiresMintable(t *testing.T) {
	tx := &types.Transaction{
		Type: types.TxCreateToken, MaxBaseFee: 1,
		Token: &types.TokenParams{Symbol: "BAD", Name: "x", Decimals: 0, Supply: 10, Mintable: false, MaxSupply: 100},
	}
	if err := tx.ValidateBasic(); err == nil {
		t.Fatal("max_supply sans mintable devrait être rejeté")
	}
}

// Token burnable : un détenteur brûle ses jetons -> solde ET supply baissent.
// Un token NON burnable refuse le burn.
func TestTokenBurn(t *testing.T) {
	st := New()
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000*types.Unit)

	create := &types.Transaction{
		Type: types.TxCreateToken, MaxBaseFee: 1, Nonce: 0,
		Token: &types.TokenParams{Symbol: "BRN", Name: "Burnable", Decimals: 0, Supply: 1_000, Burnable: true},
	}
	create.SignWith(alice)
	executeStateBlock(t, st, "", 1_000, create)

	burn := &types.Transaction{Type: types.TxBurn, TokenID: "BRN", Amount: 300, MaxBaseFee: 1, Nonce: 1}
	burn.SignWith(alice)
	executeStateBlock(t, st, "", 1_000, burn)

	if got := st.GetToken("BRN").TotalSupply; got != 700 {
		t.Fatalf("supply après burn = %d, want 700", got)
	}
	if got := st.GetAccount(alice.Address()).Balances["BRN"]; got != 700 {
		t.Fatalf("solde après burn = %d, want 700", got)
	}

	// Brûler plus que son solde : refusé.
	tooMuch := &types.Transaction{Type: types.TxBurn, TokenID: "BRN", Amount: 10_000, MaxBaseFee: 1, Nonce: 2}
	tooMuch.SignWith(alice)
	if _, _, _, err := st.Execute([]*types.Transaction{tooMuch}, nil, nil, "", 1_000, true); err == nil {
		t.Fatal("burn au-delà du solde devrait être refusé")
	}
}

// Un token non burnable refuse le burn (au niveau état).
func TestTokenBurnRequiresBurnable(t *testing.T) {
	st := New()
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000*types.Unit)

	create := &types.Transaction{
		Type: types.TxCreateToken, MaxBaseFee: 1, Nonce: 0,
		Token: &types.TokenParams{Symbol: "NOB", Name: "NoBurn", Decimals: 0, Supply: 1_000, Burnable: false},
	}
	create.SignWith(alice)
	executeStateBlock(t, st, "", 1_000, create)

	burn := &types.Transaction{Type: types.TxBurn, TokenID: "NOB", Amount: 1, MaxBaseFee: 1, Nonce: 1}
	burn.SignWith(alice)
	if _, _, _, err := st.Execute([]*types.Transaction{burn}, nil, nil, "", 1_000, true); err == nil {
		t.Fatal("burn d'un token non burnable devrait être refusé")
	}
}

// Métadonnées : conservées à la création et exposées sur le token.
func TestTokenMetadataStored(t *testing.T) {
	st := New()
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000*types.Unit)

	create := &types.Transaction{
		Type: types.TxCreateToken, MaxBaseFee: 1, Nonce: 0,
		Token: &types.TokenParams{Symbol: "META", Name: "Meta", Decimals: 2, Supply: 50,
			LogoURI: "https://e/l.png", Description: "desc", Website: "https://e"},
	}
	create.SignWith(alice)
	executeStateBlock(t, st, "", 1_000, create)

	tk := st.GetToken("META")
	if tk.LogoURI != "https://e/l.png" || tk.Description != "desc" || tk.Website != "https://e" {
		t.Fatalf("métadonnées non conservées: %+v", tk.TokenParams)
	}
}
