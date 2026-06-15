package types

import (
	"encoding/json"
	"errors"

	"chaingo/internal/crypto"
)

// Kinds de votes BFT. Deux tours par hauteur dans le modèle complet
// (prevote -> precommit) ; aujourd'hui seul le precommit décide la finalité,
// le prevote prépare le verrouillage (tranche 2).
const (
	PrecommitKind = "precommit"
	PrevoteKind   = "prevote"
)

// Vote : vote BFT signé par un validateur sur un bloc à une hauteur donnée.
// Comme pour Transaction, l'ordre des champs EST le format de signature
// canonique — ne pas réordonner.
type Vote struct {
	ChainID string `json:"chain_id"`
	Height  uint64 `json:"height"`
	// Round : round du bloc voté (= round de l'en-tête). Sert au verrouillage
	// POL (#6) — un vote n'a de sens que rattaché à un round. L'équivocation
	// devient « deux votes du même kind à la même hauteur ET au même round pour
	// des hash différents » ; changer de vote à un round supérieur (sur preuve
	// d'une polka plus récente) est légitime.
	Round     uint32 `json:"round"`
	Kind      string `json:"kind"` // PrecommitKind | PrevoteKind
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

// CommitRoot : empreinte d'un ensemble de précommits (LastCommit d'un bloc),
// couverte par le hash du bloc via le header.
func CommitRoot(votes []*Vote) string {
	if len(votes) == 0 {
		return crypto.HashHex(nil)
	}
	acc := crypto.Hash([]byte(votes[0].Hash()))
	for i := 1; i < len(votes); i++ {
		acc = crypto.Hash(acc, []byte(votes[i].Hash()))
	}
	return crypto.HashHex(acc)
}
