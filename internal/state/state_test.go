package state

import (
	"testing"

	"chaingo/internal/crypto"
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

func mustKey(t *testing.T) *crypto.KeyPair {
	t.Helper()
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	return kp
}

func executeStateBlock(t *testing.T, st *State, proposer string, blockTime int64, txs ...*types.Transaction) {
	t.Helper()
	if _, _, _, err := st.Execute(txs, nil, nil, proposer, blockTime, true); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestDelegationRewardSplitUsesCommissionAndProRataShare(t *testing.T) {
	st := New()
	p := testParams()
	p.BlockIntervalMs = yearMs
	p.InflationRateBps = 10_000
	p.DelegationCommissionBps = 1_000
	st.SetParams(p)

	validator := mustKey(t)
	delegator := mustKey(t)
	st.BootstrapStake(validator.Address(), 1_000)
	st.Mint(delegator.Address(), 1_000)

	delegate := &types.Transaction{
		Type:       types.TxDelegate,
		To:         validator.Address(),
		Amount:     1_000,
		MaxBaseFee: 1,
	}
	delegate.SignWith(delegator)
	executeStateBlock(t, st, "", 1_000, delegate)

	executeStateBlock(t, st, validator.Address(), 2_000)

	if got := st.GetAccount(delegator.Address()).Balances[types.NativeToken]; got != 900 {
		t.Fatalf("delegator reward = %d, want 900", got)
	}
	if got := st.GetAccount(validator.Address()).Balances[types.NativeToken]; got != 1_100 {
		t.Fatalf("validator reward = %d, want 1100", got)
	}
	if got := st.Validators[validator.Address()].RewardsEarned; got != 1_100 {
		t.Fatalf("validator rewards earned = %d, want 1100", got)
	}
}

func TestEscrowReleaseRefundAndAccessGuards(t *testing.T) {
	t.Run("release by buyer pays seller", func(t *testing.T) {
		st := New()
		st.SetParams(testParams())

		buyer := mustKey(t)
		seller := mustKey(t)
		st.Mint(buyer.Address(), 100)

		create := escrowCreateTx(buyer, seller.Address(), "", 0)
		executeStateBlock(t, st, "", 1_000, create)

		release := &types.Transaction{
			Type:       types.TxContractExec,
			Nonce:      1,
			MaxBaseFee: 1,
			ContractID: create.Hash(),
			Action:     types.ActionRelease,
		}
		release.SignWith(buyer)
		executeStateBlock(t, st, "", 2_000, release)

		if got := st.GetAccount(seller.Address()).Balances[types.NativeToken]; got != 100 {
			t.Fatalf("seller balance = %d, want 100", got)
		}
		if got := st.GetContract(create.Hash()).Status; got != "completed" {
			t.Fatalf("contract status = %q, want completed", got)
		}
	})

	t.Run("refund by seller pays buyer", func(t *testing.T) {
		st := New()
		st.SetParams(testParams())

		buyer := mustKey(t)
		seller := mustKey(t)
		st.Mint(buyer.Address(), 100)

		create := escrowCreateTx(buyer, seller.Address(), "", 0)
		executeStateBlock(t, st, "", 1_000, create)

		refund := &types.Transaction{
			Type:       types.TxContractExec,
			MaxBaseFee: 1,
			ContractID: create.Hash(),
			Action:     types.ActionRefund,
		}
		refund.SignWith(seller)
		executeStateBlock(t, st, "", 2_000, refund)

		if got := st.GetAccount(buyer.Address()).Balances[types.NativeToken]; got != 100 {
			t.Fatalf("buyer balance = %d, want 100", got)
		}
		if got := st.GetContract(create.Hash()).Status; got != "refunded" {
			t.Fatalf("contract status = %q, want refunded", got)
		}
	})

	t.Run("stranger cannot release or refund escrow", func(t *testing.T) {
		for _, action := range []string{types.ActionRelease, types.ActionRefund} {
			t.Run(action, func(t *testing.T) {
				st := New()
				st.SetParams(testParams())

				buyer := mustKey(t)
				seller := mustKey(t)
				stranger := mustKey(t)
				st.Mint(buyer.Address(), 100)

				create := escrowCreateTx(buyer, seller.Address(), "", 0)
				executeStateBlock(t, st, "", 1_000, create)

				tx := &types.Transaction{
					Type:       types.TxContractExec,
					MaxBaseFee: 1,
					ContractID: create.Hash(),
					Action:     action,
				}
				tx.SignWith(stranger)
				if _, _, _, err := st.Execute([]*types.Transaction{tx}, nil, nil, "", 2_000, true); err == nil {
					t.Fatalf("expected stranger %s to fail", action)
				}
			})
		}
	})
}

func escrowCreateTx(buyer *crypto.KeyPair, seller, arbiter string, nonce uint64) *types.Transaction {
	tx := &types.Transaction{
		Type:       types.TxContractCreate,
		Nonce:      nonce,
		MaxBaseFee: 1,
		Contract: &types.ContractParams{
			Template: types.TemplateEscrow,
			TokenID:  types.NativeToken,
			Amount:   100,
			Seller:   seller,
			Arbiter:  arbiter,
		},
	}
	tx.SignWith(buyer)
	return tx
}
