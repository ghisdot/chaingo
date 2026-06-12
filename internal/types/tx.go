package types

import (
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"sync"

	"encoding/json"

	"chaingo/internal/crypto"
)

type TxType string

const (
	TxTransfer    TxType = "transfer"
	TxCreateToken TxType = "create_token"
	TxMint        TxType = "mint"
	TxStake       TxType = "stake"
	TxUnstake     TxType = "unstake"
)

const (
	NativeToken    = "CGO"
	NativeDecimals = 9
	Unit           = uint64(1_000_000_000) // 1 CGO en ucgo

	// SuggestedTip : tip par défaut proposé par les clients (CLI, API).
	// Le tip est un marché libre : le mempool sert les plus offrants en
	// premier. Le base fee, lui, est dynamique (EIP-1559) et BRÛLÉ —
	// voir Params et state.applyTx.
	SuggestedTip = uint64(50_000)
)

type TokenParams struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Decimals uint8  `json:"decimals"`
	Supply   uint64 `json:"supply"`
	Mintable bool   `json:"mintable"`
}

type Transaction struct {
	ChainID    string       `json:"chain_id"`
	Type       TxType       `json:"type"`
	From       string       `json:"from"`
	FromPubKey []byte       `json:"from_pub_key"`
	To         string       `json:"to,omitempty"`
	TokenID    string       `json:"token_id,omitempty"`
	Amount     uint64       `json:"amount"`
	Nonce      uint64       `json:"nonce"`
	MaxBaseFee uint64       `json:"max_base_fee"` // plafond de base fee accepté (protection contre les pics)
	Tip        uint64       `json:"tip"`          // enchère libre versée au proposeur
	Private    bool         `json:"private,omitempty"`
	Memo       string       `json:"memo,omitempty"`
	Token      *TokenParams `json:"token,omitempty"`
	Timestamp  int64        `json:"timestamp"`
	Signature  []byte       `json:"signature,omitempty"`
}

// SigningBytes returns the canonical bytes covered by the signature
// (struct field order is fixed, []byte marshals to base64 — deterministic).
func (tx *Transaction) SigningBytes() []byte {
	clone := *tx
	clone.Signature = nil
	b, _ := json.Marshal(&clone)
	return b
}

func (tx *Transaction) Hash() string { return crypto.HashHex(tx.SigningBytes()) }

// Les frais réels (burn dynamique + tip) dépendent de l'état de la chaîne
// (base fee courant, Params) : ils sont calculés dans state.applyTx.

func (tx *Transaction) SignWith(kp *crypto.KeyPair) {
	tx.From = kp.Address()
	tx.FromPubKey = kp.PubBytes()
	tx.Signature = kp.Sign(tx.SigningBytes())
}

func (tx *Transaction) VerifySignature() error {
	return crypto.Verify(tx.FromPubKey, tx.SigningBytes(), tx.Signature)
}

var symbolRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{2,7}$`)

// ValidateBasic checks everything that does not require chain state,
// including the post-quantum signature (CPU-heavy: ~0.1 ms).
func (tx *Transaction) ValidateBasic() error {
	if !crypto.ValidAddress(tx.From) {
		return errors.New("invalid from address")
	}
	if crypto.AddressFromPubBytes(tx.FromPubKey) != tx.From {
		return errors.New("public key does not match from address")
	}
	if tx.MaxBaseFee == 0 {
		return errors.New("max_base_fee required (see GET /v1/fees)")
	}
	if len(tx.Memo) > 256 {
		return errors.New("memo too long (max 256)")
	}
	switch tx.Type {
	case TxTransfer:
		if !crypto.ValidAddress(tx.To) {
			return errors.New("invalid to address")
		}
		if tx.Amount == 0 {
			return errors.New("amount must be > 0")
		}
		if tx.TokenID == "" {
			return errors.New("token_id required (use CGO for the native coin)")
		}
	case TxCreateToken:
		if tx.Token == nil {
			return errors.New("token params required")
		}
		if !symbolRe.MatchString(tx.Token.Symbol) {
			return errors.New("symbol must be 3-8 chars, A-Z then A-Z0-9")
		}
		if tx.Token.Symbol == NativeToken {
			return errors.New("symbol CGO is reserved")
		}
		if tx.Token.Supply == 0 {
			return errors.New("supply must be > 0")
		}
		if tx.Token.Decimals > 12 {
			return errors.New("decimals max 12")
		}
	case TxMint:
		if tx.TokenID == "" || tx.TokenID == NativeToken {
			return errors.New("mint requires a non-native token_id")
		}
		if tx.Amount == 0 {
			return errors.New("amount must be > 0")
		}
	case TxStake, TxUnstake:
		if tx.Amount == 0 {
			return errors.New("amount must be > 0")
		}
	default:
		return fmt.Errorf("unknown tx type %q", tx.Type)
	}
	return tx.VerifySignature()
}

// VerifyAll validates a batch of transactions in parallel across all
// cores — ML-DSA verification is the throughput bottleneck, parallelism
// is what gets us past the TPS target.
func VerifyAll(txs []*Transaction) error {
	if len(txs) == 0 {
		return nil
	}
	workers := runtime.NumCPU()
	if workers > len(txs) {
		workers = len(txs)
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		fail error
	)
	ch := make(chan *Transaction, workers*2)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tx := range ch {
				if err := tx.ValidateBasic(); err != nil {
					mu.Lock()
					if fail == nil {
						fail = fmt.Errorf("tx %s: %w", tx.Hash(), err)
					}
					mu.Unlock()
				}
			}
		}()
	}
	for _, tx := range txs {
		ch <- tx
	}
	close(ch)
	wg.Wait()
	return fail
}
