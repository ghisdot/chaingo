// Package state implémente la machine d'état de ChainGO : comptes,
// tokens, validateurs, supply (mint/burn) et sélection du proposeur.
package state

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"chaingo/internal/crypto"
	"chaingo/internal/smt"
	"chaingo/internal/stark"
	"chaingo/internal/types"
	"chaingo/internal/wasmvm"
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
	// Inactivité : Missed = slots de proposeur manqués consécutifs (remis à
	// 0 dès qu'il produit). Jailed = exclu du set actif jusqu'à JailedUntil.
	Missed      uint64 `json:"missed,omitempty"`
	Jailed      bool   `json:"jailed,omitempty"`
	JailedUntil int64  `json:"jailed_until,omitempty"`
	// Profil public optionnel (nom/site/description) défini par tx
	// validator_profile. omitempty → racine inchangée tant qu'aucun profil n'est posé.
	Profile string `json:"profile,omitempty"`
}

// weight : poids brut (stake + délégations).
func (v *Validator) weight() uint64 { return v.Stake + v.Delegated }

// activeWeight : poids comptant pour le consensus — 0 si jailé (ni tirage
// proposeur, ni pouvoir de finalité tant que jailé).
func (v *Validator) activeWeight() uint64 {
	if v.Jailed {
		return 0
	}
	return v.Stake + v.Delegated
}

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

// MultisigProposal : un paiement proposé depuis un coffre multisig OU une
// trésorerie DAO. Multisig : exécuté dès que `Threshold` signataires l'ont
// approuvé. DAO : `Approvals` = votes POUR, `Against` = votes CONTRE ; exécuté
// au quorum POUR, rejeté si le quorum POUR ne peut plus être atteint.
type MultisigProposal struct {
	To        string   `json:"to"`
	Amount    uint64   `json:"amount"`
	Approvals []string `json:"approvals"`
	Against   []string `json:"against,omitempty"` // DAO : votes CONTRE
	Executed  bool     `json:"executed"`
	Rejected  bool     `json:"rejected,omitempty"` // DAO : proposition rejetée
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
	// multisig :
	Signers   []string            `json:"signers,omitempty"`
	Threshold uint64              `json:"threshold,omitempty"`
	Proposals []*MultisigProposal `json:"proposals,omitempty"`
	Status    string              `json:"status"` // active | completed | refunded | cancelled
	CreatedAt uint64              `json:"created_at_height"`
	// presale : prix en ucgo par unité de base du token vendu.
	Price uint64 `json:"price,omitempty"`
	// airdrop : destinataires ayant déjà réclamé leur part.
	Claimed []string `json:"claimed,omitempty"`
}

// streamingVested : montant acquis à blockTime pour un flux linéaire entre
// StartMs et EndMs (identique au vesting linéaire).
func streamingVested(c *Contract, blockTime int64) uint64 {
	switch {
	case blockTime <= c.StartMs:
		return 0
	case blockTime >= c.EndMs:
		return c.Amount
	default:
		return types.MulDiv(c.Amount, uint64(blockTime-c.StartMs), uint64(c.EndMs-c.StartMs))
	}
}

func (c *Contract) hasClaimed(addr string) bool {
	for _, a := range c.Claimed {
		if a == addr {
			return true
		}
	}
	return false
}

func (c *Contract) isSigner(addr string) bool {
	for _, s := range c.Signers {
		if s == addr {
			return true
		}
	}
	return false
}

// WasmContract : un contrat WASM ARBITRAIRE déployé on-chain (≠ template no-code).
// Le bytecode est validé au déploiement (instrumentable → arrêt garanti par gas).
// Storage = KV propre au contrat ; Balance = CGO détenus par le contrat (reçus
// via la `value` des appels, dépensés via transfer). Tout est dans la racine
// d'état : json trie les clés de map et encode []byte en base64 → déterministe.
type WasmContract struct {
	Address   string            `json:"address"` // = hash de la tx de déploiement
	Code      []byte            `json:"code"`    // bytecode validé
	Storage   map[string][]byte `json:"storage"` // stockage clé→valeur du contrat
	Balance   uint64            `json:"balance"` // solde CGO du contrat
	Creator   string            `json:"creator"`
	Calls     uint64            `json:"calls"` // nombre d'appels réussis (info/explorateur)
	CreatedAt uint64            `json:"created_at_height"`
}

// ShieldedPool est le POOL BLINDÉ (zk-STARK maison, étage 5) : l'ensemble des
// notes (engagements) déposées, leur racine de Merkle Poseidon, les nullifiers
// déjà dépensés (anti double-dépense) et le solde public verrouillé.
//
// PORTÉE / CAPACITÉ : le circuit de dépense fixe la profondeur d'arbre à
// stark.SpendDepth() (= 12), soit AU PLUS 2^SpendDepth notes (4096). Au-delà, la
// racine du pool ne correspondrait plus à un arbre dépensable
// par le circuit. C'est une borne ASSUMÉE du prototype (à élargir avec un arbre
// incrémental / une profondeur supérieure dans une version auditée).
//
// DÉTERMINISME : Commitments/Notes sont des slices ordonnés (insertion), Root est
// recalculée par poolRootLocked (Poseidon Merkle, padding fixe), Nullifiers est
// une map[string]bool (json trie ses clés). Aucun time/rand dans ce chemin.
type ShieldedPool struct {
	Commitments [][]byte        `json:"commitments"`          // engagements de notes (cm sérialisés, [4]Felt = 32 octets)
	Root        []byte          `json:"root"`                 // racine Merkle Poseidon courante (32 octets), recalculée à chaque mutation
	Nullifiers  map[string]bool `json:"nullifiers"`           // nullifiers dépensés (clé = hex), anti double-dépense
	Notes       [][]byte        `json:"notes,omitempty"`      // blobs chiffrés (opaques au consensus), parallèles aux Commitments
	Balance     uint64          `json:"balance"`              // CGO publics verrouillés dans le pool
}

