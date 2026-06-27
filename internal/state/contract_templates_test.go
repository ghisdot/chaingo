package state

import (
	"testing"

	"chaingo/internal/crypto"
	"chaingo/internal/types"
)

// mkTokenFor crée un token (decimals 0) détenu par `owner` et le finance en CGO
// pour les frais. Renvoie le symbole.
func mkTokenFor(t *testing.T, st *State, owner *crypto.KeyPair, sym string, supply uint64, nonce uint64) {
	t.Helper()
	tx := &types.Transaction{
		Type: types.TxCreateToken, MaxBaseFee: 1_000_000, Nonce: nonce,
		Token: &types.TokenParams{Symbol: sym, Name: sym, Decimals: 0, Supply: supply, Burnable: true},
	}
	tx.SignWith(owner)
	executeStateBlock(t, st, "", 1_000, tx)
}

func tokBal(st *State, addr, sym string) uint64 { return st.GetAccount(addr).Balances[sym] }

// TIMELOCK : fonds verrouillés jusqu'à EndMs, puis réclamables en totalité par le
// bénéficiaire. Réclamer avant l'échéance échoue.
func TestTemplateTimelock(t *testing.T) {
	st := New()
	alice, bob := mustKey(t), mustKey(t)
	st.Mint(alice.Address(), 1000*types.Unit)
	st.Mint(bob.Address(), 1000*types.Unit)
	mkTokenFor(t, st, alice, "TST", 1_000_000, 0)

	const unlock = int64(5_000)
	create := &types.Transaction{Type: types.TxContractCreate, MaxBaseFee: 1_000_000, Nonce: 1,
		Contract: &types.ContractParams{Template: types.TemplateTimelock, TokenID: "TST", Amount: 1000,
			Beneficiary: bob.Address(), EndMs: unlock}}
	create.SignWith(alice)
	executeStateBlock(t, st, "", 1_000, create)
	id := create.Hash()

	// Réclamer avant l'échéance (blockTime 2000 < 5000) : refusé.
	early := &types.Transaction{Type: types.TxContractExec, ContractID: id, Action: types.ActionClaim, MaxBaseFee: 1_000_000, Nonce: 0}
	early.SignWith(bob)
	if _, _, _, err := st.Execute([]*types.Transaction{early}, nil, nil, "", 2_000, true); err == nil {
		t.Fatal("timelock: claim avant l'échéance devrait échouer")
	}

	// Après l'échéance (blockTime 6000) : bob reçoit tout.
	claim := &types.Transaction{Type: types.TxContractExec, ContractID: id, Action: types.ActionClaim, MaxBaseFee: 1_000_000, Nonce: 0}
	claim.SignWith(bob)
	executeStateBlock(t, st, "", 6_000, claim)
	if got := tokBal(st, bob.Address(), "TST"); got != 1000 {
		t.Fatalf("timelock: solde bob = %d, want 1000", got)
	}
	if st.Contracts[id].Status != "completed" {
		t.Fatalf("timelock: statut = %s, want completed", st.Contracts[id].Status)
	}
}

// STREAMING : flux linéaire ; le bénéficiaire réclame l'acquis, le créateur annule
// et récupère le non-acquis.
func TestTemplateStreaming(t *testing.T) {
	st := New()
	alice, bob := mustKey(t), mustKey(t)
	st.Mint(alice.Address(), 1000*types.Unit)
	st.Mint(bob.Address(), 1000*types.Unit)
	mkTokenFor(t, st, alice, "STR", 1_000_000, 0)

	create := &types.Transaction{Type: types.TxContractCreate, MaxBaseFee: 1_000_000, Nonce: 1,
		Contract: &types.ContractParams{Template: types.TemplateStreaming, TokenID: "STR", Amount: 1000,
			Beneficiary: bob.Address(), StartMs: 1_000, EndMs: 11_000}}
	create.SignWith(alice)
	executeStateBlock(t, st, "", 1_000, create)
	id := create.Hash()

	// À 50 % (blockTime 6000), bob réclame ~500.
	claim := &types.Transaction{Type: types.TxContractExec, ContractID: id, Action: types.ActionClaim, MaxBaseFee: 1_000_000, Nonce: 0}
	claim.SignWith(bob)
	executeStateBlock(t, st, "", 6_000, claim)
	if got := tokBal(st, bob.Address(), "STR"); got != 500 {
		t.Fatalf("streaming: bob après claim 50%% = %d, want 500", got)
	}

	// Le créateur annule à 50 % : bob ne gagne rien de plus (déjà réclamé), alice
	// récupère les 500 non acquis.
	aliceBefore := tokBal(st, alice.Address(), "STR")
	cancel := &types.Transaction{Type: types.TxContractExec, ContractID: id, Action: types.ActionCancel, MaxBaseFee: 1_000_000, Nonce: 2}
	cancel.SignWith(alice)
	executeStateBlock(t, st, "", 6_000, cancel)
	if got := tokBal(st, alice.Address(), "STR") - aliceBefore; got != 500 {
		t.Fatalf("streaming: remboursement créateur = %d, want 500", got)
	}
	if st.Contracts[id].Status != "cancelled" {
		t.Fatalf("streaming: statut = %s, want cancelled", st.Contracts[id].Status)
	}
}

