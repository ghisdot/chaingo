package consensus

import (
	"testing"

	"chaingo/internal/state"
	"chaingo/internal/types"
)

// TestPrevoteEquivocationRecorded (#8) : deux prevotes du même validateur au
// MÊME round pour des blocs différents produisent une preuve d'équivocation
// (alors qu'avant seuls les précommits étaient punis).
func TestPrevoteEquivocationRecorded(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	vs := mkValidators(st, 4)
	e := newEngine(st, nil)
	const h = uint64(3)
	e.freezeSetLocked(h)
	bad := vs[0]

	// Deux prevotes en conflit au round 0 → équivocation enregistrée.
	addPrevote(e, bad, h, 0, "A")
	addPrevote(e, bad, h, 0, "B")
	if len(e.evidence) != 1 {
		t.Fatalf("une preuve d'équivocation de prevote attendue, got %d", len(e.evidence))
	}
	ev := e.evidence[evidenceKey(bad.Address(), h)]
	if ev == nil || ev.VoteA.Kind != types.PrevoteKind || ev.VoteB.Kind != types.PrevoteKind {
		t.Fatalf("la preuve doit porter deux prevotes, got %+v", ev)
	}
	if err := ev.Verify("test"); err != nil {
		t.Fatalf("la preuve de prevote équivoque doit être valide : %v", err)
	}
}

// TestPrevoteEquivocationSlashes (#8) : la preuve de prevote équivoque, passée
// à state.Execute (comme dans un bloc), réduit bien le stake du fautif —
// le chemin de slash est kind-agnostique.
func TestPrevoteEquivocationSlashes(t *testing.T) {
	st := state.New()
	p := types.DefaultParams()
	st.SetParams(p)
	vs := mkValidators(st, 4)
	bad := vs[0]
	stakeBefore := st.PowerOf(bad.Address())

	mkPrevote := func(hash string) *types.Vote {
		v := &types.Vote{ChainID: "test", Height: 3, Round: 0, Kind: types.PrevoteKind, BlockHash: hash}
		v.SignWith(bad)
		return v
	}
	ev := &types.DoubleSignEvidence{Height: 3, Voter: bad.Address(), VoteA: mkPrevote("A"), VoteB: mkPrevote("B")}
	if err := ev.Verify("test"); err != nil {
		t.Fatalf("preuve invalide : %v", err)
	}
	if _, _, _, err := st.Execute(nil, []*types.DoubleSignEvidence{ev}, nil, "", 1000, true); err != nil {
		t.Fatalf("Execute avec preuve de prevote : %v", err)
	}
	stakeAfter := st.PowerOf(bad.Address())
	if stakeAfter >= stakeBefore {
		t.Fatalf("le stake du fautif doit baisser (avant %d, après %d)", stakeBefore, stakeAfter)
	}
}
