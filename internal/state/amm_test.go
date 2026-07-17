package state

import (
	"testing"

	"chaingo/internal/crypto"
	"chaingo/internal/types"
)

// mkAMM crée deux tokens (AAA, BBB) détenus par `owner`, puis un pool AMM initial
// de rA×rB. Renvoie l'ID du contrat et le nonce suivant.
func mkAMM(t *testing.T, st *State, owner *crypto.KeyPair, rA, rB uint64) (string, uint64) {
	t.Helper()
	mkTokenFor(t, st, owner, "AAA", 1_000_000, 0)
	mkTokenFor(t, st, owner, "BBB", 1_000_000, 1)
	create := &types.Transaction{
		Type: types.TxContractCreate, MaxBaseFee: 1_000_000, Nonce: 2,
		Contract: &types.ContractParams{Template: types.TemplateAMM,
			TokenID: "AAA", Amount: rA, TokenB: "BBB", AmountB: rB},
	}
	create.SignWith(owner)
	executeStateBlock(t, st, "", 1_000, create)
	return create.Hash(), 3
}

// Création : réserves seedées, parts LP au créateur.
func TestAMMCreate(t *testing.T) {
	st := New()
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000*types.Unit)
	id, _ := mkAMM(t, st, alice, 1000, 2000)

	c := st.Contracts[id]
	if c.Amount != 1000 || c.ReserveB != 2000 {
		t.Fatalf("réserves = %d/%d, want 1000/2000", c.Amount, c.ReserveB)
	}
	if c.TotalShares != 1000 || c.Shares[alice.Address()] != 1000 {
		t.Fatalf("parts LP = %d (créateur %d), want 1000/1000", c.TotalShares, c.Shares[alice.Address()])
	}
	// Les deux jetons ont quitté le créateur (1_000_000 - réserve).
	if tokBal(st, alice.Address(), "AAA") != 999_000 || tokBal(st, alice.Address(), "BBB") != 998_000 {
		t.Fatalf("soldes créateur AAA/BBB = %d/%d", tokBal(st, alice.Address(), "AAA"), tokBal(st, alice.Address(), "BBB"))
	}
}

// Swap A→B : sortie = produit constant avec frais 0,3 %, k croît, réserves à jour.
func TestAMMSwap(t *testing.T) {
	st := New()
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000*types.Unit)
	id, nonce := mkAMM(t, st, alice, 1000, 1000) // k = 1e6

	// Swap 100 AAA -> BBB. Attendu : out = 1000·(100·997)/(1000·1000+100·997)
	// = 99_700_000 / 1_099_700 = 90 (floor).
	swap := &types.Transaction{Type: types.TxContractExec, ContractID: id, Action: types.ActionSwap,
		TokenID: "AAA", Amount: 100, MaxBaseFee: 1_000_000, Nonce: nonce}
	swap.SignWith(alice)
	bBefore := tokBal(st, alice.Address(), "BBB")
	executeStateBlock(t, st, "", 1_000, swap)

	if got := tokBal(st, alice.Address(), "BBB") - bBefore; got != 90 {
		t.Fatalf("sortie swap = %d BBB, want 90", got)
	}
	c := st.Contracts[id]
	if c.Amount != 1100 || c.ReserveB != 910 {
		t.Fatalf("réserves après swap = %d/%d, want 1100/910", c.Amount, c.ReserveB)
	}
	// k doit CROÎTRE (les frais restent dans le pool) : 1100·910 = 1_001_000 > 1e6.
	if c.Amount*c.ReserveB <= 1_000_000 {
		t.Fatalf("k n'a pas augmenté : %d", c.Amount*c.ReserveB)
	}
}

// Un swap avec un jeton hors pool est rejeté.
func TestAMMSwapWrongToken(t *testing.T) {
	st := New()
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000*types.Unit)
	id, nonce := mkAMM(t, st, alice, 1000, 1000)
	mkTokenFor(t, st, alice, "CCC", 1000, nonce) // jeton hors pool, nonce après le pool

	bad := &types.Transaction{Type: types.TxContractExec, ContractID: id, Action: types.ActionSwap,
		TokenID: "CCC", Amount: 10, MaxBaseFee: 1_000_000, Nonce: nonce + 1}
	bad.SignWith(alice)
	if _, _, _, err := st.Execute([]*types.Transaction{bad}, nil, nil, "", 1_000, true); err == nil {
		t.Fatal("swap d'un jeton hors pool devrait échouer")
	}
}

