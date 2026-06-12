// Package state implémente la machine d'état de ChainGO : comptes,
// tokens, validateurs, supply (mint/burn) et sélection du proposeur.
package state

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"chaingo/internal/crypto"
	"chaingo/internal/types"
)

type Account struct {
	Address  string            `json:"address"`
	Balances map[string]uint64 `json:"balances"`
	Nonce    uint64            `json:"nonce"`
	Staked   uint64            `json:"staked"`
	// Unbonding et Delegations : agrégés depuis l'état global, uniquement
	// dans les copies renvoyées par GetAccount (vides dans l'état stocké).
	Unbonding   uint64            `json:"unbonding,omitempty"`
	Delegations map[string]uint64 `json:"delegations,omitempty"`
}

type Token struct {
	types.TokenParams
	Creator     string `json:"creator"`
	TotalSupply uint64 `json:"total_supply"`
	CreatedAt   uint64 `json:"created_at_height"`
}

type Validator struct {
	Address        string `json:"address"`
	Stake          uint64 `json:"stake"`
	BlocksProposed uint64 `json:"blocks_proposed"`
	RewardsEarned  uint64 `json:"rewards_earned"`
	// Délégations reçues : Delegated = somme, Delegators = détail.
	// Le poids du validateur (tirage + émission) = Stake + Delegated.
	Delegated  uint64            `json:"delegated,omitempty"`
	Delegators map[string]uint64 `json:"delegators,omitempty"`
}

func (v *Validator) weight() uint64 { return v.Stake + v.Delegated }

type Supply struct {
	Total  uint64 `json:"total"`
	Minted uint64 `json:"minted"`
	Burned uint64 `json:"burned"`
}

// Unbonding : CGO en cours de désengagement — rendus liquides quand le
// timestamp du bloc dépasse ReleaseAt.
type Unbonding struct {
	Address   string `json:"address"`
	Amount    uint64 `json:"amount"`
	ReleaseAt int64  `json:"release_at"` // ms epoch
}

// Contract : instance d'un template no-code. Les fonds sont verrouillés
// dans le contrat à la création (déduits du créateur) et libérés par les
// actions — jamais par du code arbitraire.
type Contract struct {
	ID          string `json:"id"` // hash de la tx de création
	Template    string `json:"template"`
	Creator     string `json:"creator"`
	TokenID     string `json:"token_id"`
	Amount      uint64 `json:"amount"`   // total verrouillé
	Released    uint64 `json:"released"` // déjà libéré
	Beneficiary string `json:"beneficiary,omitempty"`
	StartMs     int64  `json:"start_ms,omitempty"`
	EndMs       int64  `json:"end_ms,omitempty"`
	Seller      string `json:"seller,omitempty"`
	Arbiter     string `json:"arbiter,omitempty"`
	Status      string `json:"status"` // active | completed | refunded
	CreatedAt   uint64 `json:"created_at_height"`
}

type State struct {
	mu         sync.RWMutex
	Accounts   map[string]*Account   `json:"accounts"`
	Tokens     map[string]*Token     `json:"tokens"`
	Validators map[string]*Validator `json:"validators"`
	Contracts  map[string]*Contract  `json:"contracts"`
	Unbonding  []*Unbonding          `json:"unbonding"`
	Supply     Supply                `json:"supply"`
	Params     types.Params          `json:"params"`
	BaseFee    uint64                `json:"base_fee"` // base fee courant (EIP-1559)
	Height     uint64                `json:"height"`
	LastHash   string                `json:"last_hash"`
}

func New() *State {
	return &State{
		Accounts:   map[string]*Account{},
		Tokens:     map[string]*Token{},
		Validators: map[string]*Validator{},
		Contracts:  map[string]*Contract{},
	}
}

// SetParams initialise les règles de la chaîne (genèse uniquement).
func (s *State) SetParams(p types.Params) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Params = p
	s.BaseFee = p.MinBaseFee
}

