package state

import (
	"testing"

	"chaingo/internal/crypto"
	"chaingo/internal/types"
)

func exec(t *testing.T, st *State, txs ...*types.Transaction) {
	t.Helper()
	if _, _, _, err := st.Execute(txs, nil, "", 1000, true); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

// TestMultisig2of3 : un coffre 2-of-3 ne paie qu'à la 2e approbation.
func TestMultisig2of3(t *testing.T) {
	st := New()
	st.SetParams(types.DefaultParams())

	a, _ := crypto.GenerateKeyPair()
	b, _ := crypto.GenerateKeyPair()
	c, _ := crypto.GenerateKeyPair()
	payee, _ := crypto.GenerateKeyPair()
	// a finance et crée le coffre ; b et c sont aussi signataires.
	st.Mint(a.Address(), 1_000*types.Unit)

	create := &types.Transaction{Type: types.TxContractCreate, MaxBaseFee: 1 * types.Unit,
		Contract: &types.ContractParams{Template: types.TemplateMultisig, TokenID: types.NativeToken,
			Amount: 500 * types.Unit, Signers: []string{a.Address(), b.Address(), c.Address()}, Threshold: 2}}
	create.SignWith(a)
	exec(t, st, create)
	cid := create.Hash()

	if got := st.GetContract(cid); got == nil || got.Threshold != 2 || len(got.Signers) != 3 {
		t.Fatal("coffre multisig mal créé")
	}

	// a propose de payer 100 CGO à payee (auto-approuvé par a = 1/2). nonce 1 (2e tx de a).
	st.Mint(b.Address(), 1*types.Unit) // frais
	propose := &types.Transaction{Type: types.TxContractExec, ContractID: cid, Action: types.ActionPropose,
		To: payee.Address(), Amount: 100 * types.Unit, Nonce: 1, MaxBaseFee: 1 * types.Unit}
	propose.SignWith(a)
	exec(t, st, propose)
	if bal := st.GetAccount(payee.Address()).Balances[types.NativeToken]; bal != 0 {
		t.Fatalf("paiement à 1/2 approbation ne doit PAS partir, got %d", bal)
	}

	// b approuve → 2/2 → exécution.
	approve := &types.Transaction{Type: types.TxContractExec, ContractID: cid, Action: types.ActionApprove,
		Proposal: 0, MaxBaseFee: 1 * types.Unit}
	approve.SignWith(b)
	exec(t, st, approve)
	if bal := st.GetAccount(payee.Address()).Balances[types.NativeToken]; bal != 100*types.Unit {
		t.Fatalf("paiement à 2/2 attendu 100 CGO, got %d", bal)
	}
	if st.GetContract(cid).Released != 100*types.Unit {
		t.Fatal("released devrait valoir 100 CGO")
	}
}

// TestMultisigGuards : non-signataire et double-approbation rejetés.
func TestMultisigGuards(t *testing.T) {
	st := New()
	st.SetParams(types.DefaultParams())
	a, _ := crypto.GenerateKeyPair()
	b, _ := crypto.GenerateKeyPair()
	stranger, _ := crypto.GenerateKeyPair()
	payee, _ := crypto.GenerateKeyPair()
	st.Mint(a.Address(), 1_000*types.Unit)
	st.Mint(stranger.Address(), 1*types.Unit)

	create := &types.Transaction{Type: types.TxContractCreate, MaxBaseFee: 1 * types.Unit,
		Contract: &types.ContractParams{Template: types.TemplateMultisig, TokenID: types.NativeToken,
			Amount: 200 * types.Unit, Signers: []string{a.Address(), b.Address()}, Threshold: 2}}
	create.SignWith(a)
	exec(t, st, create)
	cid := create.Hash()

	propose := &types.Transaction{Type: types.TxContractExec, ContractID: cid, Action: types.ActionPropose,
		To: payee.Address(), Amount: 50 * types.Unit, Nonce: 1, MaxBaseFee: 1 * types.Unit}
	propose.SignWith(a)
	exec(t, st, propose)

	// Étranger approuve → doit échouer (strict).
	bad := &types.Transaction{Type: types.TxContractExec, ContractID: cid, Action: types.ActionApprove,
		Proposal: 0, MaxBaseFee: 1 * types.Unit}
	bad.SignWith(stranger)
	if _, _, _, err := st.Execute([]*types.Transaction{bad}, nil, "", 1000, true); err == nil {
		t.Fatal("approbation par un non-signataire devrait échouer")
	}

	// a approuve une 2e fois → doit échouer (déjà approuvé en proposant). nonce 2.
	dup := &types.Transaction{Type: types.TxContractExec, ContractID: cid, Action: types.ActionApprove,
		Proposal: 0, Nonce: 2, MaxBaseFee: 1 * types.Unit}
	dup.SignWith(a)
	if _, _, _, err := st.Execute([]*types.Transaction{dup}, nil, "", 1000, true); err == nil {
		t.Fatal("double approbation du même signataire devrait échouer")
	}
}
