package types

import (
	"encoding/hex"
	"encoding/json"
	"errors"

	"chaingo/internal/crypto"
)

// DoubleSignEvidence : preuve qu'un validateur a précommit DEUX blocs
// différents à la même hauteur (équivocation). C'est une faute byzantine
// prouvable cryptographiquement — les deux votes sont signés par lui.
//
// L'evidence transite dans le bloc (pas via le gossip) pour que le slash
// soit appliqué de façon DÉTERMINISTE par tous les nœuds qui rejouent le
// bloc — invariant n°1 : même entrée, même racine d'état partout.
type DoubleSignEvidence struct {
	Height uint64 `json:"height"`
	Voter  string `json:"voter"`
	VoteA  *Vote  `json:"vote_a"`
	VoteB  *Vote  `json:"vote_b"`
}

func (e *DoubleSignEvidence) Hash() string {
	b, _ := json.Marshal(e)
	return crypto.HashHex(b)
}

// Verify : les deux votes sont signés par `Voter`, à la même hauteur, AU MÊME
// ROUND et du MÊME KIND, sur des blocs DIFFÉRENTS. Si tout est vrai,
// l'équivocation est prouvée.
//
// L'égalité de round est ESSENTIELLE depuis l'ajout de Vote.Round (#6) : avec
// le verrouillage POL, un validateur PEUT légitimement voter pour des blocs
// différents à des rounds DIFFÉRENTS (sur preuve d'une polka plus récente).
// Seul un conflit au MÊME round est une faute. Sans ce contrôle, on pourrait
// faire slasher à tort un validateur honnête en exhibant deux de ses votes
// cross-round.
func (e *DoubleSignEvidence) Verify(chainID string) error {
	if e.VoteA == nil || e.VoteB == nil {
		return errors.New("evidence: missing vote")
	}
	if e.VoteA.BlockHash == e.VoteB.BlockHash {
		return errors.New("evidence: same block hash (not equivocation)")
	}
	if e.VoteA.Round != e.VoteB.Round {
		return errors.New("evidence: votes at different rounds (legitimate POL change, not equivocation)")
	}
	if e.VoteA.Kind != e.VoteB.Kind {
		return errors.New("evidence: votes of different kinds")
	}
	for _, v := range []*Vote{e.VoteA, e.VoteB} {
		if v.ChainID != chainID {
			return errors.New("evidence: vote on wrong chain")
		}
		if v.Height != e.Height || v.Voter != e.Voter {
			return errors.New("evidence: vote height/voter mismatch")
		}
		if err := v.Verify(); err != nil {
			return err
		}
	}
	return nil
}

// EvidenceRoot : empreinte de la liste d'evidence d'un bloc (couverte par
// le hash du bloc via le header).
func EvidenceRoot(es []*DoubleSignEvidence) string {
	if len(es) == 0 {
		return crypto.HashHex(nil)
	}
	h := make([][]byte, len(es))
	for i, e := range es {
		h[i] = crypto.Hash([]byte(e.Hash()))
	}
	acc := h[0]
	for i := 1; i < len(h); i++ {
		acc = crypto.Hash(acc, h[i])
	}
	return hex.EncodeToString(acc)
}
