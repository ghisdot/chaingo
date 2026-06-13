package state

import (
	"testing"

	"chaingo/internal/types"
)

const yearMs = int64(365 * 24 * 3600 * 1000)

func testParams() types.Params {
	p := types.DefaultParams()
	p.MinBaseFee = 0
	p.TargetBlockTxs = 0
	p.TokenCreateFee = 0
	p.ContractCreateFee = 0
	p.MinValidatorStake = 1_000
	p.MinDelegation = 1_000
	p.InflationRateBps = 0
	return p
}

func TestExecuteTransferAppliesBalancesNonceBurnAndTip(t *testing.T) {
	st := New()
	p := testParams()
	p.MinBaseFee = 100
	st.SetParams(p)

	const (
		alice    = "cgo1alice"
		bob      = "cgo1bob"
		proposer = "cgo1proposer"
	)
	st.Mint(alice, 10_000)

	tx := &types.Transaction{
		Type:       types.TxTransfer,
		From:       alice,
		To:         bob,
		TokenID:    types.NativeToken,
		Amount:     2_000,
		Nonce:      0,
		MaxBaseFee: 100,
		Tip:        25,
	}

	applied, failed, _, err := st.Execute([]*types.Transaction{tx}, proposer, 1_000, false)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(applied) != 1 || len(failed) != 0 {
		t.Fatalf("applied=%d failed=%d, want one applied and no failures", len(applied), len(failed))
	}

	if got := st.GetAccount(alice).Balances[types.NativeToken]; got != 7_875 {
		t.Fatalf("alice balance = %d, want 7875", got)
	}
	if got := st.GetAccount(bob).Balances[types.NativeToken]; got != 2_000 {
		t.Fatalf("bob balance = %d, want 2000", got)
	}
	if got := st.GetAccount(proposer).Balances[types.NativeToken]; got != 25 {
		t.Fatalf("proposer tip balance = %d, want 25", got)
	}
	if got := st.GetAccount(alice).Nonce; got != 1 {
		t.Fatalf("alice nonce = %d, want 1", got)
	}
	supply := st.GetSupply()
	if supply.Burned != 100 {
		t.Fatalf("burned supply = %d, want 100", supply.Burned)
	}
}

func TestDelegationRewardSplitUsesCommissionAndProRataShare(t *testing.T) {
	st := New()
	p := testParams()
	p.BlockIntervalMs = yearMs
	p.InflationRateBps = 10_000
	p.DelegationCommissionBps = 1_000
	st.SetParams(p)

	const (
		validator = "cgo1validator"
		delegator = "cgo1delegator"
	)
	st.BootstrapStake(validator, 1_000)
	st.Mint(delegator, 1_000)

	delegate := &types.Transaction{
		Type:       types.TxDelegate,
		From:       delegator,
		To:         validator,
		Amount:     1_000,
		Nonce:      0,
		MaxBaseFee: 0,
	}
	if _, _, _, err := st.Execute([]*types.Transaction{delegate}, "", 1_000, true); err != nil {
		t.Fatalf("delegate failed: %v", err)
	}

	if _, _, _, err := st.Execute(nil, validator, 2_000, true); err != nil {
		t.Fatalf("reward block failed: %v", err)
	}

	if got := st.GetAccount(delegator).Balances[types.NativeToken]; got != 900 {
		t.Fatalf("delegator reward = %d, want 900", got)
	}
	if got := st.GetAccount(validator).Balances[types.NativeToken]; got != 1_100 {
		t.Fatalf("validator reward = %d, want 1100", got)
	}
}

func TestVestingClaimUnlocksPartialThenTotalAmount(t *testing.T) {
	st := New()
	st.SetParams(testParams())

	const (
		creator     = "cgo1creator"
		beneficiary = "cgo1beneficiary"
	)
	st.Mint(creator, 1_000)

	create := &types.Transaction{
		Type:       types.TxContractCreate,
		From:       creator,
		Nonce:      0,
		MaxBaseFee: 0,
		Contract: &types.ContractParams{
			Template:    types.TemplateVesting,
			TokenID:     types.NativeToken,
			Amount:      100,
			Beneficiary: beneficiary,
			StartMs:     1_000,
			EndMs:       2_000,
		},
	}
	if _, _, _, err := st.Execute([]*types.Transaction{create}, "", 500, true); err != nil {
		t.Fatalf("contract create failed: %v", err)
	}

	claimHalf := &types.Transaction{
		Type:       types.TxContractExec,
		From:       beneficiary,
		Nonce:      0,
		MaxBaseFee: 0,
		ContractID: create.Hash(),
		Action:     types.ActionClaim,
	}
	if _, _, _, err := st.Execute([]*types.Transaction{claimHalf}, "", 1_500, true); err != nil {
		t.Fatalf("partial claim failed: %v", err)
	}
	if got := st.GetAccount(beneficiary).Balances[types.NativeToken]; got != 50 {
		t.Fatalf("partial vested balance = %d, want 50", got)
	}

	claimRest := &types.Transaction{
		Type:       types.TxContractExec,
		From:       beneficiary,
		Nonce:      1,
		MaxBaseFee: 0,
		ContractID: create.Hash(),
		Action:     types.ActionClaim,
	}
	if _, _, _, err := st.Execute([]*types.Transaction{claimRest}, "", 2_500, true); err != nil {
		t.Fatalf("final claim failed: %v", err)
	}
	if got := st.GetAccount(beneficiary).Balances[types.NativeToken]; got != 100 {
		t.Fatalf("fully vested balance = %d, want 100", got)
	}
	if got := st.GetContract(create.Hash()).Status; got != "completed" {
		t.Fatalf("contract status = %q, want completed", got)
	}
}