type State struct {
	mu         sync.RWMutex
	Accounts      map[string]*Account      `json:"accounts"`
	Tokens        map[string]*Token        `json:"tokens"`
	Validators    map[string]*Validator    `json:"validators"`
	Contracts     map[string]*Contract     `json:"contracts"`
	WasmContracts map[string]*WasmContract `json:"wasm_contracts,omitempty"`
	// Shielded : pool blindé (étage 5). omitempty + nil par défaut => quand le
	// pool n'a jamais servi (mainnet, ou réseau privacy-off), il est ABSENT du
	// JSON, donc la racine d'état est OCTET-POUR-OCTET identique à avant l'ajout
	// du champ : les chaînes existantes ne forkent pas.
	Shielded *ShieldedPool `json:"shielded,omitempty"`
	// Slashed : équivocations déjà punies (clé "voter@height") — garantit
	// qu'une même faute n'est slashée qu'une fois, même si plusieurs nœuds
	// l'incluent dans des blocs.
	Slashed   map[string]bool `json:"slashed"`
	Unbonding []*Unbonding    `json:"unbonding"`
	Supply    Supply          `json:"supply"`
	Params    types.Params    `json:"params"`
	BaseFee   uint64          `json:"base_fee"` // base fee courant (EIP-1559)
	Height    uint64          `json:"height"`
	LastHash  string          `json:"last_hash"`
	// FinalizedHeight : dernière hauteur finalisée par un commit ≥ 2/3 porté
	// dans un bloc. Persistée (survit au redémarrage), mais HORS de la racine
	// d'état (déjà couverte par les blocs via LastCommitRoot).
	FinalizedHeight uint64 `json:"finalized_height"`
}

