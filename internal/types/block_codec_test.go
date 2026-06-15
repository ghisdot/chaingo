package types

import (
	"encoding/json"
	"reflect"
	"testing"

	"chaingo/internal/crypto"
)

// makeSignedTx : tx signée fixture pour les tests de bloc.
func makeSignedTx(t *testing.T, kp *crypto.KeyPair, nonce uint64, memo string) *Transaction {
	t.Helper()
	tx := &Transaction{
		ChainID:    "chaingo-test",
		Type:       TxTransfer,
		To:         "cg1111111111111111111111111111111111111111",
		TokenID:    NativeToken,
		Amount:     uint64(nonce) * Unit,
		Nonce:      nonce,
		MaxBaseFee: 200_000,
		Tip:        50_000,
		Memo:       memo,
		Timestamp:  1_700_000_000_000 + int64(nonce),
	}
	tx.SignWith(kp)
	return tx
}

// makeSignedVote : précommit signé fixture.
func makeSignedVote(t *testing.T, kp *crypto.KeyPair, height uint64, blockHash string) *Vote {
	t.Helper()
	v := &Vote{
		ChainID:   "chaingo-test",
		Height:    height,
		Kind:      PrecommitKind,
		BlockHash: blockHash,
	}
	v.SignWith(kp)
	return v
}

// TestVoteBinaryRoundtripPreservesSignature : un vote signé survit le round-trip.
func TestVoteBinaryRoundtripPreservesSignature(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	v := makeSignedVote(t, kp, 42, "abcdef0123456789")

	bin, err := v.MarshalBinary()
	if err != nil {
		t.Fatalf("Vote.MarshalBinary: %v", err)
	}
	var dec Vote
	if err := dec.UnmarshalBinary(bin); err != nil {
		t.Fatalf("Vote.UnmarshalBinary: %v", err)
	}
	if !reflect.DeepEqual(*v, dec) {
		t.Fatalf("vote roundtrip: champ différent\n  avant: %+v\n  après: %+v", v, dec)
	}
	if err := dec.Verify(); err != nil {
		t.Fatalf("Vote.Verify après round-trip : %v — la signature ne tient plus", err)
	}
	if dec.Hash() != v.Hash() {
		t.Fatalf("hash du vote modifié : %q vs %q", dec.Hash(), v.Hash())
	}
}

// TestEvidenceBinaryRoundtripPreservesEverything : une evidence d'équivocation
// signée des deux côtés survit le round-trip et reste cryptographiquement valide.
func TestEvidenceBinaryRoundtripPreservesEverything(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	chainID := "chaingo-test"
	a := &Vote{ChainID: chainID, Height: 100, Kind: PrecommitKind, BlockHash: "AAA"}
	b := &Vote{ChainID: chainID, Height: 100, Kind: PrecommitKind, BlockHash: "BBB"}
	a.SignWith(kp)
	b.SignWith(kp)
	ev := &DoubleSignEvidence{Height: 100, Voter: kp.Address(), VoteA: a, VoteB: b}

	if err := ev.Verify(chainID); err != nil {
		t.Fatalf("evidence d'origine devrait être valide : %v", err)
	}
	bin, err := ev.MarshalBinary()
	if err != nil {
		t.Fatalf("Evidence.MarshalBinary: %v", err)
	}
	var dec DoubleSignEvidence
	if err := dec.UnmarshalBinary(bin); err != nil {
		t.Fatalf("Evidence.UnmarshalBinary: %v", err)
	}
	if err := dec.Verify(chainID); err != nil {
		t.Fatalf("Evidence.Verify après round-trip : %v", err)
	}
	if dec.Hash() != ev.Hash() {
		t.Fatalf("hash de l'evidence modifié")
	}
}

