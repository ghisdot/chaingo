package state

import (
	"testing"

	"chaingo/internal/crypto"
	"chaingo/internal/types"
)

// daoParams : params de test sans frais (assertions de solde nettes).
func daoParams() types.Params {
	p := types.DefaultParams()
	p.MinBaseFee = 0
	p.TokenCreateFee = 0
	p.ContractCreateFee = 0
	p.InflationRateBps = 0
	return p
}

// setupDAO : crée un DAO à `nMembers` membres, quorum `threshold`, trésorerie
// `treasury` CGO, financée par le créateur (1er membre). Renvoie l'état, les
// clés des membres et l'ID du contrat.
func setupDAO(t *testing.T, nMembers int, threshold, treasury uint64) (*State, []*crypto.KeyPair, string) {
	t.Helper()
	st := New()
	st.SetParams(daoParams())
	members := make([]*crypto.KeyPair, nMembers)
	addrs := make([]string, nMembers)
	for i := range members {
		members[i], _ = crypto.GenerateKeyPair()
		addrs[i] = members[i].Address()
	}
	st.Mint(members[0].Address(), treasury) // le créateur finance la trésorerie

	create := &types.Transaction{
		Type: types.TxContractCreate, MaxBaseFee: 1,
		Contract: &types.ContractParams{
			Template: types.TemplateDAO, TokenID: types.NativeToken,
			Amount: treasury, Signers: addrs, Threshold: threshold,
		},
	}
	create.SignWith(members[0])
	if _, _, _, err := st.Execute([]*types.Transaction{create}, nil, nil, "", 1000, true); err != nil {
		t.Fatalf("create dao: %v", err)
	}
	return st, members, create.Hash()
}

func daoExec(t *testing.T, st *State, kp *crypto.KeyPair, id, action string, proposal uint64, to string, amount uint64, blockTime int64) error {
	t.Helper()
	tx := &types.Transaction{
		Type: types.TxContractExec, MaxBaseFee: 1, ContractID: id,
		Action: action, Proposal: proposal, To: to, Amount: amount,
		Nonce: st.NonceOf(kp.Address()), // nonce courant du membre
	}
	tx.SignWith(kp)
	_, _, _, err := st.Execute([]*types.Transaction{tx}, nil, nil, "", blockTime, true)
	return err
}

// TestDAOProposalPassesAtQuorum : une proposition financée passe quand le
// quorum de votes POUR est atteint, et la trésorerie paie le bénéficiaire.
func TestDAOProposalPassesAtQuorum(t *testing.T) {
	st, members, id := setupDAO(t, 3, 2, 1000) // 3 membres, quorum 2, trésorerie 1000
	beneficiary, _ := crypto.GenerateKeyPair()

	// Membre 0 propose 600 vers beneficiary (vote POUR d'office → 1/2).
	if err := daoExec(t, st, members[0], id, types.ActionPropose, 0, beneficiary.Address(), 600, 2000); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if got := st.GetContract(id).Proposals[0].Executed; got {
		t.Fatal("1 vote POUR ne doit pas suffire (quorum 2)")
	}
	if bal := st.GetAccount(beneficiary.Address()).Balances[types.NativeToken]; bal != 0 {
		t.Fatalf("bénéficiaire ne doit rien avoir encore, got %d", bal)
	}

	// Membre 1 vote POUR → quorum 2/2 atteint → exécution.
	if err := daoExec(t, st, members[1], id, types.ActionApprove, 0, "", 0, 3000); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !st.GetContract(id).Proposals[0].Executed {
		t.Fatal("au quorum, la proposition doit s'exécuter")
	}
	if bal := st.GetAccount(beneficiary.Address()).Balances[types.NativeToken]; bal != 600 {
		t.Fatalf("bénéficiaire doit avoir 600, got %d", bal)
	}
	if rel := st.GetContract(id).Released; rel != 600 {
		t.Fatalf("released doit être 600, got %d", rel)
	}
}