func (s *State) GetParams() types.Params {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Params
}

func (s *State) GetBaseFee() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.BaseFee
}

func (s *State) acct(addr string) *Account {
	a, ok := s.Accounts[addr]
	if !ok {
		a = &Account{Address: addr, Balances: map[string]uint64{}}
		s.Accounts[addr] = a
	}
	return a
}

// ---- lectures ----

func (s *State) GetHeight() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Height
}

func (s *State) GetLastHash() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastHash
}

func (s *State) NonceOf(addr string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if a, ok := s.Accounts[addr]; ok {
		return a.Nonce
	}
	return 0
}

func (s *State) GetAccount(addr string) *Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.Accounts[addr]
	if !ok {
		a = &Account{Address: addr, Balances: map[string]uint64{}}
	}
	cp := *a
	cp.Balances = make(map[string]uint64, len(a.Balances))
	for k, v := range a.Balances {
		cp.Balances[k] = v
	}
	for _, u := range s.Unbonding {
		if u.Address == addr {
			cp.Unbonding += u.Amount
		}
	}
	for vAddr, v := range s.Validators {
		if amt, ok := v.Delegators[addr]; ok {
			if cp.Delegations == nil {
				cp.Delegations = map[string]uint64{}
			}
			cp.Delegations[vAddr] = amt
		}
	}
	return &cp
}

func (s *State) GetSupply() Supply {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Supply
}

func (s *State) ListValidators() []*Validator {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Validator, 0, len(s.Validators))
	for _, v := range s.Validators {
		cp := *v
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Stake > out[j].Stake })
	return out
}

func (s *State) ListTokens() []*Token {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Token, 0, len(s.Tokens))
	for _, t := range s.Tokens {
		cp := *t
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out
}

func (s *State) ListContracts() []*Contract {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Contract, 0, len(s.Contracts))
	for _, c := range s.Contracts {
		cp := *c
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

func (s *State) GetContract(id string) *Contract {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.Contracts[id]
	if !ok {
		return nil
	}
	cp := *c
	return &cp
}

func (s *State) GetToken(sym string) *Token {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.Tokens[sym]
	if !ok {
		return nil
	}
	cp := *t
	return &cp
}

// ---- sélection du proposeur ----

// SelectProposer picks the block proposer deterministically, weighted by
// stake, seeded by (prevHash, height, round) — every node computes the
// same one. round > 0 désigne les proposeurs de secours : si le proposeur
// du round r ne produit pas dans l'intervalle de bloc, le round r+1 prend
// la main (liveness quand un validateur staké est hors-ligne).
func (s *State) SelectProposer(height uint64, prevHash string, round uint32) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	type sv struct {
		addr  string
		stake uint64
	}
	var vals []sv
	var total uint64
	for _, v := range s.Validators {
		if w := v.weight(); w > 0 {
			vals = append(vals, sv{v.Address, w})
			total += w
		}
	}
	if total == 0 {
		return ""
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i].addr < vals[j].addr })
	var hb [12]byte
	binary.BigEndian.PutUint64(hb[:8], height)
	binary.BigEndian.PutUint32(hb[8:], round)
	seed := crypto.Hash([]byte(prevHash), hb[:])
	r := binary.BigEndian.Uint64(seed[:8]) % total
	for _, v := range vals {
		if r < v.stake {
			return v.addr
		}
		r -= v.stake
	}
	return vals[len(vals)-1].addr
}

// ---- exécution ----

