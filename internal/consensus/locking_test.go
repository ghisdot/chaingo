package consensus

import (
	"testing"

	"chaingo/internal/state"
	"chaingo/internal/types"
)

// TestLockingRule : un nœud verrouillé sur un bloc à une hauteur ne précommet
// un bloc concurrent QUE sur preuve d'une polka à un round strictement
// supérieur (POL #6 tranche 3). Sinon il reste fidèle à son verrou.
func TestLockingRule(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	vs := mkValidators(st, 4) // 4 × 1M, ce nœud = vs[0]
	e := newEngine(st, vs[0])
	const h = uint64(2)
	e.freezeSetLocked(h)

	var precommits []*types.Vote
	e.OnVote = func(v *types.Vote) {
		if v.Kind == types.PrecommitKind {
			precommits = append(precommits, v)
		}
	}

	// 1. Premier précommit (round 0, A) → émis, verrou (0, A).
	e.castVoteKind(h, 0, types.PrecommitKind, "A")
	if len(precommits) != 1 || precommits[0].BlockHash != "A" {
		t.Fatalf("précommit A attendu, got %v", precommits)
	}
	if e.locked[h].hash != "A" || e.locked[h].round != 0 {
		t.Fatalf("verrou attendu (0,A), got %+v", e.locked[h])
	}

	// 2. Même round, hash B → refusé (auto-équivocation au même round).
	e.castVoteKind(h, 0, types.PrecommitKind, "B")
	if len(precommits) != 1 {
		t.Fatal("aucun 2e précommit ne doit être signé au même round")
	}

	// 3. Round supérieur (1) pour B mais SANS polka → refusé (reste verrouillé).
	e.castVoteKind(h, 1, types.PrecommitKind, "B")
	if len(precommits) != 1 {
		t.Fatal("verrouillé sur A : doit refuser B sans polka de round supérieur")
	}
	if e.locked[h].hash != "A" {
		t.Fatal("le verrou ne doit pas changer sans polka")
	}

	// 4. Crée une polka pour B au round 1 (3/4 prevotes), puis re-précommet.
	for i := 1; i <= 3; i++ {
		addPrevote(e, vs[i], h, 1, "B")
	}
	if !e.hasPolka(h, 1, "B") {
		t.Fatal("polka round 1 pour B attendue (3/4)")
	}
	e.castVoteKind(h, 1, types.PrecommitKind, "B")
	if len(precommits) != 2 || precommits[1].BlockHash != "B" {
		t.Fatalf("doit précommettre B sur polka de round 1, got %v", precommits)
	}
	if e.locked[h].hash != "B" || e.locked[h].round != 1 {
		t.Fatalf("le verrou doit passer à (1,B), got %+v", e.locked[h])
	}
}

// TestPrevoteTheLock : « prevote-the-lock » (Tendermint). Un nœud verrouillé sur
// un bloc ne PREVOTE un bloc concurrent QUE sur preuve d'une polka à un round
// strictement supérieur — sinon il reste fidèle à son verrou et ne contribue pas
// à former une polka adverse non fondée.
func TestPrevoteTheLock(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	vs := mkValidators(st, 4) // 4 × 1M, ce nœud = vs[0]
	e := newEngine(st, vs[0])
	const h = uint64(2)
	e.freezeSetLocked(h)

	var prevotesB []*types.Vote
	e.OnVote = func(v *types.Vote) {
		if v.Kind == types.PrevoteKind && v.BlockHash == "B" {
			prevotesB = append(prevotesB, v)
		}
	}

	// Verrou sur A au round 0 (via précommit).
	e.castVoteKind(h, 0, types.PrecommitKind, "A")
	if e.locked[h].hash != "A" {
		t.Fatalf("verrou (0,A) attendu, got %+v", e.locked[h])
	}

	// 1. Prevote B au MÊME round (0) → refusé (verrouillé sur A, pas de polka sup.).
	e.castVoteKind(h, 0, types.PrevoteKind, "B")
	if len(prevotesB) != 0 {
		t.Fatal("verrouillé sur A : ne doit pas prevoter B au même round")
	}

	// 2. Prevote B au round supérieur (1) SANS polka → refusé.
	e.castVoteKind(h, 1, types.PrevoteKind, "B")
	if len(prevotesB) != 0 {
		t.Fatal("ne doit pas prevoter B au round 1 sans polka de round supérieur")
	}

	// 3. Polka pour B au round 1 (3/4 prevotes), puis prevote B au round 1 → OK.
	for i := 1; i <= 3; i++ {
		addPrevote(e, vs[i], h, 1, "B")
	}
	if !e.hasPolka(h, 1, "B") {
		t.Fatal("polka round 1 pour B attendue (3/4)")
	}
	e.castVoteKind(h, 1, types.PrevoteKind, "B")
	if len(prevotesB) != 1 {
		t.Fatalf("doit prevoter B sur polka de round supérieur, got %d", len(prevotesB))
	}

	// Le prevote ne pose PAS de verrou : le nœud reste verrouillé sur A (seul le
	// précommit verrouille).
	if e.locked[h].hash != "A" {
		t.Fatalf("un prevote ne doit pas changer le verrou (reste A), got %+v", e.locked[h])
	}
}

// TestLockingDefaultPathUnchanged : au premier vote d'une hauteur (cas nominal
// sans reorg), le comportement est identique à l'historique — un prevote + un
// précommit émis, verrou posé, pas de blocage.
func TestLockingDefaultPathUnchanged(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	vs := mkValidators(st, 3)
	e := newEngine(st, vs[0])
	const h = uint64(1)
	e.freezeSetLocked(h)

	var all []*types.Vote
	e.OnVote = func(v *types.Vote) { all = append(all, v) }

	e.castVote(h, 0, "BLOCK1") // prevote + précommit
	if len(all) != 2 {
		t.Fatalf("prevote + précommit attendus, got %d", len(all))
	}
	if e.locked[h].hash != "BLOCK1" {
		t.Fatal("le précommit doit poser le verrou sur BLOCK1")
	}
	// Re-voter le même bloc : idempotent, rien de plus.
	e.castVote(h, 0, "BLOCK1")
	if len(all) != 2 {
		t.Fatal("re-vote idempotent : aucune émission supplémentaire")
	}
}
