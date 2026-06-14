package consensus

import (
	"testing"
	"time"

	"chaingo/internal/crypto"
	"chaingo/internal/mempool"
	"chaingo/internal/state"
	"chaingo/internal/types"
)

// TestQuorumThreshold vérifie la règle BFT stricte > 2/3.
func TestQuorumThreshold(t *testing.T) {
	cases := []struct {
		power, total uint64
		want         bool
	}{
		{0, 0, false},
		{2, 3, false}, // exactement 2/3 → insuffisant
		{3, 4, true},  // 3/4 = 75 % > 2/3
		{2, 4, false}, // 2/4 = 50 %
		{67, 100, true},
		{66, 100, false},
	}
	for _, c := range cases {
		if got := hasQuorum(c.power, c.total); got != c.want {
			t.Errorf("hasQuorum(%d,%d)=%v, want %v", c.power, c.total, got, c.want)
		}
	}
}

// TestVoteSignVerify : aller-retour de signature ML-DSA-65 + détection
// d'altération.
func TestVoteSignVerify(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	v := &types.Vote{ChainID: "test", Height: 7, BlockHash: "abc"}
	v.SignWith(kp)
	if err := v.Verify(); err != nil {
		t.Fatalf("vote valide rejeté: %v", err)
	}
	v.BlockHash = "def" // altération
	if v.Verify() == nil {
		t.Fatal("vote altéré accepté")
	}
}

func mkValidators(st *state.State, n int) []*crypto.KeyPair {
	ks := make([]*crypto.KeyPair, n)
	for i := range ks {
		ks[i], _ = crypto.GenerateKeyPair()
		st.BootstrapStake(ks[i].Address(), 1_000_000*types.Unit)
	}
	return ks
}

func voteFrom(e *Engine, kp *crypto.KeyPair, height uint64, hash string) (bool, error) {
	v := &types.Vote{ChainID: "test", Height: height, BlockHash: hash}
	v.SignWith(kp)
	return e.AddVote(v)
}

// TestFinalityNeedsSupermajority : avec 4 validateurs égaux, la finalité
// n'avance qu'à partir de 3 précommits (3/4 > 2/3), pas à 2/4.
func TestFinalityNeedsSupermajority(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	vs := mkValidators(st, 4)
	e := New(st, mempool.New(10), nil, vs[0], time.Hour, 10)
	e.SetChainID("test")

	// Bloc local committé à la hauteur 1.
	st.Commit(1, "HASH1")

	// 2 précommits (2/4 = 50 %) → pas de finalité.
	for i := 0; i < 2; i++ {
		if ok, err := voteFrom(e, vs[i], 1, "HASH1"); !ok || err != nil {
			t.Fatalf("vote %d refusé: ok=%v err=%v", i, ok, err)
		}
	}
	if e.FinalizedHeight() != 0 {
		t.Fatalf("finalité à 2/4 ne devrait pas avancer, got %d", e.FinalizedHeight())
	}

	// 3e précommit (3/4 = 75 %) → finalité hauteur 1.
	if _, err := voteFrom(e, vs[2], 1, "HASH1"); err != nil {
		t.Fatalf("3e vote refusé: %v", err)
	}
	if e.FinalizedHeight() != 1 {
		t.Fatalf("finalité devrait atteindre 1 à 3/4, got %d", e.FinalizedHeight())
	}
}

// TestVoteRulesRejectAndDedup : doublons, non-validateurs et hash divergent.
func TestVoteRulesRejectAndDedup(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	vs := mkValidators(st, 4)
	e := New(st, mempool.New(10), nil, vs[0], time.Hour, 10)
	e.SetChainID("test")
	st.Commit(1, "HASH1")

	// Doublon : le même vote compté une seule fois.
	voteFrom(e, vs[0], 1, "HASH1")
	if ok, _ := voteFrom(e, vs[0], 1, "HASH1"); ok {
		t.Fatal("doublon de vote accepté comme nouveau")
	}

	// Non-validateur : rejeté.
	stranger, _ := crypto.GenerateKeyPair()
	if _, err := voteFrom(e, stranger, 1, "HASH1"); err == nil {
		t.Fatal("vote d'un non-validateur accepté")
	}

	// Quorum sur un AUTRE hash (fork) ne finalise pas notre bloc local.
	for i := 0; i < 4; i++ {
		voteFrom(e, vs[i], 1, "FORKHASH")
	}
	if e.FinalizedHeight() != 0 {
		t.Fatalf("un quorum sur un hash divergent ne doit pas finaliser notre chaîne, got %d", e.FinalizedHeight())
	}
}
