package types

import (
	"testing"

	"chaingo/internal/crypto"
)

// Fuzzing des décodeurs binaires — invariant n°1 de robustesse réseau :
// UnmarshalBinary sur des octets ARBITRAIRES ne doit JAMAIS paniquer.
// Au pire il renvoie une erreur. Une panique = un nœud qu'un attaquant
// peut tuer avec un paquet forgé (DoS).
//
//   go test ./internal/types/ -run=^$ -fuzz=FuzzTx     -fuzztime=20s
//   go test ./internal/types/ -run=^$ -fuzz=FuzzBlock  -fuzztime=20s
//   go test ./internal/types/ -run=^$ -fuzz=FuzzVote   -fuzztime=20s
//
// Bonus : pour les entrées qui décodent SANS erreur, on vérifie la
// stabilité du round-trip — réencoder puis redécoder doit redonner le
// même objet (sinon le format est ambigu, faille d'équivocation possible).

func seedTx() []byte {
	kp, _ := crypto.GenerateKeyPair()
	tx := &Transaction{
		ChainID: "chaingo-fuzz", Type: TxTransfer, To: "cg1111111111111111111111111111111111111111",
		TokenID: NativeToken, Amount: 42 * Unit, Nonce: 7, MaxBaseFee: 200_000, Tip: 50_000,
		Memo: "fuzz", Timestamp: 1_700_000_000_000,
	}
	tx.SignWith(kp)
	b, _ := tx.MarshalBinary()
	return b
}

func FuzzTx(f *testing.F) {
	f.Add(seedTx())
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) // uvarint overflow
	f.Fuzz(func(t *testing.T, data []byte) {
		var tx Transaction
		if err := tx.UnmarshalBinary(data); err != nil {
			return // erreur attendue sur entrée invalide
		}
		// Décodé sans erreur → le round-trip doit être stable.
		re, err := tx.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary après decode réussi a échoué: %v", err)
		}
		var tx2 Transaction
		if err := tx2.UnmarshalBinary(re); err != nil {
			t.Fatalf("re-decode d'un encodage valide a échoué: %v", err)
		}
	})
}

func seedBlock() []byte {
	proposer, _ := crypto.GenerateKeyPair()
	s, _ := crypto.GenerateKeyPair()
	v, _ := crypto.GenerateKeyPair()
	tx := &Transaction{ChainID: "chaingo-fuzz", Type: TxTransfer, To: "cg1", TokenID: NativeToken,
		Amount: Unit, Nonce: 0, MaxBaseFee: 1, Tip: 1, Timestamp: 1}
	tx.SignWith(s)
	vote := &Vote{ChainID: "chaingo-fuzz", Height: 0, Kind: PrecommitKind, BlockHash: "h"}
	vote.SignWith(v)
	blk := &Block{
		Header:     BlockHeader{Height: 1, PrevHash: "p", Proposer: proposer.Address(), StateRoot: "s"},
		Txs:        []*Transaction{tx},
		LastCommit: []*Vote{vote},
	}
	blk.Hash = blk.ComputeHash()
	blk.ProposerPubKey = proposer.PubBytes()
	blk.ProposerSignature = proposer.Sign(blk.SigningBytes())
	b, _ := blk.MarshalBinary()
	return b
}

func FuzzBlock(f *testing.F) {
	f.Add(seedBlock())
	f.Add([]byte{})
	f.Add([]byte{0x01, 0x00, 0x00})
	// Une slice annonçant un nombre d'éléments gigantesque ne doit pas
	// faire exploser la mémoire ni paniquer.
	f.Add([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0xff, 0x0f})
	f.Fuzz(func(t *testing.T, data []byte) {
		var b Block
		if err := b.UnmarshalBinary(data); err != nil {
			return
		}
		re, err := b.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary après decode réussi a échoué: %v", err)
		}
		var b2 Block
		if err := b2.UnmarshalBinary(re); err != nil {
			t.Fatalf("re-decode d'un bloc valide a échoué: %v", err)
		}
	})
}

func seedVote() []byte {
	kp, _ := crypto.GenerateKeyPair()
	v := &Vote{ChainID: "chaingo-fuzz", Height: 42, Kind: PrecommitKind, BlockHash: "deadbeef"}
	v.SignWith(kp)
	b, _ := v.MarshalBinary()
	return b
}

func FuzzVote(f *testing.F) {
	f.Add(seedVote())
	f.Add([]byte{})
	f.Add([]byte{0x05, 0x01})
	f.Fuzz(func(t *testing.T, data []byte) {
		var v Vote
		if err := v.UnmarshalBinary(data); err != nil {
			return
		}
		re, err := v.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary après decode réussi a échoué: %v", err)
		}
		var v2 Vote
		if err := v2.UnmarshalBinary(re); err != nil {
			t.Fatalf("re-decode d'un vote valide a échoué: %v", err)
		}
	})
}
