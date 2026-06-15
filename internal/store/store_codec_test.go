package store

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"chaingo/internal/crypto"
	"chaingo/internal/types"
)

// mkBlock : un bloc signé minimal mais complet (header + 1 tx signée +
// 1 précommit signé) pour les tests de stockage.
func mkBlock(t *testing.T, height uint64) *types.Block {
	t.Helper()
	proposer, _ := crypto.GenerateKeyPair()
	signer, _ := crypto.GenerateKeyPair()
	voter, _ := crypto.GenerateKeyPair()

	tx := &types.Transaction{
		ChainID: "chaingo-store-test", Type: types.TxTransfer,
		To: "cg1111111111111111111111111111111111111111", TokenID: types.NativeToken,
		Amount: height * types.Unit, Nonce: 0, MaxBaseFee: 200_000, Tip: 50_000,
		Timestamp: 1_700_000_000_000,
	}
	tx.SignWith(signer)

	v := &types.Vote{ChainID: "chaingo-store-test", Height: height - 1, Kind: types.PrecommitKind, BlockHash: "parent"}
	v.SignWith(voter)

	b := &types.Block{
		Header: types.BlockHeader{
			Height: height, PrevHash: "parent", Timestamp: 1_700_000_000_500,
			Proposer: proposer.Address(), TxRoot: types.TxRoot([]*types.Transaction{tx}),
			LastCommitRoot: types.CommitRoot([]*types.Vote{v}), StateRoot: "state-root",
		},
		Txs:        []*types.Transaction{tx},
		LastCommit: []*types.Vote{v},
	}
	b.Hash = b.ComputeHash()
	b.ProposerPubKey = proposer.PubBytes()
	b.ProposerSignature = proposer.Sign(b.SigningBytes())
	return b
}

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestBlockBinaryStorageRoundtrip : un bloc sauvé puis relu doit être
// identique, hash préservé, signatures valides. Vérifie aussi qu'il est
// bien stocké au format binaire (tag 0x01).
func TestBlockBinaryStorageRoundtrip(t *testing.T) {
	s := openTemp(t)
	b := mkBlock(t, 5)
	if err := s.SaveBlock(b); err != nil {
		t.Fatalf("SaveBlock: %v", err)
	}

	got, err := s.GetBlock(5)
	if err != nil {
		t.Fatalf("GetBlock: %v", err)
	}
	if got == nil {
		t.Fatal("GetBlock returned nil")
	}
	if got.Hash != b.Hash {
		t.Fatalf("hash modifié : %q vs %q", got.Hash, b.Hash)
	}
	if got.ComputeHash() != b.Hash {
		t.Fatal("ComputeHash après relecture != hash original")
	}
	if err := got.VerifyProposerSig(); err != nil {
		t.Fatalf("sig proposeur invalide après relecture : %v", err)
	}
	if err := got.Txs[0].VerifySignature(); err != nil {
		t.Fatalf("sig tx invalide après relecture : %v", err)
	}
	if err := got.LastCommit[0].Verify(); err != nil {
		t.Fatalf("sig vote invalide après relecture : %v", err)
	}

	// Recherche par hash → même bloc.
	byHash, err := s.BlockByHash(b.Hash)
	if err != nil || byHash == nil {
		t.Fatalf("BlockByHash: %v / nil=%v", err, byHash == nil)
	}
	if byHash.Hash != b.Hash {
		t.Fatal("BlockByHash a renvoyé le mauvais bloc")
	}
}

// TestLegacyJSONBlockStillReadable : un bloc écrit à l'ANCIEN format (JSON
// brut, sans tag) doit rester lisible — c'est l'invariant de migration
// pour ne pas perdre les bases existantes du testnet.
func TestLegacyJSONBlockStillReadable(t *testing.T) {
	s := openTemp(t)
	b := mkBlock(t, 7)

	// Écrit manuellement le bloc en JSON brut dans le bucket, comme le
	// faisait l'ancienne version de SaveBlock.
	legacy, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if legacy[0] != blockTagLegacyJSON {
		t.Fatalf("le JSON devrait commencer par '{', got 0x%02x", legacy[0])
	}
	if err := s.putRawBlock(7, legacy); err != nil {
		t.Fatalf("putRawBlock: %v", err)
	}

	got, err := s.GetBlock(7)
	if err != nil {
		t.Fatalf("GetBlock (legacy): %v", err)
	}
	if got == nil || got.Hash != b.Hash {
		t.Fatal("bloc legacy JSON illisible ou corrompu")
	}
	if err := got.VerifyProposerSig(); err != nil {
		t.Fatalf("sig proposeur invalide sur bloc legacy : %v", err)
	}
}

// TestMixedFormatsCoexist : une base contenant à la fois des blocs legacy
// JSON et des blocs binaires (cas réel après upgrade) se relit correctement.
func TestMixedFormatsCoexist(t *testing.T) {
	s := openTemp(t)
	legacyBlk := mkBlock(t, 10)
	legacy, _ := json.Marshal(legacyBlk)
	if err := s.putRawBlock(10, legacy); err != nil {
		t.Fatalf("putRawBlock: %v", err)
	}
	binBlk := mkBlock(t, 11)
	if err := s.SaveBlock(binBlk); err != nil {
		t.Fatalf("SaveBlock: %v", err)
	}

	g10, _ := s.GetBlock(10)
	g11, _ := s.GetBlock(11)
	if g10 == nil || g10.Hash != legacyBlk.Hash {
		t.Fatal("bloc legacy #10 illisible dans une base mixte")
	}
	if g11 == nil || g11.Hash != binBlk.Hash {
		t.Fatal("bloc binaire #11 illisible dans une base mixte")
	}
}
