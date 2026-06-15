package consensus

import (
	"testing"
	"time"

	"chaingo/internal/crypto"
	"chaingo/internal/mempool"
	"chaingo/internal/state"
	"chaingo/internal/types"
)

// TestNoSelfEquivocation : un validateur ne signe JAMAIS deux votes du même
// kind à la même hauteur. Sinon il produit sa propre preuve d'équivocation et
// se fait slasher. castVoteKind l'enforce, par kind.
func TestNoSelfEquivocation(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	k, _ := crypto.GenerateKeyPair()
	st.BootstrapStake(k.Address(), 1_000_000*types.Unit)
	e := New(st, mempool.New(10), nil, k, time.Hour, 10)
	e.SetChainID("test")

	var emitted []*types.Vote
	e.OnVote = func(v *types.Vote) { emitted = append(emitted, v) }

	// castVote émet désormais prevote + précommit pour la hauteur 5.
	e.castVote(5, "HASH_A")
	if len(emitted) != 2 {
		t.Fatalf("un prevote + un précommit attendus à la hauteur 5, %d émis", len(emitted))
	}
	if emitted[0].Kind != types.PrevoteKind || emitted[1].Kind != types.PrecommitKind {
		t.Fatalf("ordre attendu prevote puis précommit, got %s puis %s", emitted[0].Kind, emitted[1].Kind)
	}

	// Re-vote à la même hauteur sur un AUTRE hash → REFUSÉ (anti-auto-équivocation),
	// pour les DEUX kinds. Aucun vote supplémentaire émis.
	e.castVote(5, "HASH_B")
	if len(emitted) != 2 {
		t.Fatalf("re-vote conflictuel à la hauteur 5 ne doit RIEN émettre de plus, got %d", len(emitted))
	}

	// Idempotent (même hash) : pas de nouvelle émission non plus.
	e.castVote(5, "HASH_A")
	if len(emitted) != 2 {
		t.Fatalf("idempotence : pas d'émission attendue, got %d", len(emitted))
	}

	// Une nouvelle hauteur reste votable → 2 votes de plus.
	e.castVote(6, "HASH_C")
	if len(emitted) != 4 {
		t.Fatalf("nouvelle hauteur : 2 votes attendus de plus, got %d (total)", len(emitted))
	}
}
