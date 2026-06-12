// Package node : assemble store, état, mempool, consensus, p2p et API.
package node

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"chaingo/internal/api"
	"chaingo/internal/consensus"
	"chaingo/internal/crypto"
	"chaingo/internal/genesis"
	"chaingo/internal/mempool"
	"chaingo/internal/p2p"
	"chaingo/internal/state"
	"chaingo/internal/store"
	"chaingo/internal/types"
)

const Version = "0.1.0-devnet"

// Config : paramètres LOCAUX du nœud (adresses, datadir). Les règles de
// la chaîne (intervalle de bloc, frais, stake min…) viennent des Params
// du document de genèse, pas d'ici.
type Config struct {
	DataDir       string
	APIAddr       string
	P2PAddr       string
	Peers         []string
	Dev           bool
	GenesisPath   string // fichier genesis.json
	GenesisURL    string // ou URL http(s)://host:port/v1/genesis
	ValidatorSeed string // fichier contenant la seed hex du validateur
	WebDir        string // dossier du site vitrine ("" = pas de site)
	MempoolMax    int
}

type Node struct {
	cfg      Config
	st       *state.State
	pool     *mempool.Mempool
	db       *store.Store
	engine   *consensus.Engine
	p2p      *p2p.Server
	gen      *genesis.Genesis
	key      *crypto.KeyPair // validateur (peut être nil)
	faucet   *crypto.KeyPair // devnet uniquement
	faucetMu sync.Mutex
	start    time.Time
}

func New(cfg Config) (*Node, error) {
	if cfg.MempoolMax == 0 {
		cfg.MempoolMax = 100_000
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}

	n := &Node{cfg: cfg, st: state.New(), pool: mempool.New(cfg.MempoolMax), start: time.Now()}

	db, err := store.Open(filepath.Join(cfg.DataDir, "chain.db"))
	if err != nil {
		return nil, err
	}
	n.db = db

	// Clé validateur : --dev en génère une, sinon fichier seed optionnel.
	if cfg.Dev {
		n.key, err = loadOrCreateKey(filepath.Join(cfg.DataDir, "validator.seed"))
		if err != nil {
			return nil, err
		}
		n.faucet, err = loadOrCreateKey(filepath.Join(cfg.DataDir, "faucet.seed"))
		if err != nil {
			return nil, err
		}
	} else if cfg.ValidatorSeed != "" {
		n.key, err = loadKey(cfg.ValidatorSeed)
		if err != nil {
			return nil, err
		}
	}

	if err := n.initChain(); err != nil {
		return nil, err
	}

	// Les règles de consensus viennent des Params de la chaîne.
	params := n.st.GetParams()
	n.engine = consensus.New(n.st, n.pool, n.db, n.key,
		time.Duration(params.BlockIntervalMs)*time.Millisecond, int(params.MaxBlockTxs))

	n.p2p = p2p.NewServer(cfg.P2PAddr, n.gen.ChainID, p2p.Handlers{
		OnTx: func(tx *types.Transaction) bool {
			isNew, err := n.acceptTx(tx)
			return err == nil && isNew
		},
		OnBlock: func(b *types.Block) (bool, bool) {
			err := n.engine.ApplyExternalBlock(b)
			if errors.Is(err, consensus.ErrGap) {
				return false, true
			}
			if err != nil {
				log.Printf("[node] rejected block #%d: %v", b.Header.Height, err)
				return false, false
			}
			return true, false
		},
		Height: n.st.GetHeight,
		Block: func(h uint64) *types.Block {
			b, _ := n.db.GetBlock(h)
			return b
		},
	})
	n.engine.OnBlock = func(b *types.Block) { n.p2p.Broadcast("block", b, nil) }
	return n, nil
}

