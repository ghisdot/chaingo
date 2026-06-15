package consensus

import (
	"testing"

	"chaingo/internal/state"
	"chaingo/internal/types"
)

// addPrecommitRound : émet un précommit signé à un round donné via AddVote.
func addPrecommitRound(e *Engine, kp *keyPair, h uint64, round uint32, hash string) (bool, error) {
	v := &types.Vote{ChainID: "test", Height: h, Round: round, Kind: types.PrecommitKind, BlockHash: hash}
	v.SignWith(kp)
	return e.AddVote(v)
}

// TestEquivocationIsRoundAware : le pool ne signale une équivocation (et ne
// génère donc une preuve de slash) que pour deux précommits du MÊME validateur
// au MÊME round sur des blocs différents. Un changement cross-round est
// légitime (POL) et ne doit PAS produire de preuve.
func TestEquivocationIsRoundAware(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	vs := mkValidators(st, 4)
	e := newEngine(st, nil)
	const h = uint64(2)
	e.freezeSetLocked(h)
	voter := vs[0]

	// Précommit au round 0 pour A, puis au round 1 pour B → cross-round,
	// légitime : AUCUNE preuve d'équivocation ne doit être enregistrée.
	addPrecommitRound(e, voter, h, 0, "A")
	addPrecommitRound(e, voter, h, 1, "B")
	if len(e.evidence) != 0 {
		t.Fatalf("changement cross-round ne doit PAS produire de preuve, got %d", len(e.evidence))
	}

	// Deux précommits au MÊME round (0) pour des blocs différents → vraie
	// équivocation : une preuve est enregistrée.
	addPrecommitRound(e, voter, h, 0, "C") // conflit avec A au round 0
	if len(e.evidence) == 0 {
		t.Fatal("un conflit au même round doit produire une preuve d'équivocation")
	}
}
