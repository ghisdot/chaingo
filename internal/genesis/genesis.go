// Package genesis : document de genèse et application à l'état initial.
package genesis

import (
	"encoding/json"
	"fmt"
	"sort"

	"chaingo/internal/crypto"
	"chaingo/internal/state"
	"chaingo/internal/types"
)

// VestingGrant : allocation verrouillée à la genèse, débloquée linéairement
// entre StartMs et EndMs au profit de Beneficiary (réutilise le contrat
// no-code "vesting"). Sert à enforcer on-chain les parts équipe/trésorerie.
type VestingGrant struct {
	Beneficiary string `json:"beneficiary"`
	Amount      uint64 `json:"amount"`
	StartMs     int64  `json:"start_ms"`
	EndMs       int64  `json:"end_ms"`
}

type Genesis struct {
	ChainID   string            `json:"chain_id"`
	Timestamp int64             `json:"timestamp"`
	Params    *types.Params     `json:"params,omitempty"` // nil => types.DefaultParams()
	Alloc     map[string]uint64 `json:"alloc"`            // soldes liquides (ucgo)
	Stakes    map[string]uint64 `json:"stakes"`           // stakes validateurs (ucgo)
	Vesting   []VestingGrant    `json:"vesting,omitempty"`
}

func (g *Genesis) Bytes() []byte {
	b, _ := json.MarshalIndent(g, "", "  ")
	return b
}

func Parse(data []byte) (*Genesis, error) {
	g := &Genesis{}
	if err := json.Unmarshal(data, g); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *Genesis) params() types.Params {
	if g.Params != nil {
		return *g.Params
	}
	return types.DefaultParams()
}

// Apply seeds the state and returns the genesis block (height 0).
// Deterministic: every node applying the same document gets the same
// block hash and state root.
func (g *Genesis) Apply(st *state.State) *types.Block {
	st.SetParams(g.params())
	for addr, amount := range g.Alloc {
		st.Mint(addr, amount)
	}
	for addr, amount := range g.Stakes {
		st.BootstrapStake(addr, amount)
	}
	// Vesting : ordre stable (par bénéficiaire puis montant) pour des IDs
	// de contrat déterministes entre tous les nœuds.
	grants := append([]VestingGrant(nil), g.Vesting...)
	sort.Slice(grants, func(i, j int) bool {
		if grants[i].Beneficiary != grants[j].Beneficiary {
			return grants[i].Beneficiary < grants[j].Beneficiary
		}
		return grants[i].Amount < grants[j].Amount
	})
	for i, gr := range grants {
		st.BootstrapVesting(fmt.Sprintf("genesis-vesting-%d", i), gr.Beneficiary, gr.Amount, gr.StartMs, gr.EndMs)
	}
	b := &types.Block{
		Header: types.BlockHeader{
			Height:         0,
			PrevHash:       "",
			Timestamp:      g.Timestamp,
			Proposer:       "",
			TxRoot:         types.TxRoot(nil),
			EvidenceRoot:   types.EvidenceRoot(nil),
			LastCommitRoot: types.CommitRoot(nil),
			StateRoot:      st.Root(),
		},
	}
	b.Hash = b.ComputeHash()
	st.Commit(0, b.Hash)
	return b
}

// Summary : total supply à la genèse, ventilée.
type Summary struct {
	ChainID     string
	Liquid      uint64
	Staked      uint64
	Vested      uint64
	TotalSupply uint64
	Validators  int
	VestingN    int
}

// Validate vérifie qu'un document de genèse est cohérent et déployable, et
// renvoie un résumé. Erreurs strictes : adresses invalides, stake sous le
// minimum, montants nuls, fenêtre de vesting incohérente.
func (g *Genesis) Validate() (*Summary, error) {
	if g.ChainID == "" {
		return nil, fmt.Errorf("chain_id requis")
	}
	p := g.params()
	if p.BlockIntervalMs <= 0 || p.MaxBlockTxs == 0 || p.MinValidatorStake == 0 {
		return nil, fmt.Errorf("params invalides (block_interval_ms, max_block_txs, min_validator_stake)")
	}
	s := &Summary{ChainID: g.ChainID}
	for addr, amt := range g.Alloc {
		if !crypto.ValidAddress(addr) {
			return nil, fmt.Errorf("alloc: adresse invalide %q", addr)
		}
		if amt == 0 {
			return nil, fmt.Errorf("alloc: montant nul pour %s", addr)
		}
		s.Liquid += amt
	}
	for addr, amt := range g.Stakes {
		if !crypto.ValidAddress(addr) {
			return nil, fmt.Errorf("stakes: adresse invalide %q", addr)
		}
		if amt < p.MinValidatorStake {
			return nil, fmt.Errorf("stakes: %s sous le minimum (%d < %d)", addr, amt, p.MinValidatorStake)
		}
		s.Staked += amt
		s.Validators++
	}
	for i, gr := range g.Vesting {
		if !crypto.ValidAddress(gr.Beneficiary) {
			return nil, fmt.Errorf("vesting[%d]: bénéficiaire invalide %q", i, gr.Beneficiary)
		}
		if gr.Amount == 0 {
			return nil, fmt.Errorf("vesting[%d]: montant nul", i)
		}
		if gr.StartMs <= 0 || gr.EndMs <= gr.StartMs {
			return nil, fmt.Errorf("vesting[%d]: fenêtre invalide (end_ms doit suivre start_ms > 0)", i)
		}
		s.Vested += gr.Amount
		s.VestingN++
	}
	if s.Validators == 0 {
		return nil, fmt.Errorf("au moins un validateur (stakes) est requis")
	}
	s.TotalSupply = s.Liquid + s.Staked + s.Vested
	return s, nil
}
