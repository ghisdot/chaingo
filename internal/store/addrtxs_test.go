package store

import (
	"os"
	"path/filepath"
	"testing"

	"chaingo/internal/types"
)

func newStore(t *testing.T) (*Store, func()) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "chain.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s, func() { s.Close(); os.RemoveAll(dir) }
}

// TestBlockByHash : un bloc sauvé est retrouvable par son hash.
func TestBlockByHash(t *testing.T) {
	st, done := newStore(t)
	defer done()

	b := &types.Block{Header: types.BlockHeader{Height: 42}, Hash: "abc123"}
	if err := st.SaveBlock(b); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := st.BlockByHash("abc123")
	if err != nil || got == nil {
		t.Fatalf("BlockByHash: got=%v err=%v", got, err)
	}
	if got.Header.Height != 42 {
		t.Fatalf("hauteur attendue 42, got %d", got.Header.Height)
	}
	if got2, _ := st.BlockByHash("inconnu"); got2 != nil {
		t.Fatal("BlockByHash sur un hash inconnu devrait renvoyer nil")
	}
}

// TestAddressTxsOrderedAndPaginated : l'index renvoie les tx d'une adresse
// dans l'ordre récent → ancien, et la pagination via `before` fonctionne.
func TestAddressTxsOrderedAndPaginated(t *testing.T) {
	st, done := newStore(t)
	defer done()

	const alice = "cg11111111111111111111111111111111111111aa"
	const bob = "cg22222222222222222222222222222222222222bb"

	// 5 blocs, chacun avec 1 tx alice→bob (hashes uniques par memo distinct).
	for h := uint64(1); h <= 5; h++ {
		tx := &types.Transaction{
			Type: types.TxTransfer, From: alice, To: bob, Amount: h,
			Nonce: h - 1, Memo: string(rune('A' + h)),
		}
		b := &types.Block{
			Header: types.BlockHeader{Height: h},
			Hash:   "blk-" + tx.Memo,
			Txs:    []*types.Transaction{tx},
		}
		if err := st.SaveBlock(b); err != nil {
			t.Fatalf("save block %d: %v", h, err)
		}
	}

	// Alice (from) → 5 tx, ordre descendant.
	got, err := st.AddressTxs(alice, 10, 0)
	if err != nil {
		t.Fatalf("AddressTxs: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("attendu 5 tx pour alice, got %d", len(got))
	}
	for i := 0; i < len(got); i++ {
		wantH := uint64(5 - i)
		if got[i].Height != wantH {
			t.Fatalf("position %d : height %d, attendu %d", i, got[i].Height, wantH)
		}
	}

	// Bob (to) → 5 tx aussi (l'index suit les deux côtés).
	got, _ = st.AddressTxs(bob, 10, 0)
	if len(got) != 5 {
		t.Fatalf("bob doit aussi être indexé (to), got %d tx", len(got))
	}

	// Pagination : limit=2 + before=3 → veut les tx strictement < hauteur 3 → [2, 1].
	got, _ = st.AddressTxs(alice, 2, 3)
	if len(got) != 2 || got[0].Height != 2 || got[1].Height != 1 {
		t.Fatalf("pagination cassée, got %+v", got)
	}

	// Une adresse jamais vue → liste vide.
	got, _ = st.AddressTxs("cg99999999999999999999999999999999999999zz", 10, 0)
	if len(got) != 0 {
		t.Fatalf("adresse inconnue doit renvoyer 0 tx, got %d", len(got))
	}
}
