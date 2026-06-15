package consensus

import (
	"testing"
	"time"

	"chaingo/internal/crypto"
	"chaingo/internal/mempool"
	"chaingo/internal/state"
	"chaingo/internal/types"
)

// TestNoSelfEquivocation : un validateur ne doit JAMAIS émettre deux
// précommits à la même hauteur (sinon il produit sa propre preuve
// d'équivocation et se fait slasher). castVote l'enforce.
func TestNoSelfEquivocation(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	k, _ := crypto.GenerateKeyPair()
	st.BootstrapStake(k.Address(), 1_000_000*types.Unit)
	e := New(st, mempool.New(10), nil, k, time.Hour, 10)
	e.SetChainID("test")

	var emitted []*types.Vote
	e.OnVote = func(v *types.Vote) { emitted = append(emitted, v) }

	e.castVote(5, "HASH_A")
	e.castVote(5, "HASH_B") // même hauteur, autre hash → REFUSÉ (anti-auto-équivocation)
	e.castVote(5, "HASH_A") // idempotent → pas de nouvelle émission

	if len(emitted) != 1 {
		t.Fatalf("un seul précommit attendu à la hauteur 5, %d émis", len(emitted))
	}
	if emitted[0].BlockHash != "HASH_A" {
		t.Fatalf("le précommit doit porter le 1er hash voté, got %q", emitted[0].BlockHash)
	}

	// Une nouvelle hauteur reste votable.
	e.castVote(6, "HASH_C")
	if len(emitted) != 2 {
		t.Fatal("un précommit à une hauteur jamais votée devrait être émis")
	}
}
