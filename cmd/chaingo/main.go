// chaingo : binaire unique — nœud, wallet, transactions, tokens, bench.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"chaingo/internal/crypto"
	"chaingo/internal/node"
	"chaingo/internal/types"
	"chaingo/internal/wallet"
)

const usage = `ChainGO — blockchain post-quantique (ML-DSA-65 / FIPS 204)

Usage :
  chaingo node start [--dev] [--api :8545] [--p2p :9000] [--peers host:port,...]
                     [--datadir DIR] [--genesis FILE | --genesis-url URL]
                     [--validator-seed FILE]
  chaingo wallet new <name> [--pass MDP]
  chaingo wallet list
  chaingo wallet show <name>
  chaingo balance <adresse|wallet> [--api URL]
  chaingo send --from <wallet> --to <adresse|wallet> --amount 1.5 [--token CGO]
               [--fast | --tip 0.0002] [--private] [--memo TXT] [--pass MDP] [--api URL]
  chaingo token create --from <wallet> --symbol TKN --name "Mon Token"
               --supply 1000000 [--decimals 9] [--mintable] [--pass MDP] [--api URL]
  chaingo stake --from <wallet> --amount 10000 [--pass MDP] [--api URL]
  chaingo unstake --from <wallet> --amount 10000 [--pass MDP] [--api URL]
  chaingo delegate --from <wallet> --to <validateur> --amount 50 [--pass MDP] [--api URL]
  chaingo undelegate --from <wallet> --to <validateur> --amount 50 [--pass MDP] [--api URL]
  chaingo faucet --to <adresse|wallet> [--amount 100] [--api URL]   (devnet)
  chaingo bench [--txs 10000] [--senders 16]
`

const defaultAPI = "http://127.0.0.1:8545"

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(1)
	}
	var err error
	switch os.Args[1] {
	case "node":
		err = cmdNode(os.Args[2:])
	case "wallet":
		err = cmdWallet(os.Args[2:])
	case "balance":
		err = cmdBalance(os.Args[2:])
	case "send":
		err = cmdSend(os.Args[2:])
	case "token":
		err = cmdToken(os.Args[2:])
	case "stake":
		err = cmdStake(os.Args[2:], types.TxStake)
	case "unstake":
		err = cmdStake(os.Args[2:], types.TxUnstake)
	case "delegate":
		err = cmdDelegate(os.Args[2:], types.TxDelegate)
	case "undelegate":
		err = cmdDelegate(os.Args[2:], types.TxUndelegate)
	case "faucet":
		err = cmdFaucet(os.Args[2:])
	case "bench":
		err = cmdBench(os.Args[2:])
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Printf("commande inconnue %q\n\n%s", os.Args[1], usage)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "erreur :", err)
		os.Exit(1)
	}
}

// ---------- node ----------

func cmdNode(args []string) error {
	if len(args) < 1 || args[0] != "start" {
		return fmt.Errorf("usage : chaingo node start [flags]")
	}
	fs := flag.NewFlagSet("node start", flag.ExitOnError)
	home, _ := os.UserHomeDir()
	dev := fs.Bool("dev", false, "devnet : génère validateur + faucet, active /v1/dev/*")
	api := fs.String("api", ":8545", "adresse d'écoute API REST")
	p2pAddr := fs.String("p2p", ":9000", "adresse d'écoute P2P (vide = désactivé)")
	peers := fs.String("peers", "", "pairs à joindre, séparés par des virgules")
	datadir := fs.String("datadir", filepath.Join(home, ".chaingo", "node"), "répertoire de données")
	genesisPath := fs.String("genesis", "", "fichier genesis.json (l'intervalle de bloc, les frais, etc. y sont définis via params)")
	genesisURL := fs.String("genesis-url", "", "URL /v1/genesis d'un nœud existant")
	seed := fs.String("validator-seed", "", "fichier seed hex du validateur")
	web := fs.String("web", "web", "dossier du site vitrine servi à la racine de l'API (vide = désactivé)")
	fs.Parse(args[1:])

	n, err := node.New(node.Config{
		DataDir:       *datadir,
		APIAddr:       strings.TrimPrefix(*api, "http://"),
		P2PAddr:       *p2pAddr,
		Peers:         strings.Split(*peers, ","),
		Dev:           *dev,
		GenesisPath:   *genesisPath,
		GenesisURL:    *genesisURL,
		ValidatorSeed: *seed,
		WebDir:        *web,
	})
	if err != nil {
		return err
	}
	return n.Start()
}