// Execute applies one block's worth of work, in deterministic order:
// release matured unbondings, apply txs (dynamic fees), mint the
// inflation reward to the proposer, adjust the EIP-1559 base fee, and
// return the resulting state root. strict=false (proposer building a
// block) drops failing txs; strict=true (follower replaying a received
// block) reports the first failure — caller restores the snapshot.
// blockTime is the block header timestamp (ms) — never the local clock.
func (s *State) Execute(txs []*types.Transaction, proposer string, blockTime int64, strict bool) (applied []*types.Transaction, failed map[string]error, root string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.releaseUnbondedLocked(blockTime)

	failed = map[string]error{}
	for _, tx := range txs {
		if aerr := s.applyTx(tx, proposer, blockTime); aerr != nil {
			if strict {
				return nil, nil, "", fmt.Errorf("tx %s: %w", tx.Hash(), aerr)
			}
			failed[tx.Hash()] = aerr
			continue
		}
		applied = append(applied, tx)
	}

	if proposer != "" {
		reward := types.RewardPerBlock(s.totalStakedLocked(), s.Params)
		if reward > 0 {
			s.distributeRewardLocked(proposer, reward)
		}
	}
	s.BaseFee = types.NextBaseFee(s.BaseFee, len(applied), s.Params)
	return applied, failed, s.rootLocked(), nil
}

func (s *State) totalStakedLocked() uint64 {
	var total uint64
	for _, v := range s.Validators {
		total += v.weight()
	}
	return total
}

// distributeRewardLocked : partage la récompense du bloc entre le
// validateur proposeur et ses délégateurs, au pro-rata de leur mise,
// moins la commission du validateur. Ordre d'itération trié — le
// résultat doit être identique sur tous les nœuds.
func (s *State) distributeRewardLocked(proposer string, reward uint64) {
	v, ok := s.Validators[proposer]
	if !ok || v.Delegated == 0 {
		s.mintLocked(proposer, reward)
		if ok {
			v.BlocksProposed++
			v.RewardsEarned += reward
		}
		return
	}
	weight := v.weight()
	delegShare := types.MulDiv(reward, v.Delegated, weight)
	commission := types.MulDiv(delegShare, s.Params.DelegationCommissionBps, 10_000)
	pool := delegShare - commission

	addrs := make([]string, 0, len(v.Delegators))
	for a := range v.Delegators {
		addrs = append(addrs, a)
	}
	sort.Strings(addrs)
	distributed := uint64(0)
	for _, a := range addrs {
		share := types.MulDiv(pool, v.Delegators[a], v.Delegated)
		s.acct(a).Balances[types.NativeToken] += share
		distributed += share
	}
	// part du validateur : sa mise propre + commission + poussière d'arrondi
	validatorPart := reward - delegShare + commission + (pool - distributed)
	s.acct(proposer).Balances[types.NativeToken] += validatorPart
	s.Supply.Total += reward
	s.Supply.Minted += reward
	v.BlocksProposed++
	v.RewardsEarned += validatorPart
}

func (s *State) releaseUnbondedLocked(blockTime int64) {
	if len(s.Unbonding) == 0 {
		return
	}
	var still []*Unbonding
	for _, u := range s.Unbonding {
		if u.ReleaseAt <= blockTime {
			s.acct(u.Address).Balances[types.NativeToken] += u.Amount
		} else {
			still = append(still, u)
		}
	}
	s.Unbonding = still
}

