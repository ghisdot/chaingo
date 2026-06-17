// Package api : l'API REST publique du nœud — conçue pour être simple :
// JSON partout, CORS ouvert, erreurs uniformes {"error": "..."}.
package api

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"chaingo/internal/crypto"
	"chaingo/internal/mempool"
	"chaingo/internal/state"
	"chaingo/internal/types"
)

// wasmContractSummary : vue JSON compacte d'un contrat WASM (sans le bytecode brut).
func wasmContractSummary(c *state.WasmContract) map[string]any {
	return map[string]any{
		"address":           c.Address,
		"creator":           c.Creator,
		"balance":           c.Balance,
		"calls":             c.Calls,
		"created_at_height": c.CreatedAt,
		"code_size":         len(c.Code),
		"storage_keys":      len(c.Storage),
	}
}

// Backend is what the node exposes to the API layer.
type Backend interface {
	Status() map[string]any
	SubmitTx(tx *types.Transaction) (string, error)
	GetBlockByHeight(h uint64) *types.Block
	LatestBlocks(n int) []*types.Block
	GetTx(hash string) (*types.Transaction, uint64, bool)
	GetBlockByHash(hash string) *types.Block
	AddressTxs(addr string, limit int, beforeHeight uint64) []map[string]any
	GetAccount(addr string) *state.Account
	Validators() []*state.Validator
	Tokens() []*state.Token
	GetToken(sym string) *state.Token
	Contracts() []*state.Contract
	GetContract(id string) *state.Contract
	WasmContracts() []*state.WasmContract
	GetWasmContract(addr string) *state.WasmContract
	ShieldedPool() *state.ShieldedPool
	MempoolSize() int
	MempoolPending(limit int) []mempool.PendingInfo
	SupplyInfo() state.Supply
	Fees() map[string]any
	Height() uint64
	IsDev() bool
	FaucetEnabled() bool
	FaucetSend(to string, amount uint64) (string, error)
	DevNewWallet() (map[string]any, error)
	GenesisDoc() []byte
}

type Server struct {
	b      Backend
	addr   string
	webDir string // site vitrine + stats live, servi à la racine ("" = désactivé)
}