// ---------- wallet ----------

func cmdWallet(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage : chaingo wallet new|list|show")
	}
	switch args[0] {
	case "new":
		fs := flag.NewFlagSet("wallet new", flag.ExitOnError)
		pass := fs.String("pass", "", "mot de passe du keystore")
		fs.Parse(args[1:])
		if fs.NArg() < 1 {
			return fmt.Errorf("usage : chaingo wallet new <name>")
		}
		name := fs.Arg(0)
		kp, path, err := wallet.Create(name, *pass)
		if err != nil {
			return err
		}
		fmt.Printf("Wallet %q créé (signatures post-quantiques %s)\n", name, crypto.Scheme.Name())
		fmt.Printf("  Adresse : %s\n", kp.Address())
		fmt.Printf("  Fichier : %s\n", path)
		if *pass == "" {
			fmt.Println("  ⚠ keystore chiffré avec un mot de passe VIDE — ok en devnet seulement")
		}
		return nil
	case "list":
		keys, err := wallet.List()
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			fmt.Println("aucun wallet — créez-en un avec : chaingo wallet new <name>")
			return nil
		}
		for _, k := range keys {
			fmt.Printf("%-20s %s\n", k.Name, k.Address)
		}
		return nil
	case "show":
		if len(args) < 2 {
			return fmt.Errorf("usage : chaingo wallet show <name>")
		}
		keys, err := wallet.List()
		if err != nil {
			return err
		}
		for _, k := range keys {
			if k.Name == args[1] {
				fmt.Printf("%s\n", k.Address)
				return nil
			}
		}
		return fmt.Errorf("wallet %q introuvable", args[1])
	default:
		return fmt.Errorf("usage : chaingo wallet new|list|show")
	}
}

// ---------- balance ----------

func cmdBalance(args []string) error {
	fs := flag.NewFlagSet("balance", flag.ExitOnError)
	api := fs.String("api", defaultAPI, "URL de l'API")
	fs.Parse(args)
	if fs.NArg() < 1 {
		return fmt.Errorf("usage : chaingo balance <adresse|wallet>")
	}
	addr, err := resolveAddress(fs.Arg(0))
	if err != nil {
		return err
	}
	var acct struct {
		Address     string            `json:"address"`
		Balances    map[string]uint64 `json:"balances"`
		Nonce       uint64            `json:"nonce"`
		Staked      uint64            `json:"staked"`
		Unbonding   uint64            `json:"unbonding"`
		Delegations map[string]uint64 `json:"delegations"`
	}
	if err := getJSON(*api+"/v1/accounts/"+addr, &acct); err != nil {
		return err
	}
	fmt.Printf("Compte %s (nonce %d)\n", acct.Address, acct.Nonce)
	if len(acct.Balances) == 0 && acct.Staked == 0 {
		fmt.Println("  (vide)")
	}
	if acct.Unbonding > 0 {
		fmt.Printf("  %-10s %s (en unbonding)\n", "CGO", formatAmount(acct.Unbonding, types.NativeDecimals))
	}
	for tok, bal := range acct.Balances {
		dec := uint8(types.NativeDecimals)
		if tok != types.NativeToken {
			var t struct {
				Decimals uint8 `json:"decimals"`
			}
			if getJSON(*api+"/v1/tokens/"+tok, &t) == nil {
				dec = t.Decimals
			}
		}
		fmt.Printf("  %-10s %s\n", tok, formatAmount(bal, dec))
	}
	if acct.Staked > 0 {
		fmt.Printf("  %-10s %s (staké)\n", "CGO", formatAmount(acct.Staked, types.NativeDecimals))
	}
	for vAddr, amt := range acct.Delegations {
		fmt.Printf("  %-10s %s (délégué à %s…)\n", "CGO", formatAmount(amt, types.NativeDecimals), vAddr[:12])
	}
	return nil
}

