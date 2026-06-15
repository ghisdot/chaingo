package types

import (
	"encoding/json"
	"testing"

	"chaingo/internal/crypto"
)

// Benchmarks du codec binaire vs JSON — mesures reproductibles pour suivre
// l'évolution et documenter les gains (tranche 5).
//
//   go test ./internal/types/ -bench=Codec -benchmem -run=^$
//
// On mesure le débit (ns/op) ET les allocations (B/op, allocs/op) du
// chemin encode puis decode complet, pour une tx signée et un bloc complet.

func benchTx(b *testing.B) *Transaction {
	kp, _ := crypto.GenerateKeyPair()
	tx := &Transaction{
		ChainID: "chaingo-bench", Type: TxTransfer,
		To: "cg1111111111111111111111111111111111111111", TokenID: NativeToken,
		Amount: 42 * Unit, Nonce: 7, MaxBaseFee: 200_000, Tip: 50_000,
		Memo: "bench", Timestamp: 1_700_000_000_000,
	}
	tx.SignWith(kp)
	return tx
}

func benchBlock(b *testing.B) *Block {
	proposer, _ := crypto.GenerateKeyPair()
	s1, _ := crypto.GenerateKeyPair()
	s2, _ := crypto.GenerateKeyPair()
	v1, _ := crypto.GenerateKeyPair()
	v2, _ := crypto.GenerateKeyPair()
	mkTx := func(kp *crypto.KeyPair) *Transaction {
		tx := &Transaction{ChainID: "chaingo-bench", Type: TxTransfer,
			To: "cg1111111111111111111111111111111111111111", TokenID: NativeToken,
			Amount: Unit, Nonce: 0, MaxBaseFee: 200_000, Tip: 50_000, Timestamp: 1_700_000_000_000}
		tx.SignWith(kp)
		return tx
	}
	mkVote := func(kp *crypto.KeyPair) *Vote {
		v := &Vote{ChainID: "chaingo-bench", Height: 41, Kind: PrecommitKind, BlockHash: "h"}
		v.SignWith(kp)
		return v
	}
	txs := []*Transaction{mkTx(s1), mkTx(s2)}
	commit := []*Vote{mkVote(v1), mkVote(v2)}
	blk := &Block{
		Header: BlockHeader{Height: 42, PrevHash: "p", Timestamp: 1_700_000_000_500,
			Proposer: proposer.Address(), TxRoot: TxRoot(txs), LastCommitRoot: CommitRoot(commit), StateRoot: "s"},
		Txs: txs, LastCommit: commit,
	}
	blk.Hash = blk.ComputeHash()
	blk.ProposerPubKey = proposer.PubBytes()
	blk.ProposerSignature = proposer.Sign(blk.SigningBytes())
	return blk
}

func BenchmarkCodecTxBinary(b *testing.B) {
	tx := benchTx(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bin, _ := tx.MarshalBinary()
		var dec Transaction
		_ = dec.UnmarshalBinary(bin)
	}
}

func BenchmarkCodecTxJSON(b *testing.B) {
	tx := benchTx(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bin, _ := json.Marshal(tx)
		var dec Transaction
		_ = json.Unmarshal(bin, &dec)
	}
}

func BenchmarkCodecBlockBinary(b *testing.B) {
	blk := benchBlock(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bin, _ := blk.MarshalBinary()
		var dec Block
		_ = dec.UnmarshalBinary(bin)
	}
}

func BenchmarkCodecBlockJSON(b *testing.B) {
	blk := benchBlock(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bin, _ := json.Marshal(blk)
		var dec Block
		_ = json.Unmarshal(bin, &dec)
	}
}