func New() *State {
	return &State{
		Accounts:      map[string]*Account{},
		Tokens:        map[string]*Token{},
		Validators:    map[string]*Validator{},
		Contracts:     map[string]*Contract{},
		WasmContracts: map[string]*WasmContract{},
		Slashed:       map[string]bool{},
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

// ListWasmContracts renvoie les contrats WASM déployés (copie superficielle —
// le bytecode n'est PAS recopié, ne pas muter). Triés du plus récent au plus ancien.
func (s *State) ListWasmContracts() []*WasmContract {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*WasmContract, 0, len(s.WasmContracts))
	for _, c := range s.WasmContracts {
		cp := *c
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

// GetWasmContract renvoie un contrat WASM par adresse (copie superficielle), ou nil.
func (s *State) GetWasmContract(addr string) *WasmContract {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.WasmContracts[addr]
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

// ---- pouvoir de vote (finalité BFT) ----

// PowerOf : pouvoir de vote d'un validateur = stake propre + délégations.
func (s *State) PowerOf(addr string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.Validators[addr]; ok {
		return v.activeWeight()
	}
	return 0
}

// TotalPower : somme des pouvoirs de vote de tous les validateurs actifs.
func (s *State) TotalPower() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalStakedLocked()
}

// ValidatorSet : photo IMMUABLE du pouvoir de vote actif à un instant donné.
// Sert à figer le set qui gouverne les votes BFT d'une hauteur (le 2/3 doit se
// mesurer contre un dénominateur stable, identique sur tous les nœuds — sinon
// un commit légitime peut être rejeté quand l'état évolue). Voir
// docs/design/phase2-validator-set-freeze.md.
type ValidatorSet struct {
	Powers map[string]uint64 // adresse -> pouvoir actif (stake + délégations)
	Total  uint64            // somme des pouvoirs (dénominateur du quorum)
}

// PowerOf : pouvoir figé d'un votant (0 s'il n'était pas dans ce set).
func (vs *ValidatorSet) PowerOf(addr string) uint64 {
	if vs == nil {
		return 0
	}
	return vs.Powers[addr]
}

// SnapshotActiveSet renvoie une photo du set de validateurs actifs (pouvoir
// > 0) à l'instant de l'appel. La map est une copie : modifier l'état ensuite
// n'altère pas la photo.
func (s *State) SnapshotActiveSet() *ValidatorSet {
	s.mu.RLock()
	defer s.mu.RUnlock()
	vs := &ValidatorSet{Powers: make(map[string]uint64, len(s.Validators))}
	for _, v := range s.Validators {
		if w := v.activeWeight(); w > 0 {
			vs.Powers[v.Address] = w
			vs.Total += w
		}
	}
	return vs
}

// IsSlashed : l'équivocation (voter, height) a-t-elle déjà été punie ?
func (s *State) IsSlashed(voter string, height uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Slashed[slashKey(voter, height)]
}

// SetFinalized avance la hauteur finalisée (monotone). Appelé après
// vérification d'un commit ≥ 2/3 porté par un bloc.
func (s *State) SetFinalized(height uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if height > s.FinalizedHeight {
		s.FinalizedHeight = height
	}
}

func (s *State) GetFinalized() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.FinalizedHeight
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
		if w := v.activeWeight(); w > 0 {
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
func (s *State) Execute(txs []*types.Transaction, evidence []*types.DoubleSignEvidence, absent []string, proposer string, blockTime int64, strict bool) (applied []*types.Transaction, failed map[string]error, root string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.releaseUnbondedLocked(blockTime)

	// Slashing déterministe AVANT les txs : une faute punie au plus une
	// fois (marqueur Slashed). L'ordre est celui de la liste du bloc, mais
	// le résultat est indépendant de l'ordre (chaque slash est isolé).
	for _, ev := range evidence {
		s.slashLocked(ev.Voter, ev.Height, blockTime)
	}
	// Inactivité : comptage déterministe des slots manqués (jail au seuil).
	if proposer != "" {
		s.applyDowntimeLocked(proposer, absent, blockTime)
	}

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

// totalStakedLocked : poids actif total (jailés exclus) — base du quorum
// de finalité et de l'émission.
func (s *State) totalStakedLocked() uint64 {
	var total uint64
	for _, v := range s.Validators {
		total += v.activeWeight()
	}
	return total
}

func slashKey(voter string, height uint64) string {
	return voter + "@" + strconv.FormatUint(height, 10)
}

// slashLocked punit une équivocation : SlashDoubleSignBps du stake propre
// ET de chaque délégation du validateur sont BRÛLÉS (déflationniste).
// Idempotent via le marqueur Slashed : une faute (voter,height) n'est
// punie qu'une fois, peu importe combien de blocs portent la preuve.
func (s *State) slashLocked(voter string, height uint64, blockTime int64) {
	key := slashKey(voter, height)
	if s.Slashed == nil {
		s.Slashed = map[string]bool{}
	}
	if s.Slashed[key] {
		return
	}
	s.Slashed[key] = true

	v, ok := s.Validators[voter]
	if !ok {
		return // validateur déjà sorti — rien à slasher
	}
	s.slashWeightLocked(v, s.Params.SlashDoubleSignBps)

	// Validateur entièrement slashé (bps=100 %) : on le sort et on libère
	// le reliquat des délégations en unbonding.
	if v.Stake == 0 {
		for a, amt := range v.Delegators {
			s.Unbonding = append(s.Unbonding, &Unbonding{Address: a, Amount: amt, ReleaseAt: blockTime + s.Params.UnbondingSeconds*1000})
		}
		delete(s.Validators, voter)
	}
}

// slashWeightLocked brûle `bps` du stake propre ET de chaque délégation du
// validateur (déterministe, ordre trié). Réutilisé par le slash de
// double-signature et celui d'inactivité.
func (s *State) slashWeightLocked(v *Validator, bps uint64) {
	cut := types.MulDiv(v.Stake, bps, 10_000)
	v.Stake -= cut
	if a, ok := s.Accounts[v.Address]; ok {
		if a.Staked >= cut {
			a.Staked -= cut
		} else {
			a.Staked = 0
		}
	}
	burn := cut
	if len(v.Delegators) > 0 {
		addrs := make([]string, 0, len(v.Delegators))
		for a := range v.Delegators {
			addrs = append(addrs, a)
		}
		sort.Strings(addrs)
		for _, a := range addrs {
			dcut := types.MulDiv(v.Delegators[a], bps, 10_000)
			v.Delegators[a] -= dcut
			v.Delegated -= dcut
			burn += dcut
		}
	}
	s.Supply.Total -= burn
	s.Supply.Burned += burn
}

// applyDowntimeLocked : compte les slots de proposeur manqués. Le proposeur
// effectif voit son compteur remis à zéro ; chaque proposeur élu d'un round
// de secours sauté (`absent`) prend une absence. Au seuil → jail + slash.
func (s *State) applyDowntimeLocked(proposer string, absent []string, blockTime int64) {
	if v, ok := s.Validators[proposer]; ok {
		v.Missed = 0
	}
	for _, addr := range absent {
		v, ok := s.Validators[addr]
		if !ok || v.Jailed {
			continue
		}
		v.Missed++
		if v.Missed >= s.Params.DowntimeJailThreshold {
			s.slashWeightLocked(v, s.Params.SlashDowntimeBps)
			v.Jailed = true
			v.JailedUntil = blockTime + s.Params.JailSeconds*1000
			v.Missed = 0
		}
	}
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
	if tx.Type == types.TxWasmDeploy {
		burn += s.Params.WasmDeployFee
	}
	if tx.Type == types.TxWasmCall {
		burn += s.Params.WasmCallFee
	}
	// Frais réseau d'une tx blindée (brûlé, en plus du base fee) — payé en CGO
	// public par l'émetteur pour les trois types de tx du pool.
	if tx.Type == types.TxShield || tx.Type == types.TxShieldedTransfer || tx.Type == types.TxUnshield {
		burn += s.Params.ShieldFee
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
	// presale : l'acheteur engage tx.Amount CGO (le coût réel ≤ ce montant est
	// débité dans le handler ; on s'assure ici qu'il en dispose).
	if tx.Type == types.TxContractExec && tx.Action == types.ActionBuy {
		needNative += tx.Amount
	}
	if tx.Type == types.TxWasmCall {
		needNative += tx.Amount // value (CGO) envoyée au contrat
	}
	if tx.Type == types.TxShield {
		needNative += tx.Amount // CGO publics déposés dans le pool blindé
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
		// Plafond max-supply (0 = illimité) : le mint ne peut le dépasser. Garde
		// aussi contre le débordement uint64.
		if t.TotalSupply+tx.Amount < t.TotalSupply {
			return errors.New("mint: supply overflow")
		}
		if t.MaxSupply > 0 && t.TotalSupply+tx.Amount > t.MaxSupply {
			return fmt.Errorf("mint: would exceed max supply (%d)", t.MaxSupply)
		}
		target := tx.From
		if tx.To != "" {
			target = tx.To
		}
		t.TotalSupply += tx.Amount
		s.acct(target).Balances[tx.TokenID] += tx.Amount
	case types.TxBurn:
		t, ok := s.Tokens[tx.TokenID]
		if !ok {
			return fmt.Errorf("unknown token %q", tx.TokenID)
		}
		if !t.Burnable {
			return errors.New("token is not burnable")
		}
		if from.Balances[tx.TokenID] < tx.Amount {
			return errors.New("insufficient token balance to burn")
		}
		from.Balances[tx.TokenID] -= tx.Amount
		t.TotalSupply -= tx.Amount
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
	case types.TxUnjail:
		v, ok := s.Validators[tx.From]
		if !ok || !v.Jailed {
			return errors.New("not a jailed validator")
		}
		if blockTime < v.JailedUntil {
			return fmt.Errorf("still jailed for %d ms", v.JailedUntil-blockTime)
		}
		v.Jailed = false
		v.Missed = 0
	case types.TxValidatorProfile:
		v, ok := s.Validators[tx.From]
		if !ok {
			return errors.New("only a validator can set a profile")
		}
		v.Profile = tx.Memo
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
			Signers:     c.Signers,
			Threshold:   c.Threshold,
			Price:       c.Price,
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
		case c.Template == types.TemplateMultisig && tx.Action == types.ActionPropose:
			if !c.isSigner(tx.From) {
				return errors.New("multisig: only a signer can propose")
			}
			if c.Amount-c.Released < tx.Amount {
				return errors.New("multisig: proposed amount exceeds the vault balance")
			}
			p := &MultisigProposal{To: tx.To, Amount: tx.Amount, Approvals: []string{tx.From}}
			c.Proposals = append(c.Proposals, p)
			s.maybeExecuteMultisig(c, p)
		case c.Template == types.TemplateMultisig && tx.Action == types.ActionApprove:
			if !c.isSigner(tx.From) {
				return errors.New("multisig: only a signer can approve")
			}
			if tx.Proposal >= uint64(len(c.Proposals)) {
				return fmt.Errorf("multisig: unknown proposal %d", tx.Proposal)
			}
			p := c.Proposals[tx.Proposal]
			if p.Executed {
				return errors.New("multisig: proposal already executed")
			}
			for _, a := range p.Approvals {
				if a == tx.From {
					return errors.New("multisig: already approved")
				}
			}
			p.Approvals = append(p.Approvals, tx.From)
			s.maybeExecuteMultisig(c, p)
		case c.Template == types.TemplateDAO && tx.Action == types.ActionPropose:
			if !c.isSigner(tx.From) {
				return errors.New("dao: only a member can propose")
			}
			if c.Amount-c.Released < tx.Amount {
				return errors.New("dao: proposed amount exceeds the treasury balance")
			}
			// Le proposant vote POUR d'office.
			p := &MultisigProposal{To: tx.To, Amount: tx.Amount, Approvals: []string{tx.From}}
			c.Proposals = append(c.Proposals, p)
			s.maybeResolveDAO(c, p)
		case c.Template == types.TemplateDAO && (tx.Action == types.ActionApprove || tx.Action == types.ActionReject):
			if !c.isSigner(tx.From) {
				return errors.New("dao: only a member can vote")
			}
			if tx.Proposal >= uint64(len(c.Proposals)) {
				return fmt.Errorf("dao: unknown proposal %d", tx.Proposal)
			}
			p := c.Proposals[tx.Proposal]
			if p.Executed || p.Rejected {
				return errors.New("dao: proposal already resolved")
			}
			if proposalHasVoter(p, tx.From) {
				return errors.New("dao: already voted on this proposal")
			}
			if tx.Action == types.ActionApprove {
				p.Approvals = append(p.Approvals, tx.From)
			} else {
				p.Against = append(p.Against, tx.From)
			}
			s.maybeResolveDAO(c, p)
		case c.Template == types.TemplateTimelock && tx.Action == types.ActionClaim:
			if tx.From != c.Beneficiary {
				return errors.New("timelock: only the beneficiary can claim")
			}
			if blockTime < c.EndMs {
				return errors.New("timelock: funds are still locked")
			}
			s.acct(c.Beneficiary).Balances[c.TokenID] += c.Amount - c.Released
			c.Released = c.Amount
			c.Status = "completed"
		case c.Template == types.TemplateStreaming && tx.Action == types.ActionClaim:
			if tx.From != c.Beneficiary {
				return errors.New("streaming: only the beneficiary can claim")
			}
			claimable := streamingVested(c, blockTime) - c.Released
			if claimable == 0 {
				return errors.New("streaming: nothing streamed yet")
			}
			s.acct(c.Beneficiary).Balances[c.TokenID] += claimable
			c.Released += claimable
			if c.Released == c.Amount {
				c.Status = "completed"
			}
		case c.Template == types.TemplateStreaming && tx.Action == types.ActionCancel:
			if tx.From != c.Creator {
				return errors.New("streaming: only the creator can cancel")
			}
			// Le bénéficiaire reçoit l'acquis non encore réclamé ; le créateur
			// récupère le reste non acquis.
			vested := streamingVested(c, blockTime)
			s.acct(c.Beneficiary).Balances[c.TokenID] += vested - c.Released
			s.acct(c.Creator).Balances[c.TokenID] += c.Amount - vested
			c.Released = c.Amount
			c.Status = "cancelled"
		case c.Template == types.TemplateAirdrop && tx.Action == types.ActionClaim:
			if !c.isSigner(tx.From) {
				return errors.New("airdrop: caller is not a recipient")
			}
			if c.hasClaimed(tx.From) {
				return errors.New("airdrop: already claimed")
			}
			share := c.Amount / uint64(len(c.Signers))
			s.acct(tx.From).Balances[c.TokenID] += share
			c.Released += share
			c.Claimed = append(c.Claimed, tx.From)
			if len(c.Claimed) == len(c.Signers) {
				// Tous ont réclamé : la poussière (Amount - N·share) revient au créateur.
				if dust := c.Amount - c.Released; dust > 0 {
					s.acct(c.Creator).Balances[c.TokenID] += dust
					c.Released = c.Amount
				}
				c.Status = "completed"
			}
		case c.Template == types.TemplatePresale && tx.Action == types.ActionBuy:
			// L'acheteur envoie tx.Amount CGO ; il reçoit floor(montant/prix) unités
			// de base du token, et ne paie QUE le coût exact (le reste lui reste).
			tokensOut := tx.Amount / c.Price
			if tokensOut == 0 {
				return errors.New("presale: amount too small to buy 1 token unit")
			}
			if tokensOut > c.Amount-c.Released {
				return errors.New("presale: not enough inventory left")
			}
			cost := tokensOut * c.Price
			if from.Balances[types.NativeToken] < cost {
				return errors.New("presale: insufficient CGO balance")
			}
			from.Balances[types.NativeToken] -= cost          // l'acheteur paie en CGO
			s.acct(c.Creator).Balances[types.NativeToken] += cost // le créateur encaisse
			s.acct(tx.From).Balances[c.TokenID] += tokensOut   // l'acheteur reçoit le token
			c.Released += tokensOut
			if c.Released == c.Amount {
				c.Status = "completed"
			}
		case c.Template == types.TemplatePresale && tx.Action == types.ActionCancel:
			if tx.From != c.Creator {
				return errors.New("presale: only the creator can close")
			}
			// Le créateur clôt la vente et récupère l'inventaire invendu.
			s.acct(c.Creator).Balances[c.TokenID] += c.Amount - c.Released
			c.Released = c.Amount
			c.Status = "cancelled"
		default:
			return fmt.Errorf("action %q not valid for template %q", tx.Action, c.Template)
		}
	case types.TxShield:
		// GATE EN PREMIER (verrou de sûreté du système de preuve maison).
		if !s.Params.PrivacyEnabled {
			return errors.New("shielded pool disabled on this network (params.privacy_enabled=false)")
		}
		// --- VALIDATION COMPLÈTE AVANT TOUTE MUTATION (atomicité) ---
		// On ne crée PAS encore le pool : tout est calculé sur des copies locales,
		// de sorte qu'un échec ne laisse aucune trace (ni pool vide, ni solde
		// modifié). C'est crucial en mode non-strict (proposeur) où une tx en échec
		// est simplement abandonnée sans rollback.
		if _, derr := cmToDigest(tx.ShieldCommitment); derr != nil {
			return fmt.Errorf("shield: %w", derr)
		}
		// BORNE DE RANGE : le montant déposé devient la valeur (cachée) de la note.
		// Au-delà de 2^RangeBits, le circuit ne pourra JAMAIS en prouver la dépense
		// (range-proof insatisfaisable) => note définitivement indépensable. On
		// refuse en amont plutôt que de verrouiller des fonds.
		if tx.Amount >= stark.MaxNoteValue() {
			return fmt.Errorf("shield: montant %d hors borne de range (max %d)", tx.Amount, stark.MaxNoteValue()-1)
		}
		var curCommits [][]byte
		if s.Shielded != nil {
			curCommits = s.Shielded.Commitments
		}
		// Capacité : l'arbre du circuit a 2^SpendDepth feuilles. On refuse d'insérer
		// au-delà (sinon la racine ne serait plus dépensable par le circuit).
		capacity := 1 << uint(stark.SpendDepth())
		if len(curCommits)+1 > capacity {
			return fmt.Errorf("shield: pool plein (capacité %d notes)", capacity)
		}
		newCommits := append(append([][]byte(nil), curCommits...), append([]byte(nil), tx.ShieldCommitment...))
		newRoot, rerr := poolRoot(newCommits)
		if rerr != nil {
			return fmt.Errorf("shield: %w", rerr)
		}
		// --- MUTATION (tout est validé) — c'est seulement ICI que le pool naît. ---
		pool := s.ensureShieldedLocked()
		from.Balances[types.NativeToken] -= tx.Amount
		pool.Balance += tx.Amount
		pool.Commitments = newCommits
		pool.Notes = append(pool.Notes, append([]byte(nil), tx.ShieldNote...))
		pool.Root = newRoot
	case types.TxShieldedTransfer:
		if !s.Params.PrivacyEnabled {
			return errors.New("shielded pool disabled on this network (params.privacy_enabled=false)")
		}
		// --- VALIDATION (avant toute mutation) ---
		public, proof, perr := s.verifySpendLocked(tx)
		if perr != nil {
			return fmt.Errorf("shielded_transfer: %w", perr)
		}
		_ = proof
		pool := s.Shielded // verifySpendLocked garantit pool != nil
		fee := public.Fee.Uint64()
		if pool.Balance < fee {
			return errors.New("shielded_transfer: pool balance below proof fee")
		}
		// Les N notes de sortie (OutCms) sont insérées dans l'arbre : capacité.
		capacity := 1 << uint(stark.SpendDepth())
		if len(pool.Commitments)+len(public.OutCms) > capacity {
			return fmt.Errorf("shielded_transfer: pool plein (capacité %d notes)", capacity)
		}
		newCommits := append([][]byte(nil), pool.Commitments...)
		for _, oc := range public.OutCms {
			newCommits = append(newCommits, digestToBytes(oc))
		}
		newRoot, rerr := poolRoot(newCommits)
		if rerr != nil {
			return fmt.Errorf("shielded_transfer: %w", rerr)
		}
		// --- MUTATION ---
		for _, nf := range public.Nfs {
			pool.Nullifiers[nullifierKey(nf)] = true
		}
		pool.Commitments = newCommits
		pool.Notes = append(pool.Notes, append([]byte(nil), tx.ShieldNote...))
		pool.Root = newRoot
		// Fee (seul montant public) brûlé depuis le pool : la valeur quitte la
		// supply. Les montants des notes restent cachés dans le pool.
		pool.Balance -= fee
		s.Supply.Total -= fee
		s.Supply.Burned += fee
	case types.TxUnshield:
		if !s.Params.PrivacyEnabled {
			return errors.New("shielded pool disabled on this network (params.privacy_enabled=false)")
		}
		// --- VALIDATION (avant toute mutation) ---
		public, proof, perr := s.verifySpendLocked(tx)
		if perr != nil {
			return fmt.Errorf("unshield: %w", perr)
		}
		_ = proof
		pool := s.Shielded
		amount := public.Fee.Uint64() // ici, Fee = montant public RENDU (pas brûlé)
		if pool.Balance < amount {
			return errors.New("unshield: pool balance below public amount")
		}
		// Les N notes de change (OutCms) sont réinsérées : capacité.
		capacity := 1 << uint(stark.SpendDepth())
		if len(pool.Commitments)+len(public.OutCms) > capacity {
			return fmt.Errorf("unshield: pool plein (capacité %d notes)", capacity)
		}
		newCommits := append([][]byte(nil), pool.Commitments...)
		for _, oc := range public.OutCms {
			newCommits = append(newCommits, digestToBytes(oc))
		}
		newRoot, rerr := poolRoot(newCommits)
		if rerr != nil {
			return fmt.Errorf("unshield: %w", rerr)
		}
		// --- MUTATION ---
		for _, nf := range public.Nfs {
			pool.Nullifiers[nullifierKey(nf)] = true
		}
		pool.Commitments = newCommits
		if len(tx.ShieldNote) > 0 {
			pool.Notes = append(pool.Notes, append([]byte(nil), tx.ShieldNote...))
		}
		pool.Root = newRoot
		// Le montant public sort du pool vers To en CGO public (déplacement, pas
		// de burn : la supply est inchangée). Le ShieldFee réseau, lui, est déjà
		// brûlé depuis le compte émetteur (cf. section frais).
		pool.Balance -= amount
		s.acct(tx.To).Balances[types.NativeToken] += amount
	case types.TxWasmDeploy:
		if !s.Params.WasmEnabled {
			return errors.New("wasm contracts disabled on this network (params.wasm_enabled=false)")
		}
		if uint64(len(tx.Code)) > s.Params.WasmMaxCodeLen {
			return fmt.Errorf("wasm code too large: %d > %d bytes", len(tx.Code), s.Params.WasmMaxCodeLen)
		}
		// Garde de déploiement : le bytecode doit être instrumentable (arrêt
		// garanti par gas) et restreint au sous-ensemble d'opcodes déterministe.
		if err := wasmvm.Validate(tx.Code); err != nil {
			return err
		}
		if s.WasmContracts == nil {
			s.WasmContracts = map[string]*WasmContract{}
		}
		addr := tx.Hash()
		if _, exists := s.WasmContracts[addr]; exists {
			return errors.New("wasm contract already exists")
		}
		s.WasmContracts[addr] = &WasmContract{
			Address:   addr,
			Code:      append([]byte(nil), tx.Code...),
			Storage:   map[string][]byte{},
			Creator:   tx.From,
			CreatedAt: s.Height + 1,
		}
	case types.TxWasmCall:
		if !s.Params.WasmEnabled {
			return errors.New("wasm contracts disabled on this network (params.wasm_enabled=false)")
		}
		wc, ok := s.WasmContracts[tx.ContractID]
		if !ok {
			return fmt.Errorf("unknown wasm contract %q", tx.ContractID)
		}
		value := tx.Amount
		// Sandbox seedé depuis l'ÉTAT RÉEL (copie du storage du contrat, solde +
		// value reçue). RunDeterministic force l'interpréteur wazero (même chemin
		// d'exécution sur toute architecture) — couplé au gas déterministe, c'est
		// reproductible bit-à-bit sur tous les nœuds.
		sb := wasmvm.NewSandbox(tx.From, int64(value))
		sb.Balance = int64(wc.Balance + value)
		for k, v := range wc.Storage {
			sb.Storage[k] = append([]byte(nil), v...)
		}
		gas := s.Params.WasmGasLimit
		if tx.Gas > 0 && tx.Gas < gas {
			gas = tx.Gas
		}
		_, rerr := sb.RunDeterministic(context.Background(), wc.Code, tx.Action, int64(gas), tx.Args...)
		if rerr == nil {
			// Succès : on COMMIT les effets sur l'état réel, atomiquement.
			from.Balances[types.NativeToken] -= value
			wc.Storage = make(map[string][]byte, len(sb.Storage))
			for k, v := range sb.Storage {
				wc.Storage[k] = append([]byte(nil), v...)
			}
			for _, t := range sb.Transfers {
				if t.Amount > 0 {
					s.acct(t.To).Balances[types.NativeToken] += uint64(t.Amount)
				}
			}
			wc.Balance = uint64(sb.Balance)
			wc.Calls++
		}
		// Trap / out-of-gas (rerr != nil) : effets du contrat IGNORÉS, value non
		// débitée — mais les frais d'appel restent brûlés (l'émetteur paie sa
		// tentative : anti-DoS). Commit-ou-non est DÉTERMINISTE (même décision
		// sur tous les nœuds).
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

// maybeExecuteMultisig exécute une proposition dès que le seuil
// d'approbations est atteint et que le coffre a encore les fonds.
func (s *State) maybeExecuteMultisig(c *Contract, p *MultisigProposal) {
	if p.Executed || uint64(len(p.Approvals)) < c.Threshold {
		return
	}
	if c.Amount-c.Released < p.Amount {
		return // d'autres propositions ont déjà épuisé le coffre
	}
	s.acct(p.To).Balances[c.TokenID] += p.Amount
	c.Released += p.Amount
	p.Executed = true
	if c.Released == c.Amount {
		c.Status = "completed"
	}
}

// proposalHasVoter : l'adresse a-t-elle déjà voté (POUR ou CONTRE) ?
func proposalHasVoter(p *MultisigProposal, addr string) bool {
	for _, a := range p.Approvals {
		if a == addr {
			return true
		}
	}
	for _, a := range p.Against {
		if a == addr {
			return true
		}
	}
	return false
}

// maybeResolveDAO tranche une proposition DAO : l'EXÉCUTE si le quorum de votes
// POUR (Threshold) est atteint et la trésorerie a les fonds ; la REJETTE si le
// quorum POUR ne peut plus être atteint (trop de votes CONTRE). Déterministe.
func (s *State) maybeResolveDAO(c *Contract, p *MultisigProposal) {
	if p.Executed || p.Rejected {
		return
	}
	if uint64(len(p.Approvals)) >= c.Threshold {
		if c.Amount-c.Released < p.Amount {
			return // trésorerie épuisée par d'autres propositions
		}
		s.acct(p.To).Balances[c.TokenID] += p.Amount
		c.Released += p.Amount
		p.Executed = true
		if c.Released == c.Amount {
			c.Status = "completed"
		}
		return
	}
	// POUR max atteignable = membres - (votes CONTRE). S'il passe sous le quorum,
	// la proposition ne pourra jamais aboutir → rejet.
	members := uint64(len(c.Signers))
	if members-uint64(len(p.Against)) < c.Threshold {
		p.Rejected = true
	}
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

// BootstrapVesting verrouille des fonds à la genèse dans un contrat de
// vesting (parts équipe/trésorerie). Les fonds entrent dans la supply mais
// ne sont dans aucun solde : le bénéficiaire les réclame au fil du temps
// via `contract claim`, comme un vesting créé par transaction.
func (s *State) BootstrapVesting(id, beneficiary string, amount uint64, startMs, endMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Supply.Total += amount
	s.Supply.Minted += amount
	s.Contracts[id] = &Contract{
		ID:          id,
		Template:    types.TemplateVesting,
		Creator:     "genesis",
		TokenID:     types.NativeToken,
		Amount:      amount,
		Beneficiary: beneficiary,
		StartMs:     startMs,
		EndMs:       endMs,
		Status:      "active",
		CreatedAt:   0,
	}
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

// ---- pool blindé (étage 5) ----

// poseidonDigestBytes : taille d'un digest [4]Felt sérialisé (4 × 8 octets BE).
const poseidonDigestBytes = 4 * 8 // 32

// ensureShieldedLocked initialise le pool blindé à la PREMIÈRE utilisation
// (lazy). Tant qu'aucune tx blindée n'a été appliquée, s.Shielded reste nil =>
// absent du JSON => racine d'état inchangée pour les chaînes existantes.
func (s *State) ensureShieldedLocked() *ShieldedPool {
	if s.Shielded == nil {
		s.Shielded = &ShieldedPool{Nullifiers: map[string]bool{}}
	}
	if s.Shielded.Nullifiers == nil {
		s.Shielded.Nullifiers = map[string]bool{}
	}
	return s.Shielded
}

// cmToDigest décode un engagement sérialisé (32 octets = 4 Felt big-endian) en
// digest [4]Felt. Renvoie une erreur si la taille est mauvaise (refus propre,
// jamais de panique).
func cmToDigest(b []byte) ([4]stark.Felt, error) {
	var d [4]stark.Felt
	if len(b) != poseidonDigestBytes {
		return d, fmt.Errorf("commitment de taille %d, attendu %d", len(b), poseidonDigestBytes)
	}
	for k := 0; k < 4; k++ {
		d[k] = stark.FeltFromBytes(b[k*8 : k*8+8])
	}
	return d, nil
}

// digestToBytes sérialise un digest [4]Felt en 32 octets big-endian (inverse de
// cmToDigest).
func digestToBytes(d [4]stark.Felt) []byte {
	out := make([]byte, 0, poseidonDigestBytes)
	for k := 0; k < 4; k++ {
		out = append(out, d[k].Bytes()...)
	}
	return out
}

// poolRoot recalcule la racine de Merkle POSEIDON des engagements du pool. Les
// feuilles sont COMPLÉTÉES à EXACTEMENT 2^SpendDepth (feuille de padding fixe =
// digest nul) : la profondeur de l'arbre vaut donc toujours stark.SpendDepth(),
// ce qui est la profondeur que le circuit de dépense sait prouver. Un wallet qui
// reconstruit l'arbre des MÊMES engagements (même padding) obtient la MÊME
// racine, donc sa preuve vérifie SpendPublic.MerkleRoot == pool.Root.
//
// DÉTERMINISME : ordre d'insertion des engagements + padding fixe ; PoseidonCommit
// est pur. Renvoie une erreur si un engagement est mal formé (taille).
func poolRoot(commitments [][]byte) ([]byte, error) {
	full := 1 << uint(stark.SpendDepth()) // nombre de feuilles de l'arbre (2^profondeur)
	if len(commitments) > full {
		return nil, fmt.Errorf("pool plein : %d notes > capacité %d (profondeur %d)",
			len(commitments), full, stark.SpendDepth())
	}
	leaves := make([][4]stark.Felt, full)
	for i, cm := range commitments {
		d, err := cmToDigest(cm)
		if err != nil {
			return nil, fmt.Errorf("commitment %d: %w", i, err)
		}
		leaves[i] = d
	}
	// Les emplacements [len(commitments), full) restent à la feuille de padding
	// (digest nul = [4]Felt zéro), valeur déterministe et connue du wallet.
	root, _ := stark.PoseidonCommit(leaves)
	return digestToBytes(root), nil
}

// GetShieldedPool renvoie une COPIE profonde du pool blindé (ou nil si jamais
// utilisé) — pour l'API/explorateur. La copie évite toute mutation concurrente
// de l'état réel par l'appelant.
func (s *State) GetShieldedPool() *ShieldedPool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Shielded == nil {
		return nil
	}
	cp := &ShieldedPool{
		Balance:    s.Shielded.Balance,
		Root:       append([]byte(nil), s.Shielded.Root...),
		Nullifiers: make(map[string]bool, len(s.Shielded.Nullifiers)),
	}
	for _, c := range s.Shielded.Commitments {
		cp.Commitments = append(cp.Commitments, append([]byte(nil), c...))
	}
	for _, n := range s.Shielded.Notes {
		cp.Notes = append(cp.Notes, append([]byte(nil), n...))
	}
	for k, v := range s.Shielded.Nullifiers {
		cp.Nullifiers[k] = v
	}
	return cp
}

// nullifierKey dérive la clé (hex) d'un nullifier [4]Felt pour l'index
// Nullifiers. Déterministe (big-endian via digestToBytes).
func nullifierKey(nf [4]stark.Felt) string {
	return crypto.HashHex(digestToBytes(nf))
}

// verifySpendLocked effectue TOUTE la vérification d'une dépense blindée
// (shielded_transfer / unshield), SANS muter l'état :
//   - décode SpendPublic puis SpendProof (formats bornés, jamais de panique) ;
//   - VÉRIFIE la preuve zk-STARK (VerifySpend) — le cœur de la soundness ;
//   - exige que la racine prouvée == la racine COURANTE du pool (la note dépensée
//     appartient bien à l'arbre actuel) ;
//   - exige que le nullifier ne soit pas déjà dépensé (anti double-dépense).
//
// Renvoie l'énoncé public décodé et la preuve si tout passe ; une erreur sinon.
// Le pool DOIT exister (sinon il n'y a aucune note à dépenser).
func (s *State) verifySpendLocked(tx *types.Transaction) (stark.SpendNPublic, stark.AirProof, error) {
	var public stark.SpendNPublic
	var proof stark.AirProof
	if s.Shielded == nil || len(s.Shielded.Commitments) == 0 {
		return public, proof, errors.New("pool blindé vide : aucune note à dépenser")
	}
	public, err := stark.UnmarshalSpendNPublic(tx.SpendPublic)
	if err != nil {
		return public, proof, fmt.Errorf("spend_public invalide: %w", err)
	}
	proof, err = stark.UnmarshalSpendProof(tx.SpendProof)
	if err != nil {
		return public, proof, fmt.Errorf("spend_proof invalide: %w", err)
	}
	// Vérification cryptographique de la preuve M-entrées/N-sorties (déterministe).
	if !stark.VerifySpendN(public, proof) {
		return public, proof, errors.New("preuve de dépense invalide")
	}
	// La racine prouvée doit être la racine COURANTE du pool : toutes les notes
	// dépensées appartiennent à l'arbre actuel (pas d'un arbre obsolète / forgé).
	if !bytesEqual(digestToBytes(public.MerkleRoot), s.Shielded.Root) {
		return public, proof, errors.New("racine de la preuve != racine courante du pool")
	}
	// Anti double-dépense : chaque nullifier ne doit pas déjà figurer, ET les
	// nullifiers de CETTE tx doivent être DEUX À DEUX DISTINCTS. Sans ce second
	// contrôle, une même note pourrait être utilisée plusieurs fois en entrée d'une
	// même tx (le circuit ne l'interdit pas : il produit alors des nullifiers
	// identiques) — ce qui CRÉERAIT DE LA VALEUR. C'est une garantie portée par la
	// couche état, pas par le circuit.
	seen := make(map[string]bool, len(public.Nfs))
	for _, nf := range public.Nfs {
		key := nullifierKey(nf)
		if s.Shielded.Nullifiers[key] {
			return public, proof, errors.New("nullifier déjà dépensé (double-dépense)")
		}
		if seen[key] {
			return public, proof, errors.New("nullifier en double dans la tx (double-dépense intra-tx)")
		}
		seen[key] = true
	}
	return public, proof, nil
}

// bytesEqual compare deux slices d'octets (évite d'importer bytes pour un seul
// usage ; nil et slice vide sont considérés égaux).
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---- racine d'état & persistance ----

// rootLocked hashes the canonical JSON of the chain state. encoding/json
// sorts map keys, so this is deterministic across nodes. (v1: O(n) per
// block — replaced by a sparse Merkle tree in Phase 2.)
func (s *State) rootLocked() string {
	if s.Params.SparseMerkleRoot {
		return s.sparseRootLocked()
	}
	b, _ := json.Marshal(struct {
		Accounts      map[string]*Account      `json:"accounts"`
		Tokens        map[string]*Token        `json:"tokens"`
		Validators    map[string]*Validator    `json:"validators"`
		Contracts     map[string]*Contract     `json:"contracts"`
		WasmContracts map[string]*WasmContract `json:"wasm_contracts,omitempty"`
		Shielded      *ShieldedPool            `json:"shielded,omitempty"`
		Slashed       map[string]bool          `json:"slashed"`
		Unbonding     []*Unbonding             `json:"unbonding"`
		Supply        Supply                   `json:"supply"`
		Params        types.Params             `json:"params"`
		BaseFee       uint64                   `json:"base_fee"`
	}{s.Accounts, s.Tokens, s.Validators, s.Contracts, s.WasmContracts, s.Shielded, s.Slashed, s.Unbonding, s.Supply, s.Params, s.BaseFee})
	return crypto.HashHex(b)
}

// sparseRootLocked : racine d'état via arbre de Merkle creux (internal/smt).
// Chaque entrée d'état devient UNE feuille (clé préfixée, valeur = JSON canonique),
// ce qui permet des PREUVES D'INCLUSION par compte pour les clients légers.
// L'arbre est INDÉPENDANT DE L'ORDRE d'insertion (déterministe entre nœuds).
// COUVRE EXACTEMENT les mêmes composants que la version JSON ci-dessus — toute
// omission casserait le consensus (la racine ne refléterait pas un changement) :
// un test de complétude (mute chaque composant) garde cet invariant.
func (s *State) sparseRootLocked() string {
	t := smt.New()
	put := func(key string, v any) {
		b, _ := json.Marshal(v)
		t.Update([]byte(key), b)
	}
	for k, v := range s.Accounts {
		put("acc/"+k, v)
	}
	for k, v := range s.Validators {
		put("val/"+k, v)
	}
	for k, v := range s.Tokens {
		put("tok/"+k, v)
	}
	for k, v := range s.Contracts {
		put("ctr/"+k, v)
	}
	for k, v := range s.WasmContracts {
		put("wasm/"+k, v)
	}
	for k, v := range s.Slashed {
		put("slash/"+k, v)
	}
	put("supply", s.Supply)
	put("params", s.Params)
	put("base_fee", s.BaseFee)
	put("unbonding", s.Unbonding)
	if s.Shielded != nil {
		put("shielded", s.Shielded)
	}
	return hex.EncodeToString(t.Root())
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
	if tmp.WasmContracts == nil {
		tmp.WasmContracts = map[string]*WasmContract{}
	}
	s.WasmContracts = tmp.WasmContracts
	// Pool blindé : restauré tel quel (nil si absent => pool jamais utilisé, racine
	// inchangée). Si présent avec une map de nullifiers nil (JSON tronqué), on la
	// ré-initialise pour éviter un nil-deref à la prochaine dépense.
	if tmp.Shielded != nil && tmp.Shielded.Nullifiers == nil {
		tmp.Shielded.Nullifiers = map[string]bool{}
	}
	s.Shielded = tmp.Shielded
	if tmp.Slashed == nil {
		tmp.Slashed = map[string]bool{}
	}
	s.Slashed = tmp.Slashed
	s.Unbonding = tmp.Unbonding
	s.Supply = tmp.Supply
	s.Params = tmp.Params
	s.BaseFee = tmp.BaseFee
	s.Height = tmp.Height
	s.LastHash = tmp.LastHash
	s.FinalizedHeight = tmp.FinalizedHeight
	return nil
}
