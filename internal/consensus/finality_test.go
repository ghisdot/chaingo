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

// TestVoteSignVerify : aller-retour ML-DSA-65 + détection d'altération.
func TestVoteSignVerify(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	v := &types.Vote{ChainID: "test", Height: 7, BlockHash: "abc"}
	v.SignWith(kp)
	if err := v.Verify(); err != nil {
		t.Fatalf("vote valide rejeté: %v", err)
	}
	v.BlockHash = "def"
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

func addVote(e *Engine, kp *crypto.KeyPair, h uint64, hash string) {
	v := &types.Vote{ChainID: "test", Height: h, BlockHash: hash}
	v.SignWith(kp)
	e.AddVote(v)
}

func newEngine(st *state.State, key *crypto.KeyPair) *Engine {
	e := New(st, mempool.New(10), nil, key, time.Hour, 10)
	e.SetChainID("test")
	return e
}

// TestCommitNeedsSupermajority : un commit ne se forme (et ne finalisera donc
// le parent) qu'à partir de > 2/3 du stake actif.
func TestCommitNeedsSupermajority(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	vs := mkValidators(st, 4)
	e := newEngine(st, vs[0])
	const h, hash = uint64(1), "HASH1"

	for i := 0; i < 2; i++ { // 2/4 = 50 %
		addVote(e, vs[i], h, hash)
	}
	if c := e.buildLastCommit(h, hash); c != nil {
		t.Fatalf("2/4 ne doit pas former de commit, got %d votes", len(c))
	}

	addVote(e, vs[2], h, hash) // 3/4 = 75 %
	c := e.buildLastCommit(h, hash)
	if len(c) != 3 {
		t.Fatalf("commit attendu (3 votes), got %v", c)
	}
	power, err := e.verifyCommit(c, h, hash)
	if err != nil || !hasQuorum(power, st.TotalPower()) {
		t.Fatalf("le commit 3/4 devrait être valide et atteindre le quorum (err=%v)", err)
	}
}

// TestCommitRejectsBadVotes : doublon de votant, hash divergent, non-validateur.
func TestCommitRejectsBadVotes(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	vs := mkValidators(st, 4)
	e := newEngine(st, vs[0])
	const h, hash = uint64(1), "HASH1"

	v0 := &types.Vote{ChainID: "test", Height: h, BlockHash: hash}
	v0.SignWith(vs[0])
	// Doublon du même votant dans un commit → rejet.
	if _, err := e.verifyCommit([]*types.Vote{v0, v0}, h, hash); err == nil {
		t.Fatal("doublon de votant dans le commit devrait être rejeté")
	}
	// Hash divergent → rejet.
	if _, err := e.verifyCommit([]*types.Vote{v0}, h, "AUTRE"); err == nil {
		t.Fatal("commit sur un hash divergent devrait être rejeté")
	}
	// Non-validateur → pouvoir 0 → rejet.
	stranger, _ := crypto.GenerateKeyPair()
	vs1 := &types.Vote{ChainID: "test", Height: h, BlockHash: hash}
	vs1.SignWith(stranger)
	if _, err := e.verifyCommit([]*types.Vote{vs1}, h, hash); err == nil {
		t.Fatal("vote d'un non-validateur devrait être rejeté")
	}

	// Quorum sur un AUTRE hash ne forme pas de commit pour NOTRE hash.
	for i := 0; i < 4; i++ {
		addVote(e, vs[i], h, "FORK")
	}
	if c := e.buildLastCommit(h, hash); c != nil {
		t.Fatal("un quorum sur un hash divergent ne doit pas former le commit de notre bloc")
	}
}
