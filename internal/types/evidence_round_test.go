package types

import (
	"testing"

	"chaingo/internal/crypto"
)

// TestEvidenceRequiresSameRound : depuis Vote.Round (#6), une preuve
// d'équivocation n'est valide que si les deux votes sont au MÊME round. Deux
// votes du même validateur pour des blocs différents à des rounds DIFFÉRENTS
// sont un changement légitime (POL) — surtout PAS un motif de slash.
func TestEvidenceRequiresSameRound(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	const chainID = "chaingo-test"

	mk := func(round uint32, hash string) *Vote {
		v := &Vote{ChainID: chainID, Height: 12, Round: round, Kind: PrecommitKind, BlockHash: hash}
		v.SignWith(kp)
		return v
	}

	// Même round, hash différents → vraie équivocation, valide.
	good := &DoubleSignEvidence{Height: 12, Voter: kp.Address(), VoteA: mk(0, "AAA"), VoteB: mk(0, "BBB")}
	if err := good.Verify(chainID); err != nil {
		t.Fatalf("équivocation au même round devrait être valide: %v", err)
	}

	// Rounds différents → changement légitime, NE DOIT PAS être une preuve valide.
	crossRound := &DoubleSignEvidence{Height: 12, Voter: kp.Address(), VoteA: mk(0, "AAA"), VoteB: mk(1, "BBB")}
	if err := crossRound.Verify(chainID); err == nil {
		t.Fatal("FAILLE : deux votes cross-round ne doivent pas constituer une équivocation punissable")
	}

	// Kinds différents (prevote vs précommit) → pas une preuve valide.
	pv := &Vote{ChainID: chainID, Height: 12, Round: 0, Kind: PrevoteKind, BlockHash: "BBB"}
	pv.SignWith(kp)
	mixed := &DoubleSignEvidence{Height: 12, Voter: kp.Address(), VoteA: mk(0, "AAA"), VoteB: pv}
	if err := mixed.Verify(chainID); err == nil {
		t.Fatal("votes de kinds différents ne doivent pas constituer une équivocation")
	}
}