// TestDAOProposalRejectedByAgainst : assez de votes CONTRE rendent le quorum
// POUR inatteignable → la proposition est rejetée, rien n'est payé.
func TestDAOProposalRejectedByAgainst(t *testing.T) {
	st, members, id := setupDAO(t, 3, 2, 1000) // quorum 2 sur 3 membres
	beneficiary, _ := crypto.GenerateKeyPair()

	// M0 propose (POUR = 1). M1 et M2 votent CONTRE → POUR max = 3-2 = 1 < 2 → rejet.
	daoExec(t, st, members[0], id, types.ActionPropose, 0, beneficiary.Address(), 600, 2000)
	daoExec(t, st, members[1], id, types.ActionReject, 0, "", 0, 3000)
	if st.GetContract(id).Proposals[0].Rejected {
		t.Fatal("1 vote CONTRE ne suffit pas à rejeter (POUR max = 2 = quorum)")
	}
	if err := daoExec(t, st, members[2], id, types.ActionReject, 0, "", 0, 4000); err != nil {
		t.Fatalf("reject: %v", err)
	}
	p := st.GetContract(id).Proposals[0]
	if !p.Rejected {
		t.Fatal("2 votes CONTRE sur 3 membres (quorum 2) doivent rejeter")
	}
	if p.Executed {
		t.Fatal("une proposition rejetée ne doit pas s'exécuter")
	}
	if bal := st.GetAccount(beneficiary.Address()).Balances[types.NativeToken]; bal != 0 {
		t.Fatalf("rien ne doit être payé sur un rejet, got %d", bal)
	}
}

// TestDAOAccessAndDoubleVoteGuards : non-membre rejeté ; double vote rejeté ;
// vote sur proposition résolue rejeté.
func TestDAOAccessAndDoubleVoteGuards(t *testing.T) {
	st, members, id := setupDAO(t, 3, 2, 1000)
	stranger, _ := crypto.GenerateKeyPair()
	beneficiary, _ := crypto.GenerateKeyPair()

	// Non-membre ne peut pas proposer.
	if err := daoExec(t, st, stranger, id, types.ActionPropose, 0, beneficiary.Address(), 100, 2000); err == nil {
		t.Fatal("un non-membre ne doit pas pouvoir proposer")
	}
	// M0 propose.
	daoExec(t, st, members[0], id, types.ActionPropose, 0, beneficiary.Address(), 600, 3000)
	// Non-membre ne peut pas voter.
	if err := daoExec(t, st, stranger, id, types.ActionApprove, 0, "", 0, 4000); err == nil {
		t.Fatal("un non-membre ne doit pas pouvoir voter")
	}
	// M0 a déjà voté POUR (en proposant) → re-vote refusé.
	if err := daoExec(t, st, members[0], id, types.ActionApprove, 0, "", 0, 5000); err == nil {
		t.Fatal("double vote du même membre doit être refusé")
	}
	// M0 ne peut pas voter CONTRE après avoir voté POUR.
	if err := daoExec(t, st, members[0], id, types.ActionReject, 0, "", 0, 6000); err == nil {
		t.Fatal("voter POUR puis CONTRE doit être refusé")
	}
	// M1 vote POUR → exécution. Ensuite tout vote sur la proposition résolue échoue.
	daoExec(t, st, members[1], id, types.ActionApprove, 0, "", 0, 7000)
	if !st.GetContract(id).Proposals[0].Executed {
		t.Fatal("la proposition doit être exécutée")
	}
	if err := daoExec(t, st, members[2], id, types.ActionApprove, 0, "", 0, 8000); err == nil {
		t.Fatal("voter sur une proposition déjà résolue doit être refusé")
	}
}

// TestDAOTreasuryGuard : une proposition supérieure à la trésorerie disponible
// est refusée à la proposition.
func TestDAOTreasuryGuard(t *testing.T) {
	st, members, id := setupDAO(t, 2, 1, 500)
	beneficiary, _ := crypto.GenerateKeyPair()
	if err := daoExec(t, st, members[0], id, types.ActionPropose, 0, beneficiary.Address(), 600, 2000); err == nil {
		t.Fatal("proposer plus que la trésorerie doit échouer")
	}
}
