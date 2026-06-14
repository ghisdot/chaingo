package state

import (
	"testing"

	"chaingo/internal/types"
)

// TestFinalizedPersistsAndOutOfRoot : la hauteur finalisée est monotone,
// survit à un Restore (persistance), mais n'entre PAS dans la racine d'état.
func TestFinalizedPersistsAndOutOfRoot(t *testing.T) {
	st := New()
	st.SetParams(types.DefaultParams())

	st.SetFinalized(42)
	st.SetFinalized(10) // monotone : ne recule pas
	if st.GetFinalized() != 42 {
		t.Fatalf("finalized = %d, want 42 (monotone)", st.GetFinalized())
	}

	// Persistance : Restore conserve la hauteur finalisée.
	st2 := New()
	if err := st2.Restore(st.Bytes()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if st2.GetFinalized() != 42 {
		t.Fatalf("finalized non persisté après Restore: %d", st2.GetFinalized())
	}

	// Hors racine d'état : deux états identiques sauf FinalizedHeight ont la
	// même racine (sinon le consensus divergerait sur une donnée vérifiable
	// par ailleurs via les blocs).
	a := New()
	a.SetParams(types.DefaultParams())
	b := New()
	b.SetParams(types.DefaultParams())
	b.SetFinalized(99)
	if a.Root() != b.Root() {
		t.Fatal("FinalizedHeight ne doit pas modifier la racine d'état")
	}
}