func NewServer(addr, webDir string, b Backend) *Server {
	return &Server{b: b, addr: addr, webDir: webDir}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v1", s.index)
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.b.Status())
	})
	mux.HandleFunc("GET /v1/supply", func(w http.ResponseWriter, r *http.Request) {
		sup := s.b.SupplyInfo()
		writeJSON(w, 200, map[string]any{
			"total":  sup.Total,
			"minted": sup.Minted,
			"burned": sup.Burned,
			"human": map[string]string{
				"total":  formatCGO(sup.Total),
				"minted": formatCGO(sup.Minted),
				"burned": formatCGO(sup.Burned),
			},
		})
	})
	mux.HandleFunc("GET /v1/blocks", func(w http.ResponseWriter, r *http.Request) {
		n := 20
		if v := r.URL.Query().Get("limit"); v != "" {
			if p, err := strconv.Atoi(v); err == nil && p > 0 && p <= 200 {
				n = p
			}
		}
		writeJSON(w, 200, s.b.LatestBlocks(n))
	})
	mux.HandleFunc("GET /v1/blocks/{ref}", func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		var h uint64
		if ref == "latest" {
			h = s.b.Height()
		} else {
			p, err := strconv.ParseUint(ref, 10, 64)
			if err != nil {
				writeErr(w, 400, "block ref must be a height or 'latest'")
				return
			}
			h = p
		}
		b := s.b.GetBlockByHeight(h)
		if b == nil {
			writeErr(w, 404, "block not found")
			return
		}
		writeJSON(w, 200, b)
	})
	mux.HandleFunc("POST /v1/tx", func(w http.ResponseWriter, r *http.Request) {
		var tx types.Transaction
		if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
			writeErr(w, 400, "invalid tx json: "+err.Error())
			return
		}
		hash, err := s.b.SubmitTx(&tx)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 202, map[string]any{"hash": hash, "status": "pending"})
	})
	mux.HandleFunc("GET /v1/tx/{hash}", func(w http.ResponseWriter, r *http.Request) {
		tx, height, ok := s.b.GetTx(r.PathValue("hash"))
		if !ok {
			writeErr(w, 404, "tx not found")
			return
		}
		writeJSON(w, 200, map[string]any{"tx": tx, "block_height": height, "status": "confirmed"})
	})
	mux.HandleFunc("GET /v1/accounts/{addr}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.b.GetAccount(r.PathValue("addr")))
	})
	mux.HandleFunc("GET /v1/accounts/{addr}/txs", func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if p, err := strconv.Atoi(v); err == nil && p > 0 && p <= 500 {
				limit = p
			}
		}
		var before uint64
		if v := r.URL.Query().Get("before"); v != "" {
			if p, err := strconv.ParseUint(v, 10, 64); err == nil {
				before = p
			}
		}
		writeJSON(w, 200, s.b.AddressTxs(r.PathValue("addr"), limit, before))
	})
	mux.HandleFunc("GET /v1/blocks/by-hash/{hash}", func(w http.ResponseWriter, r *http.Request) {
		b := s.b.GetBlockByHash(r.PathValue("hash"))
		if b == nil {
			writeErr(w, 404, "block not found")
			return
		}
		writeJSON(w, 200, b)
	})
	mux.HandleFunc("GET /v1/search", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeErr(w, 400, "q parameter required")
			return
		}
		writeJSON(w, 200, s.search(q))
	})
	mux.HandleFunc("GET /v1/validators", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.b.Validators())
	})
	mux.HandleFunc("GET /v1/tokens", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.b.Tokens())
	})
	mux.HandleFunc("GET /v1/tokens/{symbol}", func(w http.ResponseWriter, r *http.Request) {
		t := s.b.GetToken(r.PathValue("symbol"))
		if t == nil {
			writeErr(w, 404, "token not found")
			return
		}
		writeJSON(w, 200, t)
	})
	mux.HandleFunc("GET /v1/mempool", func(w http.ResponseWriter, r *http.Request) {
		// Par défaut compact (size seulement). ?details=1[&limit=N] retourne
		// les from/nonce/tip/age des tx en attente, triées par âge décroissant —
		// permet de diagnostiquer les trous de nonce (plusieurs entrées du
		// même `from` avec des nonces non consécutifs).
		if r.URL.Query().Get("details") == "" {
			writeJSON(w, 200, map[string]any{"size": s.b.MempoolSize()})
			return
		}
		limit := 100
		if v := r.URL.Query().Get("limit"); v != "" {
			fmt.Sscanf(v, "%d", &limit)
		}
		writeJSON(w, 200, map[string]any{
			"size":    s.b.MempoolSize(),
			"pending": s.b.MempoolPending(limit),
		})
	})
	mux.HandleFunc("GET /v1/fees", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.b.Fees())
	})
	mux.HandleFunc("GET /v1/contracts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.b.Contracts())
	})
	mux.HandleFunc("GET /v1/contracts/{id}", func(w http.ResponseWriter, r *http.Request) {
		c := s.b.GetContract(r.PathValue("id"))
		if c == nil {
			writeErr(w, 404, "contract not found")
			return
		}
		writeJSON(w, 200, c)
	})
	// Contrats WASM arbitraires. La liste renvoie une vue compacte (sans le
	// bytecode, potentiellement volumineux) ; le détail expose le storage.
	mux.HandleFunc("GET /v1/wasm/contracts", func(w http.ResponseWriter, r *http.Request) {
		cs := s.b.WasmContracts()
		out := make([]map[string]any, 0, len(cs))
		for _, c := range cs {
			out = append(out, wasmContractSummary(c))
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("GET /v1/wasm/contracts/{addr}", func(w http.ResponseWriter, r *http.Request) {
		c := s.b.GetWasmContract(r.PathValue("addr"))
		if c == nil {
			writeErr(w, 404, "wasm contract not found")
			return
		}
		v := wasmContractSummary(c)
		storage := make(map[string]string, len(c.Storage))
		for k, val := range c.Storage {
			storage[hex.EncodeToString([]byte(k))] = hex.EncodeToString(val)
		}
		v["storage_hex"] = storage // clés et valeurs en hexadécimal
		writeJSON(w, 200, v)
	})
	// Pool blindé (étage 5). Vue AGRÉGÉE et SANS FUITE de CONTENU : on expose le
	// nombre de notes, la racine (hex), le nombre de nullifiers dépensés et le solde
	// public verrouillé. JAMAIS le contenu des notes chiffrées (ShieldNote), JAMAIS
	// les montants, JAMAIS la table des nullifiers (clés). `enabled` reflète le gate.
	//
	// Le paramètre ?commitments=1 ajoute la liste ORDONNÉE des engagements (hex). Ce
	// sont des digests Poseidon PUBLICS et OPAQUES — exactement ce qu'un pool blindé
	// publie (cf. l'arbre de note-commitments de Zcash) ; ils ne révèlent ni montant
	// ni destinataire. Le wallet en a besoin pour reconstruire le chemin de Merkle
	// d'une dépense. Hors de ce paramètre, la réponse reste un pur agrégat.
	mux.HandleFunc("GET /v1/shielded", func(w http.ResponseWriter, r *http.Request) {
		fees := s.b.Fees()
		enabled, _ := fees["privacy_enabled"].(bool)
		shieldFee, _ := fees["shield_fee"].(uint64)
		out := map[string]any{
			"enabled":    enabled,
			"shield_fee": shieldFee,
			"notes":      0,
			"nullifiers": 0,
			"balance":    uint64(0),
			"root":       "",
		}
		if pool := s.b.ShieldedPool(); pool != nil {
			out["notes"] = len(pool.Commitments)
			out["nullifiers"] = len(pool.Nullifiers)
			out["balance"] = pool.Balance
			out["root"] = hex.EncodeToString(pool.Root)
			if r.URL.Query().Get("commitments") != "" {
				cms := make([]string, 0, len(pool.Commitments))
				for _, cm := range pool.Commitments {
					cms = append(cms, hex.EncodeToString(cm))
				}
				out["commitments"] = cms
			}
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("GET /v1/genesis", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(s.b.GenesisDoc())
	})

	// ---- endpoints devnet uniquement ----
	mux.HandleFunc("POST /v1/dev/faucet", func(w http.ResponseWriter, r *http.Request) {
		if !s.b.FaucetEnabled() {
			writeErr(w, 403, "faucet only available on dev/testnet")
			return
		}
		var req struct {
			Address string `json:"address"`
			Amount  uint64 `json:"amount"` // en ucgo ; 0 => 100 CGO
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "invalid json")
			return
		}
		if req.Amount == 0 {
			req.Amount = 100 * types.Unit
		}
		hash, err := s.b.FaucetSend(req.Address, req.Amount)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 202, map[string]any{"hash": hash, "status": "pending"})
	})
	mux.HandleFunc("POST /v1/dev/wallet", func(w http.ResponseWriter, r *http.Request) {
		if !s.b.IsDev() {
			writeErr(w, 403, "dev wallet only available in --dev mode")
			return
		}
		wlt, err := s.b.DevNewWallet()
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 201, wlt)
	})

	// Site web servi à la racine — les routes /v1/* restent prioritaires
	// (le mux choisit toujours le motif le plus spécifique).
	if s.webDir != "" {
		fs := http.FileServer(http.Dir(s.webDir))
		mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Content-Type explicite pour le wasm : WebAssembly.instantiateStreaming
			// l'exige, et mime.TypeByExtension n'est pas fiable sur Windows.
			if strings.HasSuffix(r.URL.Path, ".wasm") {
				w.Header().Set("Content-Type", "application/wasm")
			}
			fs.ServeHTTP(w, r)
		}))
		log.Printf("[api] website served from %s at http://%s/", s.webDir, s.addr)
	}

	log.Printf("[api] REST API on http://%s/v1", s.addr)
	return http.ListenAndServe(s.addr, cors(mux))
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"name": "ChainGO API v1",
		"docs": "https://github.com/chaingo — voir README.md",
		"endpoints": []string{
			"GET  /v1/status",
			"GET  /v1/supply",
			"GET  /v1/blocks?limit=20",
			"GET  /v1/blocks/{height|latest}",
			"POST /v1/tx                       (tx signée ML-DSA-65)",
			"GET  /v1/tx/{hash}",
			"GET  /v1/blocks/by-hash/{hash}    (explorateur)",
			"GET  /v1/accounts/{address}",
			"GET  /v1/accounts/{address}/txs?limit=50&before=<height> (historique tx, pagination)",
			"GET  /v1/search?q=<terme>         (recherche universelle : bloc/tx/adresse/token)",
			"GET  /v1/validators",
			"GET  /v1/tokens",
			"GET  /v1/tokens/{symbol}",
			"GET  /v1/fees                     (base fee EIP-1559 + tips suggérés)",
			"GET  /v1/contracts                (smart contracts no-code)",
			"GET  /v1/contracts/{id}",
			"GET  /v1/wasm/contracts           (contrats WASM arbitraires)",
			"GET  /v1/wasm/contracts/{addr}",
			"GET  /v1/shielded                 (pool blindé : agrégats sans fuite)",
			"GET  /v1/mempool",
			"GET  /v1/genesis",
			"POST /v1/dev/faucet               (devnet)",
			"POST /v1/dev/wallet               (devnet)",
		},
	})
}