// ---------- send ----------

func cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	from := fs.String("from", "", "wallet émetteur")
	to := fs.String("to", "", "adresse destinataire")
	amount := fs.String("amount", "", "montant (ex : 1.5)")
	token := fs.String("token", types.NativeToken, "token à transférer")
	fast := fs.Bool("fast", false, "priorité : tip x4 (préréglage du marché des tips)")
	tip := fs.String("tip", "", "tip en CGO versé au validateur (enchère libre, prioritaire sur --fast)")
	private := fs.Bool("private", false, "mode confidentialité accrue (burn supplémentaire = 2x base fee)")
	memo := fs.String("memo", "", "mémo (max 256)")
	pass := fs.String("pass", "", "mot de passe du wallet")
	api := fs.String("api", defaultAPI, "URL de l'API")
	fs.Parse(args)
	if *from == "" || *to == "" || *amount == "" {
		return fmt.Errorf("--from, --to et --amount sont requis")
	}
	kp, err := wallet.Load(*from, *pass)
	if err != nil {
		return err
	}
	dest, err := resolveAddress(*to)
	if err != nil {
		return err
	}
	dec := uint8(types.NativeDecimals)
	if *token != types.NativeToken {
		var t struct {
			Decimals uint8 `json:"decimals"`
		}
		if err := getJSON(*api+"/v1/tokens/"+*token, &t); err != nil {
			return fmt.Errorf("token %s : %w", *token, err)
		}
		dec = t.Decimals
	}
	amt, err := parseAmount(*amount, dec)
	if err != nil {
		return err
	}
	tipAmt := types.SuggestedTip
	if *fast {
		tipAmt = types.SuggestedTip * 4
	}
	if *tip != "" {
		if tipAmt, err = parseAmount(*tip, types.NativeDecimals); err != nil {
			return err
		}
	}
	tx := &types.Transaction{
		Type:    types.TxTransfer,
		To:      dest,
		TokenID: *token,
		Amount:  amt,
		Tip:     tipAmt,
		Private: *private,
		Memo:    *memo,
	}
	return signAndSubmit(*api, kp, tx)
}

// ---------- token (no-code) ----------

func cmdToken(args []string) error {
	if len(args) < 1 || args[0] != "create" {
		return fmt.Errorf("usage : chaingo token create [flags]")
	}
	fs := flag.NewFlagSet("token create", flag.ExitOnError)
	from := fs.String("from", "", "wallet créateur")
	symbol := fs.String("symbol", "", "symbole (3-8 caractères, A-Z0-9)")
	name := fs.String("name", "", "nom du token")
	supply := fs.String("supply", "", "supply initiale (en unités entières du token)")
	decimals := fs.Uint("decimals", 9, "décimales (max 12)")
	mintable := fs.Bool("mintable", false, "le créateur pourra minter plus tard")
	pass := fs.String("pass", "", "mot de passe du wallet")
	api := fs.String("api", defaultAPI, "URL de l'API")
	fs.Parse(args[1:])
	if *from == "" || *symbol == "" || *name == "" || *supply == "" {
		return fmt.Errorf("--from, --symbol, --name et --supply sont requis")
	}
	kp, err := wallet.Load(*from, *pass)
	if err != nil {
		return err
	}
	sup, err := parseAmount(*supply, uint8(*decimals))
	if err != nil {
		return err
	}
	tx := &types.Transaction{
		Type: types.TxCreateToken,
		Token: &types.TokenParams{
			Symbol:   strings.ToUpper(*symbol),
			Name:     *name,
			Decimals: uint8(*decimals),
			Supply:   sup,
			Mintable: *mintable,
		},
	}
	fmt.Printf("Création du token %s (%s) — coût : 10 CGO brûlés\n", strings.ToUpper(*symbol), *name)
	return signAndSubmit(*api, kp, tx)
}

// ---------- stake / unstake ----------

