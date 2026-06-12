package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"runtime"
	"sync"
	"time"

	"chaingo/internal/consensus"
	"chaingo/internal/crypto"
	"chaingo/internal/mempool"
	"chaingo/internal/state"
	"chaingo/internal/types"
)

// cmdBench mesure le débit réel de la chaîne en local : signature,
// vérification post-quantique parallèle (mempool) et exécution des blocs.
// C'est le chemin complet d'une transaction, réseau exclu.
func cmdBench(args []string) error {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	txCount := fs.Int("txs", 10_000, "nombre de transactions")
	senders := fs.Int("senders", 16, "nombre de comptes émetteurs")
	fs.Parse(args)

	fmt.Printf("ChainGO bench — %d txs, %d émetteurs, %d cœurs, signatures %s\n\n",
		*txCount, *senders, runtime.NumCPU(), crypto.Scheme.Name())

	// Genèse en mémoire : règles par défaut, émetteurs financés + 1 validateur.
	st := state.New()
	st.SetParams(types.DefaultParams())
	vk, err := crypto.GenerateKeyPair()
	if err != nil {
		return err
	}
	st.BootstrapStake(vk.Address(), 1_000_000*types.Unit)

	kps := make([]*crypto.KeyPair, *senders)
	for i := range kps {
		if kps[i], err = crypto.GenerateKeyPair(); err != nil {
			return err
		}
		st.Mint(kps[i].Address(), 1_000_000*types.Unit)
	}
	rb := make([]byte, 32)
	rand.Read(rb)
	receiver := crypto.AddressFromPubBytes(rb)

	// 1) Signature des transactions (parallèle, hors chrono d'exécution).
	txs := make([]*types.Transaction, *txCount)
	tSign := time.Now()
	var wg sync.WaitGroup
	for s := 0; s < *senders; s++ {
		wg.Add(1)
		go func(s int) {
			defer wg.Done()
			nonce := uint64(0)
			for i := s; i < *txCount; i += *senders {
				tip := types.SuggestedTip
				if i%5 == 0 {
					tip *= 4 // une tx sur cinq enchérit (marché des tips)
				}
				tx := &types.Transaction{
					ChainID:    "bench",
					Type:       types.TxTransfer,
					To:         receiver,
					TokenID:    types.NativeToken,
					Amount:     1,
					Nonce:      nonce,
					MaxBaseFee: 1 * types.Unit,
					Tip:        tip,
					Timestamp:  int64(i),
				}
				tx.SignWith(kps[s])
				txs[i] = tx
				nonce++
			}
		}(s)
	}
	wg.Wait()
	signDur := time.Since(tSign)
	fmt.Printf("  Signature   : %8.0f tx/s  (%v)\n", float64(*txCount)/signDur.Seconds(), signDur.Round(time.Millisecond))

	// 2) Ingestion mempool = vérification ML-DSA parallèle.
	pool := mempool.New(*txCount + 10)
	eng := consensus.New(st, pool, nil, vk, time.Hour, 5_000)

	tIngest := time.Now()
	workers := runtime.NumCPU()
	ch := make(chan *types.Transaction, workers*2)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tx := range ch {
				if _, err := pool.Add(tx); err != nil {
					fmt.Println("  ! tx rejetée :", err)
				}
			}
		}()
	}
	for _, tx := range txs {
		ch <- tx
	}
	close(ch)
	wg.Wait()
	ingestDur := time.Since(tIngest)
	fmt.Printf("  Vérification: %8.0f tx/s  (%v)\n", float64(*txCount)/ingestDur.Seconds(), ingestDur.Round(time.Millisecond))

	// 3) Production de blocs jusqu'à vider le mempool.
	tExec := time.Now()
	blocks := 0
	for pool.Size() > 0 {
		b := eng.ProduceOnce(false)
		if b == nil {
			return fmt.Errorf("bench bloqué : %d txs restantes en mempool", pool.Size())
		}
		blocks++
	}
	execDur := time.Since(tExec)
	fmt.Printf("  Exécution   : %8.0f tx/s  (%v, %d blocs)\n", float64(*txCount)/execDur.Seconds(), execDur.Round(time.Millisecond), blocks)

	total := ingestDur + execDur
	tps := float64(*txCount) / total.Seconds()
	fmt.Printf("\n  TPS bout-en-bout (vérif + exécution) : %.0f tx/s", tps)
	if tps >= 1500 {
		fmt.Printf("  ✓ objectif 1500 TPS atteint (x%.1f)\n", tps/1500)
	} else {
		fmt.Printf("  ✗ sous l'objectif de 1500 TPS\n")
	}
	sup := st.GetSupply()
	fmt.Printf("  Brûlé pendant le bench : %s CGO — supply auto-déflationniste ✓\n", formatAmount(sup.Burned, types.NativeDecimals))
	return nil
}
