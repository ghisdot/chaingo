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
	TxDelegate    TxType = "delegate"   // To = validateur
	TxUndelegate  TxType = "undelegate" // To = validateur
	TxUnjail      TxType = "unjail"     // un validateur jailé pour inactivité rejoint le set

	// Smart contracts no-code : des templates natifs, paramétrés à la
	// création — aucun code à écrire ni à auditer.
	TxContractCreate TxType = "contract_create"
	TxContractExec   TxType = "contract_exec"
)

// Templates de contrats disponibles.
const (
	TemplateVesting  = "vesting"  // fonds débloqués linéairement vers un bénéficiaire
	TemplateEscrow   = "escrow"   // séquestre acheteur/vendeur, arbitre optionnel
	TemplateMultisig = "multisig" // coffre M-of-N : N signataires, M approbations pour dépenser
)

// Actions exécutables sur un contrat.
const (
	ActionClaim   = "claim"   // vesting : le bénéficiaire récupère la part débloquée
	ActionRelease = "release" // escrow : libère les fonds vers le vendeur
	ActionRefund  = "refund"  // escrow : rembourse l'acheteur
	ActionPropose = "propose" // multisig : un signataire propose un paiement (To, Amount)
	ActionApprove = "approve" // multisig : un signataire approuve la proposition (Proposal)
)

type ContractParams struct {
	Template    string   `json:"template"`
	TokenID     string   `json:"token_id"`
	Amount      uint64   `json:"amount"`
	Beneficiary string   `json:"beneficiary,omitempty"` // vesting
	StartMs     int64    `json:"start_ms,omitempty"`    // vesting : début du déblocage
	EndMs       int64    `json:"end_ms,omitempty"`      // vesting : 100 % débloqué
	Seller      string   `json:"seller,omitempty"`      // escrow
	Arbiter     string   `json:"arbiter,omitempty"`     // escrow (optionnel)
	Signers     []string `json:"signers,omitempty"`     // multisig
	Threshold   uint64   `json:"threshold,omitempty"`   // multisig : nb d'approbations requis
}

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
	// Smart contracts no-code :
	Contract   *ContractParams `json:"contract,omitempty"`    // contract_create
	ContractID string          `json:"contract_id,omitempty"` // contract_exec
	Action     string          `json:"action,omitempty"`      // contract_exec
	Proposal   uint64          `json:"proposal,omitempty"`    // multisig approve : index de la proposition
	Timestamp  int64           `json:"timestamp"`
	Signature  []byte          `json:"signature,omitempty"`
}

// txCanonical : alias de Transaction sans méthodes. Sert à deux choses :
//   - SigningBytes appelle json.Marshal sur (*txCanonical), donc PAS sur la
//     MarshalJSON custom ci-dessous → pas de boucle infinie via tx.Hash().
//   - Garantit que la SÉRIALISATION SIGNÉE reste exactement la même qu'avant
//     l'ajout du champ "hash" en sortie API (invariant n°1 du projet).
type txCanonical Transaction

// SigningBytes returns the canonical bytes covered by the signature
// (struct field order is fixed, []byte marshals to base64 — deterministic).
func (tx *Transaction) SigningBytes() []byte {
	clone := *tx
	clone.Signature = nil
	b, _ := json.Marshal((*txCanonical)(&clone))
	return b
}

// MarshalJSON enrichit la sortie API avec le hash de la transaction —
// indispensable pour les explorateurs et les wallets (sinon le client devrait
// recalculer SHA3-256(SigningBytes) lui-même). Le champ "hash" n'est PAS
// dans SigningBytes : il dépend de la signature.
//
// Côté Unmarshal, le champ "hash" supplémentaire est silencieusement ignoré
// (Go json ignore les champs inconnus par défaut), donc compat ascendante.
func (tx *Transaction) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		*txCanonical
		Hash string `json:"hash"`
	}{(*txCanonical)(tx), tx.Hash()})
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
	case TxUnjail:
		// rien à valider hors signature : le compte doit être un validateur jailé (vérifié à l'exécution)
	case TxDelegate, TxUndelegate:
		if !crypto.ValidAddress(tx.To) {
			return errors.New("to must be a validator address")
		}
		if tx.To == tx.From {
			return errors.New("cannot delegate to yourself (use stake)")
		}
		if tx.Amount == 0 {
			return errors.New("amount must be > 0")
		}
	case TxContractCreate:
		c := tx.Contract
		if c == nil {
			return errors.New("contract params required")
		}
		if c.TokenID == "" || c.Amount == 0 {
			return errors.New("contract token_id and amount required")
		}
		switch c.Template {
		case TemplateVesting:
			if !crypto.ValidAddress(c.Beneficiary) {
				return errors.New("vesting: invalid beneficiary address")
			}
			if c.StartMs <= 0 || c.EndMs <= c.StartMs {
				return errors.New("vesting: end_ms must be after start_ms (> 0)")
			}
		case TemplateEscrow:
			if !crypto.ValidAddress(c.Seller) {
				return errors.New("escrow: invalid seller address")
			}
			if c.Seller == tx.From {
				return errors.New("escrow: seller must differ from buyer")
			}
			if c.Arbiter != "" && !crypto.ValidAddress(c.Arbiter) {
				return errors.New("escrow: invalid arbiter address")
			}
		case TemplateMultisig:
			if len(c.Signers) < 1 {
				return errors.New("multisig: at least one signer required")
			}
			seen := map[string]bool{}
			for _, sg := range c.Signers {
				if !crypto.ValidAddress(sg) {
					return errors.New("multisig: invalid signer address")
				}
				if seen[sg] {
					return errors.New("multisig: duplicate signer")
				}
				seen[sg] = true
			}
			if c.Threshold < 1 || c.Threshold > uint64(len(c.Signers)) {
				return errors.New("multisig: threshold must be between 1 and the number of signers")
			}
		default:
			return fmt.Errorf("unknown contract template %q (vesting|escrow|multisig)", c.Template)
		}
	case TxContractExec:
		if tx.ContractID == "" {
			return errors.New("contract_id required")
		}
		switch tx.Action {
		case ActionClaim, ActionRelease, ActionRefund, ActionApprove:
		case ActionPropose:
			if !crypto.ValidAddress(tx.To) {
				return errors.New("multisig propose: invalid recipient address")
			}
			if tx.Amount == 0 {
				return errors.New("multisig propose: amount must be > 0")
			}
		default:
			return fmt.Errorf("unknown action %q (claim|release|refund|propose|approve)", tx.Action)
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