func cmdStake(args []string, typ types.TxType) error {
	fs := flag.NewFlagSet(string(typ), flag.ExitOnError)
	from := fs.String("from", "", "wallet")
	amount := fs.String("amount", "", "montant en CGO")
	pass := fs.String("pass", "", "mot de passe du wallet")
	api := fs.String("api", defaultAPI, "URL de l'API")
	fs.Parse(args)
	if *from == "" || *amount == "" {
		return fmt.Errorf("--from et --amount sont requis")
	}
	kp, err := wallet.Load(*from, *pass)
	if err != nil {
		return err
	}
	amt, err := parseAmount(*amount, types.NativeDecimals)
	if err != nil {
		return err
	}
	tx := &types.Transaction{Type: typ, Amount: amt}
	if err := signAndSubmit(*api, kp, tx); err != nil {
		return err
	}
	if typ == types.TxUnstake {
		var fees struct {
			UnbondingSeconds int64 `json:"unbonding_seconds"`
		}
		if getJSON(*api+"/v1/fees", &fees) == nil {
			fmt.Printf("Fonds en unbonding : liquides dans ~%s (règle de la chaîne)\n",
				(time.Duration(fees.UnbondingSeconds) * time.Second).String())
		}
	}
	return nil
}

// ---------- delegate / undelegate ----------

func cmdDelegate(args []string, typ types.TxType) error {
	fs := flag.NewFlagSet(string(typ), flag.ExitOnError)
	from := fs.String("from", "", "wallet délégateur")
	to := fs.String("to", "", "adresse du validateur")
	amount := fs.String("amount", "", "montant en CGO")
	pass := fs.String("pass", "", "mot de passe du wallet")
	api := fs.String("api", defaultAPI, "URL de l'API")
	fs.Parse(args)
	if *from == "" || *to == "" || *amount == "" {
		return fmt.Errorf("--from, --to et --amount sont requis")
	}
	kp, err := wallet.Load(*from, *pass)
	if err != nil {
		return err
	}
	dest, err := resolveAddress(*to)
	if err != nil {
		return err
	}
	amt, err := parseAmount(*amount, types.NativeDecimals)
	if err != nil {
		return err
	}
	tx := &types.Transaction{Type: typ, To: dest, Amount: amt}
	if err := signAndSubmit(*api, kp, tx); err != nil {
		return err
	}
	if typ == types.TxDelegate {
		fmt.Println("Délégation active : vous touchez votre part des récompenses à chaque bloc proposé par ce validateur (moins sa commission).")
	} else {
		var fees struct {
			UnbondingSeconds int64 `json:"unbonding_seconds"`
		}
		if getJSON(*api+"/v1/fees", &fees) == nil {
			fmt.Printf("Fonds en unbonding : liquides dans ~%s (règle de la chaîne)\n",
				(time.Duration(fees.UnbondingSeconds) * time.Second).String())
		}
	}
	return nil
}

// ---------- faucet ----------

func cmdFaucet(args []string) error {
	fs := flag.NewFlagSet("faucet", flag.ExitOnError)
	to := fs.String("to", "", "adresse ou wallet destinataire")
	amount := fs.String("amount", "100", "montant en CGO")
	api := fs.String("api", defaultAPI, "URL de l'API")
	fs.Parse(args)
	if *to == "" {
		return fmt.Errorf("--to est requis")
	}
	addr, err := resolveAddress(*to)
	if err != nil {
		return err
	}
	amt, err := parseAmount(*amount, types.NativeDecimals)
	if err != nil {
		return err
	}
	var resp struct {
		Hash  string `json:"hash"`
		Error string `json:"error"`
	}
	if err := postJSON(*api+"/v1/dev/faucet", map[string]any{"address": addr, "amount": amt}, &resp); err != nil {
		return err
	}
	fmt.Printf("Faucet → %s : %s CGO (tx %s)\n", addr, *amount, resp.Hash)
	return nil
}

// ---------- helpers ----------

// resolveAddress accepts either a raw address or a local wallet name.
func resolveAddress(ref string) (string, error) {
	if crypto.ValidAddress(ref) {
		return ref, nil
	}
	keys, err := wallet.List()
	if err != nil {
		return "", err
	}
	for _, k := range keys {
		if k.Name == ref {
			return k.Address, nil
		}
	}
	return "", fmt.Errorf("%q n'est ni une adresse valide ni un wallet local", ref)
}

