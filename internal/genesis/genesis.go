// Package genesis : document de genèse et application à l'état initial.
package genesis

import (
	"encoding/json"

	"chaingo/internal/state"
	"chaingo/internal/types"
)

type Genesis struct {
	ChainID   string            `json:"chain_id"`
	Timestamp int64             `json:"timestamp"`
	Params    *types.Params     `json:"params,omitempty"` // nil => types.DefaultParams()
	Alloc     map[string]uint64 `json:"alloc"`            // soldes liquides (ucgo)
	Stakes    map[string]uint64 `json:"stakes"`           // stakes validateurs (ucgo)
}

func (g *Genesis) Bytes() []byte {
	b, _ := json.Marshal(g)
	return b
}

func Parse(data []byte) (*Genesis, error) {
	g := &Genesis{}
	if err := json.Unmarshal(data, g); err != nil {
		return nil, err
	}
	return g, nil
}

// Apply seeds the state and returns the genesis block (height 0).
// Deterministic: every node applying the same document gets the same
// block hash and state root.
func (g *Genesis) Apply(st *state.State) *types.Block {
	p := types.DefaultParams()
	if g.Params != nil {
		p = *g.Params
	}
	st.SetParams(p)
	for addr, amount := range g.Alloc {
		st.Mint(addr, amount)
	}
	for addr, amount := range g.Stakes {
		st.BootstrapStake(addr, amount)
	}
	b := &types.Block{
		Header: types.BlockHeader{
			Height:    0,
			PrevHash:  "",
			Timestamp: g.Timestamp,
			Proposer:  "",
			TxRoot:    types.TxRoot(nil),
			StateRoot: st.Root(),
		},
	}
	b.Hash = b.ComputeHash()
	st.Commit(0, b.Hash)
	return b
}
