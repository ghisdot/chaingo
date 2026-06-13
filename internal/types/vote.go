package types

import (
	"encoding/json"
	"errors"

	"chaingo/internal/crypto"
)

// Vote : précommit BFT signé par un validateur sur un bloc à une hauteur
// donnée (Phase 2 — finalité). Comme pour Transaction, l'ordre des champs
// EST le format de signature canonique — ne pas réordonner.
type Vote struct {
	ChainID   string `json:"chain_id"`
	Height    uint64 `json:"height"`
	BlockHash string `json:"block_hash"`
	Voter     string `json:"voter"`
	VoterPub  []byte `json:"voter_pub"`
	Signature []byte `json:"signature,omitempty"`
}

func (v *Vote) SigningBytes() []byte {
	clone := *v
	clone.Signature = nil
	b, _ := json.Marshal(&clone)
	return b
}

func (v *Vote) Hash() string { return crypto.HashHex(v.SigningBytes()) }

func (v *Vote) SignWith(kp *crypto.KeyPair) {
	v.Voter = kp.Address()
	v.VoterPub = kp.PubBytes()
	v.Signature = kp.Sign(v.SigningBytes())
}

func (v *Vote) Verify() error {
	if crypto.AddressFromPubBytes(v.VoterPub) != v.Voter {
		return errors.New("vote: voter pubkey does not match voter address")
	}
	return crypto.Verify(v.VoterPub, v.SigningBytes(), v.Signature)
}