// Ajout puis retrait de liquidité par un second LP : parts proportionnelles,
// round-trip rend (à l'arrondi près) ce qui a été apporté.
func TestAMMAddRemoveLiquidity(t *testing.T) {
	st := New()
	alice, bob := mustKey(t), mustKey(t)
	st.Mint(alice.Address(), 1_000*types.Unit)
	st.Mint(bob.Address(), 1_000*types.Unit)
	id, _ := mkAMM(t, st, alice, 1000, 1000) // rA=rB=1000, total shares=1000

	// Alice donne à bob de quoi apporter de la liquidité.
	for i, sym := range []string{"AAA", "BBB"} {
		xfer := &types.Transaction{Type: types.TxTransfer, To: bob.Address(), TokenID: sym,
			Amount: 500, MaxBaseFee: 1_000_000, Nonce: uint64(3 + i)}
		xfer.SignWith(alice)
		executeStateBlock(t, st, "", 1_000, xfer)
	}

	// Bob apporte 200 AAA (+ 200 BBB au pro-rata) → mint = 1000·200/1000 = 200 parts.
	add := &types.Transaction{Type: types.TxContractExec, ContractID: id, Action: types.ActionAdd,
		TokenID: "AAA", Amount: 200, MaxBaseFee: 1_000_000, Nonce: 0}
	add.SignWith(bob)
	executeStateBlock(t, st, "", 1_000, add)

	c := st.Contracts[id]
	if c.Shares[bob.Address()] != 200 || c.TotalShares != 1200 {
		t.Fatalf("parts bob=%d total=%d, want 200/1200", c.Shares[bob.Address()], c.TotalShares)
	}
	if c.Amount != 1200 || c.ReserveB != 1200 {
		t.Fatalf("réserves après add = %d/%d, want 1200/1200", c.Amount, c.ReserveB)
	}
	if tokBal(st, bob.Address(), "AAA") != 300 || tokBal(st, bob.Address(), "BBB") != 300 {
		t.Fatalf("soldes bob après add = %d/%d, want 300/300", tokBal(st, bob.Address(), "AAA"), tokBal(st, bob.Address(), "BBB"))
	}

	// Bob retire ses 200 parts → 200·1200/1200 = 200 de chaque (pool non tradé).
	rem := &types.Transaction{Type: types.TxContractExec, ContractID: id, Action: types.ActionRemove,
		Amount: 200, MaxBaseFee: 1_000_000, Nonce: 1}
	rem.SignWith(bob)
	executeStateBlock(t, st, "", 1_000, rem)

	c = st.Contracts[id]
	if _, ok := c.Shares[bob.Address()]; ok {
		t.Fatal("bob devrait avoir 0 part après retrait total")
	}
	if c.TotalShares != 1000 || c.Amount != 1000 || c.ReserveB != 1000 {
		t.Fatalf("pool après retrait = shares %d, %d/%d", c.TotalShares, c.Amount, c.ReserveB)
	}
	if tokBal(st, bob.Address(), "AAA") != 500 || tokBal(st, bob.Address(), "BBB") != 500 {
		t.Fatalf("bob récupère %d/%d, want 500/500", tokBal(st, bob.Address(), "AAA"), tokBal(st, bob.Address(), "BBB"))
	}
}

// Retirer plus de parts qu'on n'en possède est rejeté.
func TestAMMRemoveTooMuch(t *testing.T) {
	st := New()
	alice, bob := mustKey(t), mustKey(t)
	st.Mint(alice.Address(), 1_000*types.Unit)
	st.Mint(bob.Address(), 1_000*types.Unit)
	id, _ := mkAMM(t, st, alice, 1000, 1000)

	rem := &types.Transaction{Type: types.TxContractExec, ContractID: id, Action: types.ActionRemove,
		Amount: 10, MaxBaseFee: 1_000_000, Nonce: 0}
	rem.SignWith(bob) // bob n'a aucune part
	if _, _, _, err := st.Execute([]*types.Transaction{rem}, nil, nil, "", 1_000, true); err == nil {
		t.Fatal("retrait sans parts LP devrait échouer")
	}
}

// Validation : deux jetons distincts et liquidité initiale non nulle.
func TestAMMValidation(t *testing.T) {
	same := &types.Transaction{Type: types.TxContractCreate, MaxBaseFee: 1,
		Contract: &types.ContractParams{Template: types.TemplateAMM, TokenID: "AAA", Amount: 1, TokenB: "AAA", AmountB: 1}}
	if err := same.ValidateBasic(); err == nil {
		t.Fatal("pool avec deux fois le même jeton accepté")
	}
	zero := &types.Transaction{Type: types.TxContractCreate, MaxBaseFee: 1,
		Contract: &types.ContractParams{Template: types.TemplateAMM, TokenID: "AAA", Amount: 0, TokenB: "BBB", AmountB: 1}}
	if err := zero.ValidateBasic(); err == nil {
		t.Fatal("pool avec liquidité initiale nulle accepté")
	}
}
