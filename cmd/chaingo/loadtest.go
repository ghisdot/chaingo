package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"chaingo/internal/crypto"
	"chaingo/internal/types"
)

// cmdLoadtest pilote un réseau ChainGO depuis l'extérieur :
//   - Crée K wallets éphémères (en mémoire, jamais sauvegardés)
//   - Les finance via le faucet du nœud (testnet/devnet uniquement)
//   - Lance N transferts entre eux en parallèle (M workers)
//   - Affiche la progression en direct + un résumé (p50/p95, échecs, TPS)
//
// Idéal pour vérifier qu'un testnet tient la charge et que les stats se
// mettent à jour en direct.
func cmdLoadtest(args []string) error {
	fs := flag.NewFlagSet("loadtest", flag.ExitOnError)
	api := fs.String("api", defaultAPI, "URL de l'API du nœud cible (testnet/devnet)")
	wallets := fs.Int("wallets", 10, "nombre de wallets éphémères à créer")
	txs := fs.Int("txs", 200, "nombre total de transferts à émettre")
	workers := fs.Int("workers", 8, "concurrence d'émission (signatures parallèles)")
	faucet := fs.Uint64("faucet", 100, "montant en CGO à demander au faucet par wallet")
	amount := fs.String("amount", "0.01", "montant CGO par transfert")
	fast := fs.Bool("fast", false, "utiliser le tip rapide (priorité dans le bloc)")
	fs.Parse(args)

	if *wallets < 2 {
		return fmt.Errorf("--wallets doit valoir au moins 2")
	}
	if *txs < 1 {
		return fmt.Errorf("--txs doit valoir au moins 1")
	}

	// État réseau initial.
	var status struct {
		ChainID     string `json:"chain_id"`
		PqSignature string `json:"pq_signature"`
		Network     string `json:"network"`
		Height      uint64 `json:"height"`
	}
	if err := getJSON(*api+"/v1/status", &status); err != nil {
		return fmt.Errorf("nœud injoignable sur %s : %w", *api, err)
	}
	if status.Network == "mainnet" || status.Network == "custom" {
		return fmt.Errorf("loadtest interdit hors testnet/devnet (réseau détecté : %s)", status.Network)
	}
	var fees struct {
		BaseFee          uint64 `json:"base_fee"`
		SuggestedMaxBase uint64 `json:"suggested_max_base"`
		SuggestedTip     uint64 `json:"suggested_tip"`
		FastTip          uint64 `json:"fast_tip"`
	}
	if err := getJSON(*api+"/v1/fees", &fees); err != nil {
		return err
	}
	tip := fees.SuggestedTip
	if *fast {
		tip = fees.FastTip
	}
	amt, err := parseAmount(*amount, types.NativeDecimals)
	if err != nil {
		return err
	}

	fmt.Printf("=== ChainGO loadtest ===\n")
	fmt.Printf("  Cible       : %s (chaîne %s, %s)\n", *api, status.ChainID, status.PqSignature)
	fmt.Printf("  Hauteur dép.: %d\n", status.Height)
	fmt.Printf("  Wallets     : %d éphémères\n", *wallets)
	fmt.Printf("  Transferts  : %d en %d workers\n", *txs, *workers)
	fmt.Printf("  Montant/tx  : %s CGO  (tip %s CGO%s)\n\n",
		*amount, formatAmount(tip, types.NativeDecimals),
		map[bool]string{false: "", true: " — fast"}[*fast])

	// 1) Créer K wallets éphémères.
	kps := make([]*crypto.KeyPair, *wallets)
	for i := range kps {
		k, err := crypto.GenerateKeyPair()
		if err != nil {
			return err
		}
		kps[i] = k
	}

	// 2) Financer via le faucet (en parallèle).
	fmt.Printf("[1/3] Faucet : %d × %d CGO\n", *wallets, *faucet)
	tStart := time.Now()
	var wg sync.WaitGroup
	faucetErr := make([]error, *wallets)
	sem := make(chan struct{}, *workers)
	for i, k := range kps {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, k *crypto.KeyPair) {
			defer wg.Done()
			defer func() { <-sem }()
			body := map[string]any{"address": k.Address(), "amount": *faucet * types.Unit}
			b, _ := json.Marshal(body)
			resp, err := http.Post(*api+"/v1/dev/faucet", "application/json", bytes.NewReader(b))
			if err != nil {
				faucetErr[i] = err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				faucetErr[i] = fmt.Errorf("HTTP %d", resp.StatusCode)
			}
		}(i, k)
	}
	wg.Wait()
	failedFaucet := 0
	for _, e := range faucetErr {
		if e != nil {
			failedFaucet++
		}
	}
	if failedFaucet > 0 {
		fmt.Printf("      ⚠ %d faucet(s) ont échoué (faucet probablement désactivé ou plafonné)\n", failedFaucet)
	}
	// Attendre que les wallets soient effectivement crédités (3 blocs ~= 1.5 s).
	time.Sleep(3 * time.Second)

	// 3) Émettre N transferts. Chaque wallet est attribué à un worker pour ne
	// jamais avoir deux goroutines qui incrémentent le même nonce.
	fmt.Printf("[2/3] Transferts : %d via %d workers…\n", *txs, *workers)
	type result struct {
		ok      bool
		latency time.Duration
	}
	results := make([]result, *txs)
	var sent int64
	var lastLog atomic.Value
	lastLog.Store(time.Now())
	jobs := make(chan int, *txs)
	for j := 0; j < *txs; j++ {
		jobs <- j
	}
	close(jobs)

	// Nonces par wallet (chaque worker gère sa colonne — pas de partage).
	nonces := make([]uint64, *wallets)
	var nonceMu sync.Mutex

	sendWg := sync.WaitGroup{}
	tSend := time.Now()
	for w := 0; w < *workers; w++ {
		sendWg.Add(1)
		go func(w int) {
			defer sendWg.Done()
			for j := range jobs {
				from := j % *wallets
				to := (j + 1) % *wallets
				nonceMu.Lock()
				nonce := nonces[from]
				nonces[from]++
				nonceMu.Unlock()

				tx := &types.Transaction{
					ChainID:    status.ChainID,
					Type:       types.TxTransfer,
					To:         kps[to].Address(),
					TokenID:    types.NativeToken,
					Amount:     amt,
					Nonce:      nonce,
					MaxBaseFee: fees.SuggestedMaxBase,
					Tip:        tip,
					Memo:       fmt.Sprintf("loadtest #%d", j),
					Timestamp:  time.Now().UnixMilli(),
				}
				tx.SignWith(kps[from])
				start := time.Now()
				var resp struct {
					Hash  string `json:"hash"`
					Error string `json:"error"`
				}
				err := postJSON(*api+"/v1/tx", tx, &resp)
				results[j] = result{ok: err == nil && resp.Error == "", latency: time.Since(start)}
				n := atomic.AddInt64(&sent, 1)
				if n%50 == 0 {
					last := lastLog.Load().(time.Time)
					if time.Since(last) > 800*time.Millisecond {
						lastLog.Store(time.Now())
						fmt.Printf("      %d/%d (%.0f%%)\n", n, *txs, float64(n)*100/float64(*txs))
					}
				}
			}
		}(w)
	}
	sendWg.Wait()
	sendDur := time.Since(tSend)

	// 4) Stats.
	fmt.Printf("[3/3] Résumé\n")
	ok := 0
	lats := make([]time.Duration, 0, len(results))
	for _, r := range results {
		if r.ok {
			ok++
		}
		lats = append(lats, r.latency)
	}
	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
	p50 := lats[len(lats)/2]
	p95 := lats[(len(lats)*95)/100]
	total := time.Since(tStart)

	// Lire la nouvelle hauteur / supply / burned pour montrer l'effet visible.
	var endStatus struct {
		Height uint64 `json:"height"`
		Supply struct {
			Burned uint64 `json:"burned"`
		} `json:"supply"`
	}
	getJSON(*api+"/v1/status", &endStatus)

	fmt.Printf("  Soumises   : %d (%d succès / %d échecs)\n", len(results), ok, len(results)-ok)
	fmt.Printf("  Durée envoi: %s\n", sendDur.Round(time.Millisecond))
	fmt.Printf("  Débit      : %.0f tx/s en soumission API\n", float64(len(results))/sendDur.Seconds())
	fmt.Printf("  Latence    : p50 %v · p95 %v\n", p50.Round(time.Millisecond), p95.Round(time.Millisecond))
	fmt.Printf("  Blocs prod.: %d → %d  (+%d hauteurs en %s)\n",
		status.Height, endStatus.Height, endStatus.Height-status.Height, total.Round(time.Millisecond))
	if endStatus.Supply.Burned > 0 {
		fmt.Printf("  Brûlé total: %s CGO (effet déflationniste vérifié sur la chaîne)\n",
			formatAmount(endStatus.Supply.Burned, types.NativeDecimals))
	}
	fmt.Printf("\nObserver en direct : %s/v1/blocks?limit=20\n", *api)
	return nil
}