// TestBlockBinaryRoundtripPreservesSignaturesAndHash : test critique de la
// tranche 2. Un bloc avec tx signées, evidence et précommits doit survivre
// le round-trip avec :
//  1. mêmes hashes (block + chaque tx + chaque vote + evidence),
//  2. toutes les signatures restent valides (proposeur, txs, votes),
//  3. tous les champs égaux.
func TestBlockBinaryRoundtripPreservesSignaturesAndHash(t *testing.T) {
	chainID := "chaingo-test"
	proposer, _ := crypto.GenerateKeyPair()
	voter1, _ := crypto.GenerateKeyPair()
	voter2, _ := crypto.GenerateKeyPair()
	signerA, _ := crypto.GenerateKeyPair()
	signerB, _ := crypto.GenerateKeyPair()

	txs := []*Transaction{
		makeSignedTx(t, signerA, 1, "tx un"),
		makeSignedTx(t, signerB, 1, "tx deux"),
	}

	// LastCommit : deux précommits du bloc parent.
	parentHash := "parent-hash-1234567890abcdef"
	commit := []*Vote{
		makeSignedVote(t, voter1, 41, parentHash),
		makeSignedVote(t, voter2, 41, parentHash),
	}

	// Evidence : équivocation d'un validateur tiers à une hauteur antérieure.
	equivocator, _ := crypto.GenerateKeyPair()
	va := &Vote{ChainID: chainID, Height: 40, Kind: PrecommitKind, BlockHash: "X"}
	vb := &Vote{ChainID: chainID, Height: 40, Kind: PrecommitKind, BlockHash: "Y"}
	va.SignWith(equivocator)
	vb.SignWith(equivocator)
	ev := &DoubleSignEvidence{Height: 40, Voter: equivocator.Address(), VoteA: va, VoteB: vb}

	b := &Block{
		Header: BlockHeader{
			Height:         42,
			PrevHash:       parentHash,
			Timestamp:      1_700_000_000_500,
			Proposer:       proposer.Address(),
			Round:          0,
			TxRoot:         TxRoot(txs),
			EvidenceRoot:   EvidenceRoot([]*DoubleSignEvidence{ev}),
			LastCommitRoot: CommitRoot(commit),
			StateRoot:      "state-root-deadbeef",
		},
		Txs:        txs,
		Evidence:   []*DoubleSignEvidence{ev},
		LastCommit: commit,
	}
	b.Hash = b.ComputeHash()
	b.ProposerPubKey = proposer.PubBytes()
	b.ProposerSignature = proposer.Sign(b.SigningBytes())

	// Sanity : tout valide avant codec.
	if err := b.VerifyProposerSig(); err != nil {
		t.Fatalf("bloc d'origine : sig proposeur invalide : %v", err)
	}

	bin, err := b.MarshalBinary()
	if err != nil {
		t.Fatalf("Block.MarshalBinary: %v", err)
	}
	var dec Block
	if err := dec.UnmarshalBinary(bin); err != nil {
		t.Fatalf("Block.UnmarshalBinary: %v", err)
	}

	// Header identique → hash identique.
	if dec.Hash != b.Hash {
		t.Fatalf("Block.Hash modifié : %q vs %q", dec.Hash, b.Hash)
	}
	if dec.ComputeHash() != b.Hash {
		t.Fatalf("Block.ComputeHash après round-trip != Hash original — SigningBytes a changé")
	}
	// Signature proposeur valide après decode.
	if err := dec.VerifyProposerSig(); err != nil {
		t.Fatalf("VerifyProposerSig après round-trip : %v", err)
	}
	// Tx signées valides.
	for i, tx := range dec.Txs {
		if err := tx.VerifySignature(); err != nil {
			t.Fatalf("tx[%d].VerifySignature après round-trip : %v", i, err)
		}
		if tx.Hash() != txs[i].Hash() {
			t.Fatalf("tx[%d] hash modifié", i)
		}
	}
	// Votes du LastCommit valides.
	for i, v := range dec.LastCommit {
		if err := v.Verify(); err != nil {
			t.Fatalf("commit[%d].Verify après round-trip : %v", i, err)
		}
		if v.Hash() != commit[i].Hash() {
			t.Fatalf("commit[%d] hash modifié", i)
		}
	}
	// Evidence reste vérifiable.
	for i, e := range dec.Evidence {
		if err := e.Verify(chainID); err != nil {
			t.Fatalf("evidence[%d].Verify après round-trip : %v", i, err)
		}
	}
}

// TestBlockBinaryCompactness : mesure le gain réel sur un bloc complet
// (header + 2 tx + 1 evidence + 2 votes) — c'est ce qui transite réellement
// entre nœuds. Affiche la mesure pour suivi.
func TestBlockBinaryCompactness(t *testing.T) {
	chainID := "chaingo-test"
	proposer, _ := crypto.GenerateKeyPair()
	v1, _ := crypto.GenerateKeyPair()
	v2, _ := crypto.GenerateKeyPair()
	sa, _ := crypto.GenerateKeyPair()
	sb, _ := crypto.GenerateKeyPair()
	txs := []*Transaction{makeSignedTx(t, sa, 1, ""), makeSignedTx(t, sb, 1, "")}
	commit := []*Vote{makeSignedVote(t, v1, 41, "h"), makeSignedVote(t, v2, 41, "h")}
	b := &Block{
		Header: BlockHeader{
			Height: 42, PrevHash: "p", Timestamp: 1_700_000_000_500,
			Proposer: proposer.Address(), TxRoot: TxRoot(txs),
			LastCommitRoot: CommitRoot(commit), StateRoot: "s",
		},
		Txs:        txs,
		LastCommit: commit,
	}
	_ = chainID
	b.Hash = b.ComputeHash()
	b.ProposerPubKey = proposer.PubBytes()
	b.ProposerSignature = proposer.Sign(b.SigningBytes())

	jsonBytes, _ := json.Marshal(b)
	bin, _ := b.MarshalBinary()
	jsonLen, binLen := len(jsonBytes), len(bin)
	gain := 100.0 * float64(jsonLen-binLen) / float64(jsonLen)
	t.Logf("Bloc complet : JSON %d octets, binaire %d octets — gain %.1f %%", jsonLen, binLen, gain)
	if binLen >= jsonLen {
		t.Fatalf("le binaire devrait être plus compact que le JSON")
	}
}