func TestEscrowReleaseRefundAndAccessGuards(t *testing.T) {
	t.Run("release by buyer pays seller", func(t *testing.T) {
		st := New()
		st.SetParams(testParams())

		const (
			buyer  = "cgo1buyer"
			seller = "cgo1seller"
		)
		st.Mint(buyer, 1_000)
		create := escrowCreateTx(buyer, seller, "", 0)
		if _, _, _, err := st.Execute([]*types.Transaction{create}, "", 1_000, true); err != nil {
			t.Fatalf("escrow create failed: %v", err)
		}
		release := &types.Transaction{
			Type:       types.TxContractExec,
			From:       buyer,
			Nonce:      1,
			MaxBaseFee: 0,
			ContractID: create.Hash(),
			Action:     types.ActionRelease,
		}
		if _, _, _, err := st.Execute([]*types.Transaction{release}, "", 2_000, true); err != nil {
			t.Fatalf("release failed: %v", err)
		}
		if got := st.GetAccount(seller).Balances[types.NativeToken]; got != 100 {
			t.Fatalf("seller balance = %d, want 100", got)
		}
	})

	t.Run("refund by seller pays buyer", func(t *testing.T) {
		st := New()
		st.SetParams(testParams())

		const (
			buyer  = "cgo1buyer"
			seller = "cgo1seller"
		)
		st.Mint(buyer, 1_000)
		create := escrowCreateTx(buyer, seller, "", 0)
		if _, _, _, err := st.Execute([]*types.Transaction{create}, "", 1_000, true); err != nil {
			t.Fatalf("escrow create failed: %v", err)
		}
		refund := &types.Transaction{
			Type:       types.TxContractExec,
			From:       seller,
			Nonce:      0,
			MaxBaseFee: 0,
			ContractID: create.Hash(),
			Action:     types.ActionRefund,
		}
		if _, _, _, err := st.Execute([]*types.Transaction{refund}, "", 2_000, true); err != nil {
			t.Fatalf("refund failed: %v", err)
		}
		if got := st.GetAccount(buyer).Balances[types.NativeToken]; got != 1_000 {
			t.Fatalf("buyer balance = %d, want 1000", got)
		}
		if got := st.GetContract(create.Hash()).Status; got != "refunded" {
			t.Fatalf("contract status = %q, want refunded", got)
		}
	})

	t.Run("stranger cannot release escrow", func(t *testing.T) {
		st := New()
		st.SetParams(testParams())

		const (
			buyer    = "cgo1buyer"
			seller   = "cgo1seller"
			stranger = "cgo1stranger"
		)
		st.Mint(buyer, 1_000)
		create := escrowCreateTx(buyer, seller, "", 0)
		if _, _, _, err := st.Execute([]*types.Transaction{create}, "", 1_000, true); err != nil {
			t.Fatalf("escrow create failed: %v", err)
		}
		release := &types.Transaction{
			Type:       types.TxContractExec,
			From:       stranger,
			Nonce:      0,
			MaxBaseFee: 0,
			ContractID: create.Hash(),
			Action:     types.ActionRelease,
		}
		if _, _, _, err := st.Execute([]*types.Transaction{release}, "", 2_000, true); err == nil {
			t.Fatal("expected stranger release to fail")
		}
	})
}

func escrowCreateTx(buyer, seller, arbiter string, nonce uint64) *types.Transaction {
	return &types.Transaction{
		Type:       types.TxContractCreate,
		From:       buyer,
		Nonce:      nonce,
		MaxBaseFee: 0,
		Contract: &types.ContractParams{
			Template: types.TemplateEscrow,
			TokenID:  types.NativeToken,
			Amount:   100,
			Seller:   seller,
			Arbiter:  arbiter,
		},
	}
}
