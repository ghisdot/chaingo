package types

import (
	"encoding/hex"
	"encoding/json"

	"chaingo/internal/crypto"
)

type BlockHeader struct {
	Height       uint64 `json:"height"`
	PrevHash     string `json:"prev_hash"`
	Timestamp    int64  `json:"timestamp"`
	Proposer     string `json:"proposer"`
	Round        uint32 `json:"round"` // round de secours (0 = nominal) — déterministe, vérifiable
	TxRoot       string `json:"tx_root"`
	EvidenceRoot string `json:"evidence_root"`
	StateRoot    string `json:"state_root"`
}

type Block struct {
	Header            BlockHeader           `json:"header"`
	Txs               []*Transaction        `json:"txs"`
	Evidence          []*DoubleSignEvidence `json:"evidence,omitempty"`
	Hash              string                `json:"hash"`
	ProposerPubKey    []byte                `json:"proposer_pub_key,omitempty"`
	ProposerSignature []byte                `json:"proposer_signature,omitempty"`
}

// TxRoot computes a Merkle root over the transaction hashes.
func TxRoot(txs []*Transaction) string {
	if len(txs) == 0 {
		return crypto.HashHex(nil)
	}
	layer := make([][]byte, len(txs))
	for i, tx := range txs {
		layer[i] = crypto.Hash(tx.SigningBytes())
	}
	for len(layer) > 1 {
		next := make([][]byte, 0, (len(layer)+1)/2)
		for i := 0; i < len(layer); i += 2 {
			if i+1 < len(layer) {
				next = append(next, crypto.Hash(layer[i], layer[i+1]))
			} else {
				next = append(next, crypto.Hash(layer[i], layer[i]))
			}
		}
		layer = next
	}
	return hex.EncodeToString(layer[0])
}

func (b *Block) SigningBytes() []byte {
	hb, _ := json.Marshal(b.Header)
	return hb
}

func (b *Block) ComputeHash() string { return crypto.HashHex(b.SigningBytes()) }

// VerifyProposerSig checks that the block was signed by the validator
// named in the header.
func (b *Block) VerifyProposerSig() error {
	if crypto.AddressFromPubBytes(b.ProposerPubKey) != b.Header.Proposer {
		return errInvalidProposer
	}
	return crypto.Verify(b.ProposerPubKey, b.SigningBytes(), b.ProposerSignature)
}

var errInvalidProposer = jsonError("proposer pubkey does not match header proposer")

type jsonError string

func (e jsonError) Error() string { return string(e) }
