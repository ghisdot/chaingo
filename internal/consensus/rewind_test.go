package consensus

import (
	"testing"

	"chaingo/internal/state"
	"chaingo/internal/types"
)

// TestRewindRestoresState (#7 fondation) : un snapshot pris à une hauteur permet
// de restaurer EXACTEMENT l'état à cette hauteur après des mutations ultérieures
// — la brique de bas niveau du reorg.
func TestRewindRestoresState(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	vs := mkValidators(st, 2)
	e := newEngine(st, nil)

	// Simule l'état "après hauteur 5" puis snapshot.
	st.BootstrapStake(vs[0].Address(), 500*types.Unit)
	e.snapshotStateLocked(5)
	rootAt5 := st.Root()

	// Mute l'état comme si on appliquait des blocs 6, 7 (branche A).
	st.BootstrapStake(vs[1].Address(), 999*types.Unit)
	e.snapshotStateLocked(6)
	if st.Root() == rootAt5 {
		t.Fatal("préalable : l'état doit avoir changé après la hauteur 5")
	}

	// Rewind vers la hauteur 5 → l'état doit redevenir EXACTEMENT celui de 5.
	if err := e.RewindTo(5); err != nil {
		t.Fatalf("RewindTo(5): %v", err)
	}
	if st.Root() != rootAt5 {
		t.Fatal("après rewind, la racine doit être identique à celle de la hauteur 5")
	}
	// Le snapshot de la hauteur 6 (branche abandonnée) doit avoir été oublié.
	if _, ok := e.snapshots[6]; ok {
		t.Fatal("le snapshot de la hauteur 6 (au-dessus du point de rewind) doit être purgé")
	}
}

// TestRewindRefusesBelowFinalized (#7) : on ne rembobine JAMAIS sous la dernière
// hauteur finalisée (immuable).
func TestRewindRefusesBelowFinalized(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	mkValidators(st, 2)
	e := newEngine(st, nil)
	e.snapshotStateLocked(3)
	st.SetFinalized(4) // 4 est finalisée

	if err := e.RewindTo(3); err == nil {
		t.Fatal("rewind sous la finalité (3 < 4) doit être refusé")
	}
}

// TestRewindNeedsSnapshot (#7) : rewind échoue proprement si la hauteur n'a pas
// de snapshot (trop ancienne / purgée).
func TestRewindNeedsSnapshot(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	mkValidators(st, 2)
	e := newEngine(st, nil)
	if err := e.RewindTo(9); err == nil {
		t.Fatal("rewind sans snapshot doit échouer")
	}
}

// TestPruneSnapshotsBelow (#7) : la purge garde la fenêtre non finalisée et
// oublie les points de restauration devenus inutiles.
func TestPruneSnapshotsBelow(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	mkValidators(st, 2)
	e := newEngine(st, nil)
	for h := uint64(1); h <= 6; h++ {
		e.snapshotStateLocked(h)
	}
	e.pruneSnapshotsBelow(5) // finalité 5 → garde {5,6}
	if _, ok := e.snapshots[4]; ok {
		t.Fatal("le snapshot 4 (< finalité) doit être purgé")
	}
	if _, ok := e.snapshots[5]; !ok {
		t.Fatal("le snapshot 5 (= point de fork minimal) doit être conservé")
	}
	if _, ok := e.snapshots[6]; !ok {
		t.Fatal("le snapshot 6 (non finalisé) doit être conservé")
	}
}