// initChain loads the existing state or applies the genesis document.
func (n *Node) initChain() error {
	if data := n.db.LoadState(); data != nil {
		if err := n.st.Restore(data); err != nil {
			return fmt.Errorf("corrupt state: %w", err)
		}
		g, err := genesis.Parse(n.db.LoadGenesis())
		if err != nil {
			return fmt.Errorf("corrupt genesis: %w", err)
		}
		n.gen = g
		log.Printf("[node] resumed chain %s at height %d", g.ChainID, n.st.GetHeight())
		return nil
	}

	var g *genesis.Genesis
	switch {
	case n.cfg.GenesisPath != "":
		data, err := os.ReadFile(n.cfg.GenesisPath)
		if err != nil {
			return err
		}
		if g, err = genesis.Parse(data); err != nil {
			return err
		}
	case n.cfg.GenesisURL != "":
		resp, err := http.Get(n.cfg.GenesisURL)
		if err != nil {
			return fmt.Errorf("fetch genesis: %w", err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if g, err = genesis.Parse(data); err != nil {
			return err
		}
		log.Printf("[node] genesis fetched from %s", n.cfg.GenesisURL)
	case n.cfg.Dev:
		// Devnet : règles mainnet (DefaultParams) sauf l'unbonding,
		// raccourci à 5 minutes pour pouvoir tester le cycle complet.
		devParams := types.DefaultParams()
		devParams.UnbondingSeconds = 300
		g = &genesis.Genesis{
			ChainID:   "chaingo-dev-1",
			Timestamp: time.Now().UnixMilli(),
			Params:    &devParams,
			Alloc: map[string]uint64{
				n.faucet.Address(): 1_000_000_000 * types.Unit, // faucet : 1 milliard de CGO
				n.key.Address():    1_000 * types.Unit,
			},
			Stakes: map[string]uint64{
				n.key.Address(): 1_000_000 * types.Unit,
			},
		}
		// On écrit aussi genesis.json pour pouvoir le partager avec d'autres nœuds.
		os.WriteFile(filepath.Join(n.cfg.DataDir, "genesis.json"), g.Bytes(), 0o600)
	default:
		return errors.New("no existing chain and no genesis: use --dev, --genesis or --genesis-url")
	}

	gb := g.Apply(n.st)
	n.gen = g
	if err := n.db.SaveGenesis(g.Bytes()); err != nil {
		return err
	}
	if err := n.db.SaveBlock(gb); err != nil {
		return err
	}
	if err := n.db.SaveState(n.st.Bytes()); err != nil {
		return err
	}
	log.Printf("[node] chain %s initialized, genesis %s…", g.ChainID, gb.Hash[:12])
	if n.cfg.Dev {
		log.Printf("[node] DEV validator: %s", n.key.Address())
		log.Printf("[node] DEV faucet:    %s (1,000,000,000 CGO)", n.faucet.Address())
	}
	return nil
}

// Start runs p2p + consensus, connects to peers, then blocks serving the API.
func (n *Node) Start() error {
	if err := n.p2p.Start(); err != nil {
		return err
	}
	for _, peerAddr := range n.cfg.Peers {
		peerAddr = strings.TrimSpace(peerAddr)
		if peerAddr == "" {
			continue
		}
		if err := n.p2p.Connect(peerAddr); err != nil {
			log.Printf("[node] connect %s: %v", peerAddr, err)
		}
	}
	n.engine.Start()
	log.Printf("[node] ChainGO %s — chain %s — PQ signatures: %s", Version, n.gen.ChainID, crypto.Scheme.Name())
	webDir := n.cfg.WebDir
	if webDir != "" {
		if _, err := os.Stat(webDir); err != nil {
			log.Printf("[node] web dir %q not found — website disabled", webDir)
			webDir = ""
		}
	}
	return api.NewServer(n.cfg.APIAddr, webDir, n).Start()
}

// acceptTx is the single entry point for new transactions (API + p2p).
func (n *Node) acceptTx(tx *types.Transaction) (bool, error) {
	if tx.ChainID != n.gen.ChainID {
		return false, fmt.Errorf("wrong chain_id: got %q, want %q", tx.ChainID, n.gen.ChainID)
	}
	// Anti-spam minimal : le nonce ne doit pas être déjà passé.
	if tx.Nonce < n.st.NonceOf(tx.From) {
		return false, fmt.Errorf("stale nonce %d (account at %d)", tx.Nonce, n.st.NonceOf(tx.From))
	}
	// Refus immédiat si le plafond de base fee est déjà dépassé.
	if bf := n.st.GetBaseFee(); tx.MaxBaseFee < bf {
		return false, fmt.Errorf("max_base_fee %d below current base fee %d (see GET /v1/fees)", tx.MaxBaseFee, bf)
	}
	isNew, err := n.pool.Add(tx)
	if err != nil && !errors.Is(err, mempool.ErrDuplicate) {
		return false, err
	}
	return isNew, nil
}

// ---- implémentation de api.Backend ----

func (n *Node) Status() map[string]any {
	return map[string]any{
		"version":      Version,
		"chain_id":     n.gen.ChainID,
		"height":       n.st.GetHeight(),
		"last_hash":    n.st.GetLastHash(),
		"mempool":      n.pool.Size(),
		"peers":        n.p2p.PeerCount(),
		"validators":   len(n.st.ListValidators()),
		"supply":       n.st.GetSupply(),
		"base_fee":     n.st.GetBaseFee(),
		"params":       n.st.GetParams(),
		"pq_signature": crypto.Scheme.Name(),
		"dev_mode":     n.cfg.Dev,
		"uptime_s":     int(time.Since(n.start).Seconds()),
	}
}

// Fees : tout ce qu'un client doit savoir pour construire une tx.
func (n *Node) Fees() map[string]any {
	bf := n.st.GetBaseFee()
	p := n.st.GetParams()
	return map[string]any{
		"base_fee":            bf,
		"suggested_max_base":  bf * 2, // marge contre les hausses entre signature et inclusion
		"suggested_tip":       types.SuggestedTip,
		"fast_tip":            types.SuggestedTip * 4,
		"private_extra_burn":  bf * p.PrivacyFeeMult,
		"token_create_fee":    p.TokenCreateFee,
		"min_validator_stake": p.MinValidatorStake,
		"unbonding_seconds":   p.UnbondingSeconds,
	}
}

func (n *Node) SubmitTx(tx *types.Transaction) (string, error) {
	isNew, err := n.acceptTx(tx)
	if err != nil {
		return "", err
	}
	if isNew {
		n.p2p.Broadcast("tx", tx, nil)
	}
	return tx.Hash(), nil
}

func (n *Node) GetBlockByHeight(h uint64) *types.Block {
	b, _ := n.db.GetBlock(h)
	return b
}

func (n *Node) LatestBlocks(count int) []*types.Block {
	top := n.st.GetHeight()
	out := []*types.Block{}
	for i := 0; i < count; i++ {
		h := int64(top) - int64(i)
		if h < 0 {
			break
		}
		b, _ := n.db.GetBlock(uint64(h))
		if b == nil {
			break
		}
		out = append(out, b)
	}
	return out
}

func (n *Node) GetTx(hash string) (*types.Transaction, uint64, bool) {
	h, ok := n.db.TxHeight(hash)
	if !ok {
		return nil, 0, false
	}
	b, _ := n.db.GetBlock(h)
	if b == nil {
		return nil, 0, false
	}
	for _, tx := range b.Txs {
		if tx.Hash() == hash {
			return tx, h, true
		}
	}
	return nil, 0, false
}

func (n *Node) GetAccount(addr string) *state.Account  { return n.st.GetAccount(addr) }
func (n *Node) Validators() []*state.Validator         { return n.st.ListValidators() }
func (n *Node) Tokens() []*state.Token                 { return n.st.ListTokens() }
func (n *Node) GetToken(sym string) *state.Token       { return n.st.GetToken(sym) }
func (n *Node) MempoolSize() int                       { return n.pool.Size() }
func (n *Node) SupplyInfo() state.Supply               { return n.st.GetSupply() }
func (n *Node) Height() uint64                         { return n.st.GetHeight() }
func (n *Node) IsDev() bool                            { return n.cfg.Dev }
func (n *Node) GenesisDoc() []byte                     { return n.gen.Bytes() }

func (n *Node) FaucetSend(to string, amount uint64) (string, error) {
	if n.faucet == nil {
		return "", errors.New("no faucet on this node")
	}
	if !crypto.ValidAddress(to) {
		return "", errors.New("invalid address")
	}
	n.faucetMu.Lock()
	defer n.faucetMu.Unlock()
	tx := &types.Transaction{
		ChainID:    n.gen.ChainID,
		Type:       types.TxTransfer,
		To:         to,
		TokenID:    types.NativeToken,
		Amount:     amount,
		Nonce:      n.pendingNonce(n.faucet.Address()),
		MaxBaseFee: 1 * types.Unit, // plafond très large : le faucet paie ce qu'il faut
		Tip:        types.SuggestedTip * 4,
		Memo:       "devnet faucet",
		Timestamp:  time.Now().UnixMilli(),
	}
	tx.SignWith(n.faucet)
	return n.SubmitTx(tx)
}

// pendingNonce: state nonce + faucet txs still in the mempool. v1: the
// faucet serializes via faucetMu and waits for inclusion is not needed
// because Take() orders by nonce.
func (n *Node) pendingNonce(addr string) uint64 {
	base := n.st.NonceOf(addr)
	// compte les tx du même compte encore en mempool
	pending := uint64(0)
	for _, tx := range n.pool.Take(n.cfg.MempoolMax, n.st.NonceOf) {
		if tx.From == addr {
			pending++
		}
	}
	return base + pending
}

func (n *Node) DevNewWallet() (map[string]any, error) {
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"address":  kp.Address(),
		"seed_hex": hex.EncodeToString(kp.Seed),
		"warning":  "devnet only — store the seed safely, it IS the wallet",
	}, nil
}

// ---- clés ----

func loadOrCreateKey(path string) (*crypto.KeyPair, error) {
	if _, err := os.Stat(path); err == nil {
		return loadKey(path)
	}
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(kp.Seed)), 0o600); err != nil {
		return nil, err
	}
	return kp, nil
}

func loadKey(path string) (*crypto.KeyPair, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("seed file %s: %w", path, err)
	}
	if len(seed) != crypto.Scheme.SeedSize() {
		return nil, fmt.Errorf("seed must be %d bytes", crypto.Scheme.SeedSize())
	}
	return crypto.FromSeed(seed), nil
}