func (s *State) applyTx(tx *types.Transaction, proposer string, blockTime int64) error {
	from := s.acct(tx.From)
	if tx.Nonce != from.Nonce {
		return fmt.Errorf("bad nonce: got %d, want %d", tx.Nonce, from.Nonce)
	}
	// Frais dynamiques : burn = base fee courant (+ surcoût private,
	// + frais de création de token), tip = enchère de l'émetteur.
	if tx.MaxBaseFee < s.BaseFee {
		return fmt.Errorf("max_base_fee %d below current base fee %d", tx.MaxBaseFee, s.BaseFee)
	}
	burn := s.BaseFee
	if tx.Private {
		burn += s.BaseFee * s.Params.PrivacyFeeMult
	}
	if tx.Type == types.TxCreateToken {
		burn += s.Params.TokenCreateFee
	}
	if tx.Type == types.TxContractCreate {
		burn += s.Params.ContractCreateFee
	}
	fee := burn + tx.Tip
	needNative := fee
	if tx.Type == types.TxTransfer && tx.TokenID == types.NativeToken {
		needNative += tx.Amount
	}
	if tx.Type == types.TxStake || tx.Type == types.TxDelegate {
		needNative += tx.Amount
	}
	if tx.Type == types.TxContractCreate && tx.Contract.TokenID == types.NativeToken {
		needNative += tx.Contract.Amount
	}
	if from.Balances[types.NativeToken] < needNative {
		return errors.New("insufficient CGO balance for amount + fees")
	}

	switch tx.Type {
	case types.TxTransfer:
		if tx.TokenID == types.NativeToken {
			from.Balances[types.NativeToken] -= tx.Amount
			s.acct(tx.To).Balances[types.NativeToken] += tx.Amount
		} else {
			if _, ok := s.Tokens[tx.TokenID]; !ok {
				return fmt.Errorf("unknown token %q", tx.TokenID)
			}
			if from.Balances[tx.TokenID] < tx.Amount {
				return fmt.Errorf("insufficient %s balance", tx.TokenID)
			}
			from.Balances[tx.TokenID] -= tx.Amount
			s.acct(tx.To).Balances[tx.TokenID] += tx.Amount
		}
	case types.TxCreateToken:
		sym := tx.Token.Symbol
		if _, exists := s.Tokens[sym]; exists {
			return fmt.Errorf("token %q already exists", sym)
		}
		s.Tokens[sym] = &Token{
			TokenParams: *tx.Token,
			Creator:     tx.From,
			TotalSupply: tx.Token.Supply,
			CreatedAt:   s.Height + 1,
		}
		from.Balances[sym] += tx.Token.Supply
	case types.TxMint:
		t, ok := s.Tokens[tx.TokenID]
		if !ok {
			return fmt.Errorf("unknown token %q", tx.TokenID)
		}
		if t.Creator != tx.From {
			return errors.New("only the token creator can mint")
		}
		if !t.Mintable {
			return errors.New("token is not mintable")
		}
		target := tx.From
		if tx.To != "" {
			target = tx.To
		}
		t.TotalSupply += tx.Amount
		s.acct(target).Balances[tx.TokenID] += tx.Amount
	case types.TxStake:
		v, ok := s.Validators[tx.From]
		current := uint64(0)
		if ok {
			current = v.Stake
		}
		if current+tx.Amount < s.Params.MinValidatorStake {
			return fmt.Errorf("resulting stake below minimum (%d ucgo required)", s.Params.MinValidatorStake)
		}
		from.Balances[types.NativeToken] -= tx.Amount
		from.Staked += tx.Amount
		if !ok {
			v = &Validator{Address: tx.From}
			s.Validators[tx.From] = v
		}
		v.Stake += tx.Amount
	case types.TxUnstake:
		v, ok := s.Validators[tx.From]
		if !ok || from.Staked < tx.Amount {
			return errors.New("insufficient staked amount")
		}
		remaining := v.Stake - tx.Amount
		if remaining != 0 && remaining < s.Params.MinValidatorStake {
			return fmt.Errorf("either unstake everything or keep at least %d ucgo staked", s.Params.MinValidatorStake)
		}
		// Pas de liquidité immédiate : les fonds entrent en unbonding et
		// seront rendus quand un bloc dépassera ReleaseAt.
		from.Staked -= tx.Amount
		s.Unbonding = append(s.Unbonding, &Unbonding{
			Address:   tx.From,
			Amount:    tx.Amount,
			ReleaseAt: blockTime + s.Params.UnbondingSeconds*1000,
		})
		v.Stake = remaining
		if remaining == 0 {
			// Le validateur quitte le réseau : ses délégateurs partent
			// automatiquement en unbonding (ordre trié — déterminisme).
			dAddrs := make([]string, 0, len(v.Delegators))
			for a := range v.Delegators {
				dAddrs = append(dAddrs, a)
			}
			sort.Strings(dAddrs)
			for _, a := range dAddrs {
				s.Unbonding = append(s.Unbonding, &Unbonding{
					Address:   a,
					Amount:    v.Delegators[a],
					ReleaseAt: blockTime + s.Params.UnbondingSeconds*1000,
				})
			}
			delete(s.Validators, tx.From)
		}
	case types.TxDelegate:
		v, ok := s.Validators[tx.To]
		if !ok {
			return fmt.Errorf("%s is not a validator", tx.To)
		}
		if v.Delegators[tx.From]+tx.Amount < s.Params.MinDelegation {
			return fmt.Errorf("delegation below minimum (%d ucgo)", s.Params.MinDelegation)
		}
		from.Balances[types.NativeToken] -= tx.Amount
		if v.Delegators == nil {
			v.Delegators = map[string]uint64{}
		}
		v.Delegators[tx.From] += tx.Amount
		v.Delegated += tx.Amount
	case types.TxUndelegate:
		v, ok := s.Validators[tx.To]
		if !ok || v.Delegators[tx.From] < tx.Amount {
			return errors.New("insufficient delegation to this validator")
		}
		remaining := v.Delegators[tx.From] - tx.Amount
		if remaining != 0 && remaining < s.Params.MinDelegation {
			return fmt.Errorf("either undelegate everything or keep at least %d ucgo", s.Params.MinDelegation)
		}
		s.Unbonding = append(s.Unbonding, &Unbonding{
			Address:   tx.From,
			Amount:    tx.Amount,
			ReleaseAt: blockTime + s.Params.UnbondingSeconds*1000,
		})
		v.Delegated -= tx.Amount
		if remaining == 0 {
			delete(v.Delegators, tx.From)
		} else {
			v.Delegators[tx.From] = remaining
		}
	case types.TxContractCreate:
		c := tx.Contract
		if c.TokenID != types.NativeToken {
			if _, ok := s.Tokens[c.TokenID]; !ok {
				return fmt.Errorf("unknown token %q", c.TokenID)
			}
			if from.Balances[c.TokenID] < c.Amount {
				return fmt.Errorf("insufficient %s balance to lock", c.TokenID)
			}
		}
		// Les fonds quittent le créateur et sont verrouillés dans le contrat.
		from.Balances[c.TokenID] -= c.Amount
		id := tx.Hash()
		s.Contracts[id] = &Contract{
			ID:          id,
			Template:    c.Template,
			Creator:     tx.From,
			TokenID:     c.TokenID,
			Amount:      c.Amount,
			Beneficiary: c.Beneficiary,
			StartMs:     c.StartMs,
			EndMs:       c.EndMs,
			Seller:      c.Seller,
			Arbiter:     c.Arbiter,
			Status:      "active",
			CreatedAt:   s.Height + 1,
		}
	case types.TxContractExec:
		c, ok := s.Contracts[tx.ContractID]
		if !ok {
			return fmt.Errorf("unknown contract %q", tx.ContractID)
		}
		if c.Status != "active" {
			return fmt.Errorf("contract is %s", c.Status)
		}
		switch {
		case c.Template == types.TemplateVesting && tx.Action == types.ActionClaim:
			if tx.From != c.Beneficiary {
				return errors.New("only the beneficiary can claim")
			}
			// Déblocage linéaire entre StartMs et EndMs, à l'horloge des blocs.
			var vested uint64
			switch {
			case blockTime <= c.StartMs:
				vested = 0
			case blockTime >= c.EndMs:
				vested = c.Amount
			default:
				vested = types.MulDiv(c.Amount, uint64(blockTime-c.StartMs), uint64(c.EndMs-c.StartMs))
			}
			claimable := vested - c.Released
			if claimable == 0 {
				return errors.New("nothing vested yet")
			}
			s.acct(c.Beneficiary).Balances[c.TokenID] += claimable
			c.Released += claimable
			if c.Released == c.Amount {
				c.Status = "completed"
			}
		case c.Template == types.TemplateEscrow && tx.Action == types.ActionRelease:
			if tx.From != c.Creator && tx.From != c.Arbiter {
				return errors.New("only the buyer or the arbiter can release")
			}
			s.acct(c.Seller).Balances[c.TokenID] += c.Amount
			c.Released = c.Amount
			c.Status = "completed"
		case c.Template == types.TemplateEscrow && tx.Action == types.ActionRefund:
			if tx.From != c.Seller && tx.From != c.Arbiter {
				return errors.New("only the seller or the arbiter can refund")
			}
			s.acct(c.Creator).Balances[c.TokenID] += c.Amount
			c.Status = "refunded"
		default:
			return fmt.Errorf("action %q not valid for template %q", tx.Action, c.Template)
		}
	default:
		return fmt.Errorf("unknown tx type %q", tx.Type)
	}

	// Frais : la part "burn" disparaît de la supply, le tip va au proposeur.
	from.Balances[types.NativeToken] -= fee
	s.Supply.Total -= burn
	s.Supply.Burned += burn
	if proposer != "" {
		s.acct(proposer).Balances[types.NativeToken] += tx.Tip
	} else {
		s.Supply.Total -= tx.Tip
		s.Supply.Burned += tx.Tip
	}
	from.Nonce++
	return nil
}