// AIRDROP : part égale réclamable une fois par destinataire ; non-destinataire et
// double-réclamation refusés ; la poussière revient au créateur à la fin.
func TestTemplateAirdrop(t *testing.T) {
	st := New()
	alice, b, c, stranger := mustKey(t), mustKey(t), mustKey(t), mustKey(t)
	for _, k := range []*crypto.KeyPair{alice, b, c, stranger} {
		st.Mint(k.Address(), 1000*types.Unit)
	}
	mkTokenFor(t, st, alice, "AIR", 1_000_000, 0)

	// 1001 unités sur 2 destinataires => part 500, poussière 1.
	create := &types.Transaction{Type: types.TxContractCreate, MaxBaseFee: 1_000_000, Nonce: 1,
		Contract: &types.ContractParams{Template: types.TemplateAirdrop, TokenID: "AIR", Amount: 1001,
			Signers: []string{b.Address(), c.Address()}}}
	create.SignWith(alice)
	executeStateBlock(t, st, "", 1_000, create)
	id := create.Hash()
	aliceBefore := tokBal(st, alice.Address(), "AIR")

	// Étranger : refusé.
	bad := &types.Transaction{Type: types.TxContractExec, ContractID: id, Action: types.ActionClaim, MaxBaseFee: 1_000_000, Nonce: 0}
	bad.SignWith(stranger)
	if _, _, _, err := st.Execute([]*types.Transaction{bad}, nil, nil, "", 1_000, true); err == nil {
		t.Fatal("airdrop: réclamation par un non-destinataire devrait échouer")
	}

	claimB := &types.Transaction{Type: types.TxContractExec, ContractID: id, Action: types.ActionClaim, MaxBaseFee: 1_000_000, Nonce: 0}
	claimB.SignWith(b)
	executeStateBlock(t, st, "", 1_000, claimB)
	if got := tokBal(st, b.Address(), "AIR"); got != 500 {
		t.Fatalf("airdrop: part de b = %d, want 500", got)
	}
	// Double réclamation : refusée.
	again := &types.Transaction{Type: types.TxContractExec, ContractID: id, Action: types.ActionClaim, MaxBaseFee: 1_000_000, Nonce: 1}
	again.SignWith(b)
	if _, _, _, err := st.Execute([]*types.Transaction{again}, nil, nil, "", 1_000, true); err == nil {
		t.Fatal("airdrop: double réclamation devrait échouer")
	}

	claimC := &types.Transaction{Type: types.TxContractExec, ContractID: id, Action: types.ActionClaim, MaxBaseFee: 1_000_000, Nonce: 0}
	claimC.SignWith(c)
	executeStateBlock(t, st, "", 1_000, claimC)
	if got := tokBal(st, c.Address(), "AIR"); got != 500 {
		t.Fatalf("airdrop: part de c = %d, want 500", got)
	}
	// Tous ont réclamé : poussière (1) au créateur, statut completed.
	if got := tokBal(st, alice.Address(), "AIR") - aliceBefore; got != 1 {
		t.Fatalf("airdrop: poussière au créateur = %d, want 1", got)
	}
	if st.Contracts[id].Status != "completed" {
		t.Fatalf("airdrop: statut = %s, want completed", st.Contracts[id].Status)
	}
}

// PRESALE : vente d'un token à prix fixe contre CGO ; l'acheteur reçoit les tokens,
// le créateur encaisse les CGO ; le créateur clôt et récupère l'invendu.
func TestTemplatePresale(t *testing.T) {
	st := New()
	alice, buyer := mustKey(t), mustKey(t)
	st.Mint(alice.Address(), 1000*types.Unit)
	st.Mint(buyer.Address(), 1000*types.Unit)
	mkTokenFor(t, st, alice, "SALE", 1_000_000, 0)

	// Vend 100 SALE à 2 CGO l'unité (price en ucgo).
	const price = 2 * types.Unit
	create := &types.Transaction{Type: types.TxContractCreate, MaxBaseFee: 1_000_000, Nonce: 1,
		Contract: &types.ContractParams{Template: types.TemplatePresale, TokenID: "SALE", Amount: 100, Price: price}}
	create.SignWith(alice)
	executeStateBlock(t, st, "", 1_000, create)
	id := create.Hash()

	aliceCgoBefore := tokBal(st, alice.Address(), "CGO")

	// L'acheteur dépense 30 CGO => 15 SALE (coût exact 30 CGO).
	buy := &types.Transaction{Type: types.TxContractExec, ContractID: id, Action: types.ActionBuy,
		Amount: 30 * types.Unit, MaxBaseFee: 1_000_000, Nonce: 0}
	buy.SignWith(buyer)
	executeStateBlock(t, st, "", 1_000, buy)
	if got := tokBal(st, buyer.Address(), "SALE"); got != 15 {
		t.Fatalf("presale: tokens achetés = %d, want 15", got)
	}
	if got := tokBal(st, alice.Address(), "CGO") - aliceCgoBefore; got != 30*types.Unit {
		t.Fatalf("presale: CGO encaissés par le créateur = %d, want %d", got, 30*types.Unit)
	}
	if st.Contracts[id].Released != 15 {
		t.Fatalf("presale: inventaire vendu = %d, want 15", st.Contracts[id].Released)
	}

	// Le créateur clôt : récupère l'invendu (85 SALE).
	aliceSaleBefore := tokBal(st, alice.Address(), "SALE")
	cancel := &types.Transaction{Type: types.TxContractExec, ContractID: id, Action: types.ActionCancel, MaxBaseFee: 1_000_000, Nonce: 2}
	cancel.SignWith(alice)
	executeStateBlock(t, st, "", 1_000, cancel)
	if got := tokBal(st, alice.Address(), "SALE") - aliceSaleBefore; got != 85 {
		t.Fatalf("presale: invendu récupéré = %d, want 85", got)
	}
	if st.Contracts[id].Status != "cancelled" {
		t.Fatalf("presale: statut = %s, want cancelled", st.Contracts[id].Status)
	}
}