// search : recherche universelle pour l'explorateur. Détecte le type de `q`
// (hauteur, hash de bloc, hash de tx, adresse, symbole de token) et renvoie
// {type, ref, url}. type = "none" si rien trouvé.
func (s *Server) search(q string) map[string]any {
	// 1) Hauteur de bloc.
	if h, err := strconv.ParseUint(q, 10, 64); err == nil {
		if s.b.GetBlockByHeight(h) != nil {
			return map[string]any{"type": "block", "ref": h, "url": "/v1/blocks/" + q}
		}
	}
	// 2) Adresse (cg + 40 hex).
	if crypto.ValidAddress(q) {
		return map[string]any{"type": "account", "ref": q, "url": "/v1/accounts/" + q}
	}
	// 3) Hash hex 64 chars : bloc ou tx ?
	if len(q) == 64 && isHex(q) {
		if s.b.GetBlockByHash(q) != nil {
			return map[string]any{"type": "block", "ref": q, "url": "/v1/blocks/by-hash/" + q}
		}
		if tx, _, ok := s.b.GetTx(q); ok && tx != nil {
			return map[string]any{"type": "tx", "ref": q, "url": "/v1/tx/" + q}
		}
	}
	// 4) Symbole de token (A-Z, 3-8).
	if isTokenSymbol(q) && s.b.GetToken(q) != nil {
		return map[string]any{"type": "token", "ref": q, "url": "/v1/tokens/" + q}
	}
	return map[string]any{"type": "none", "ref": q}
}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') && !(c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func isTokenSymbol(s string) bool {
	if len(s) < 3 || len(s) > 8 {
		return false
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func formatCGO(ucgo uint64) string {
	whole := ucgo / types.Unit
	frac := ucgo % types.Unit
	return strconv.FormatUint(whole, 10) + "." + pad9(frac) + " CGO"
}

func pad9(v uint64) string {
	s := strconv.FormatUint(v, 10)
	for len(s) < 9 {
		s = "0" + s
	}
	return s
}