// Mint crée de la monnaie native (genèse, récompenses de bloc).
func (s *State) Mint(addr string, amount uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mintLocked(addr, amount)
}

func (s *State) mintLocked(addr string, amount uint64) {
	s.acct(addr).Balances[types.NativeToken] += amount
	s.Supply.Total += amount
	s.Supply.Minted += amount
}

// BootstrapStake installe un stake à la genèse (sans passer par une tx).
func (s *State) BootstrapStake(addr string, amount uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.acct(addr)
	a.Staked += amount
	s.Supply.Total += amount
	s.Supply.Minted += amount
	v, ok := s.Validators[addr]
	if !ok {
		v = &Validator{Address: addr}
		s.Validators[addr] = v
	}
	v.Stake += amount
}

func (s *State) Commit(height uint64, hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Height = height
	s.LastHash = hash
}

// ---- racine d'état & persistance ----

// rootLocked hashes the canonical JSON of the chain state. encoding/json
// sorts map keys, so this is deterministic across nodes. (v1: O(n) per
// block — replaced by a sparse Merkle tree in Phase 2.)
func (s *State) rootLocked() string {
	b, _ := json.Marshal(struct {
		Accounts   map[string]*Account   `json:"accounts"`
		Tokens     map[string]*Token     `json:"tokens"`
		Validators map[string]*Validator `json:"validators"`
		Contracts  map[string]*Contract  `json:"contracts"`
		Unbonding  []*Unbonding          `json:"unbonding"`
		Supply     Supply                `json:"supply"`
		Params     types.Params          `json:"params"`
		BaseFee    uint64                `json:"base_fee"`
	}{s.Accounts, s.Tokens, s.Validators, s.Contracts, s.Unbonding, s.Supply, s.Params, s.BaseFee})
	return crypto.HashHex(b)
}

func (s *State) Root() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rootLocked()
}

func (s *State) Bytes() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, _ := json.Marshal(s)
	return b
}

func (s *State) Restore(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tmp := New()
	if err := json.Unmarshal(data, tmp); err != nil {
		return err
	}
	s.Accounts = tmp.Accounts
	s.Tokens = tmp.Tokens
	s.Validators = tmp.Validators
	s.Contracts = tmp.Contracts
	s.Unbonding = tmp.Unbonding
	s.Supply = tmp.Supply
	s.Params = tmp.Params
	s.BaseFee = tmp.BaseFee
	s.Height = tmp.Height
	s.LastHash = tmp.LastHash
	return nil
}