// signAndSubmit fills chain_id/nonce/fees/timestamp, signs and posts the
// tx, then waits briefly for confirmation.
func signAndSubmit(api string, kp *crypto.KeyPair, tx *types.Transaction) error {
	var status struct {
		ChainID string `json:"chain_id"`
	}
	if err := getJSON(api+"/v1/status", &status); err != nil {
		return fmt.Errorf("nœud injoignable sur %s : %w", api, err)
	}
	var acct struct {
		Nonce uint64 `json:"nonce"`
	}
	if err := getJSON(api+"/v1/accounts/"+kp.Address(), &acct); err != nil {
		return err
	}
	if tx.Tip == 0 {
		tx.Tip = types.SuggestedTip
	}
	if tx.MaxBaseFee == 0 {
		var fees struct {
			BaseFee          uint64 `json:"base_fee"`
			SuggestedMaxBase uint64 `json:"suggested_max_base"`
		}
		if err := getJSON(api+"/v1/fees", &fees); err != nil {
			return err
		}
		tx.MaxBaseFee = fees.SuggestedMaxBase
		fmt.Printf("Frais : base fee actuel %s CGO (brûlé), tip %s CGO\n",
			formatAmount(fees.BaseFee, types.NativeDecimals), formatAmount(tx.Tip, types.NativeDecimals))
	}
	tx.ChainID = status.ChainID
	tx.Nonce = acct.Nonce
	tx.Timestamp = time.Now().UnixMilli()
	tx.SignWith(kp)

	var resp struct {
		Hash  string `json:"hash"`
		Error string `json:"error"`
	}
	if err := postJSON(api+"/v1/tx", tx, &resp); err != nil {
		return err
	}
	fmt.Printf("Tx envoyée : %s\n", resp.Hash)
	for i := 0; i < 20; i++ {
		time.Sleep(400 * time.Millisecond)
		var conf struct {
			BlockHeight uint64 `json:"block_height"`
			Status      string `json:"status"`
		}
		if getJSON(api+"/v1/tx/"+resp.Hash, &conf) == nil && conf.Status == "confirmed" {
			fmt.Printf("✓ Confirmée dans le bloc #%d\n", conf.BlockHeight)
			return nil
		}
	}
	fmt.Println("… toujours en attente (vérifiez : GET /v1/tx/" + resp.Hash + ")")
	return nil
}

func getJSON(url string, out any) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return apiError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func postJSON(url string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return apiError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func apiError(resp *http.Response) error {
	var e struct {
		Error string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&e)
	if e.Error != "" {
		return fmt.Errorf("%s", e.Error)
	}
	return fmt.Errorf("HTTP %d", resp.StatusCode)
}

// parseAmount converts "1.5" with d decimals into base units.
func parseAmount(s string, d uint8) (uint64, error) {
	parts := strings.SplitN(s, ".", 2)
	whole, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("montant invalide %q", s)
	}
	mult := uint64(1)
	for i := uint8(0); i < d; i++ {
		mult *= 10
	}
	out := whole * mult
	if len(parts) == 2 {
		frac := parts[1]
		if len(frac) > int(d) {
			return 0, fmt.Errorf("trop de décimales (max %d)", d)
		}
		for len(frac) < int(d) {
			frac += "0"
		}
		f, err := strconv.ParseUint(frac, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("montant invalide %q", s)
		}
		out += f
	}
	return out, nil
}

func formatAmount(v uint64, d uint8) string {
	mult := uint64(1)
	for i := uint8(0); i < d; i++ {
		mult *= 10
	}
	if mult == 1 {
		return strconv.FormatUint(v, 10)
	}
	whole := v / mult
	frac := strconv.FormatUint(v%mult, 10)
	for len(frac) < int(d) {
		frac = "0" + frac
	}
	frac = strings.TrimRight(frac, "0")
	if frac == "" {
		return strconv.FormatUint(whole, 10)
	}
	return strconv.FormatUint(whole, 10) + "." + frac
}
