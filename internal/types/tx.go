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
	TxBurn        TxType = "burn" // le détenteur détruit ses propres jetons (token burnable)
	TxStake       TxType = "stake"
	TxUnstake     TxType = "unstake"
	TxDelegate    TxType = "delegate"   // To = validateur
	TxUndelegate  TxType = "undelegate" // To = validateur
	TxUnjail      TxType = "unjail"     // un validateur jailé pour inactivité rejoint le set

	// Smart contracts no-code : des templates natifs, paramétrés à la
	// création — aucun code à écrire ni à auditer.
	TxContractCreate TxType = "contract_create"
	TxContractExec   TxType = "contract_exec"

	// Contrats WASM arbitraires (déploiement de bytecode, façon EVM mais en
	// WebAssembly). wasm_deploy stocke du bytecode validé ; wasm_call l'exécute.
	// N'est accepté que si Params.WasmEnabled (off sur mainnet jusqu'à audit).
	TxWasmDeploy TxType = "wasm_deploy"
	TxWasmCall   TxType = "wasm_call"

	// Pool blindé (zk-STARK maison, étage 5). N'est accepté que si
	// Params.PrivacyEnabled (off sur mainnet jusqu'à audit) :
	//   - shield            : dépose des CGO PUBLICS dans le pool, crée une note
	//                         (engagement ShieldCommitment, blob chiffré ShieldNote).
	//                         Montant PUBLIC (Amount).
	//   - shielded_transfer : dépense une note et en crée une autre, montants
	//                         CACHÉS, prouvé par SpendProof. Seul Fee est public
	//                         (brûlé).
	//   - unshield          : dépense une note ; le montant public (= Fee de la
	//                         preuve) sort du pool vers To en CGO public.
	TxShield           TxType = "shield"
	TxShieldedTransfer TxType = "shielded_transfer"
	TxUnshield         TxType = "unshield"

	// Profil public d'un validateur (nom, site, description) — porté dans le
	// champ Memo (déjà sérialisé partout, donc aucun changement de codec).
	// Affiché par l'explorateur / le dashboard validateur.
	TxValidatorProfile TxType = "validator_profile"
)

// Templates de contrats disponibles.
const (
	TemplateVesting  = "vesting"  // fonds débloqués linéairement vers un bénéficiaire
	TemplateEscrow   = "escrow"   // séquestre acheteur/vendeur, arbitre optionnel
	TemplateMultisig = "multisig" // coffre M-of-N : N signataires, M approbations pour dépenser
	TemplateDAO      = "dao"      // gouvernance : membres, trésorerie, propositions votées POUR/CONTRE
	// Nouveaux templates :
	TemplatePresale   = "presale"   // vente d'un token à prix fixe contre CGO
	TemplateTimelock  = "timelock"  // fonds verrouillés jusqu'à une date, puis réclamables en totalité
	TemplateAirdrop   = "airdrop"   // distribution d'un token, part égale réclamable par destinataire
	TemplateStreaming = "streaming" // flux linéaire vers un bénéficiaire, annulable par le créateur
)

// Actions exécutables sur un contrat.
const (
	ActionClaim   = "claim"   // vesting : le bénéficiaire récupère la part débloquée
	ActionRelease = "release" // escrow : libère les fonds vers le vendeur
	ActionRefund  = "refund"  // escrow : rembourse l'acheteur
	ActionPropose = "propose" // multisig/dao : proposer un paiement depuis la trésorerie (To, Amount)
	ActionApprove = "approve" // multisig/dao : approuver / voter POUR la proposition (Proposal)
	ActionReject  = "reject"  // dao : voter CONTRE la proposition (Proposal)
	ActionBuy     = "buy"     // presale : acheter des tokens en envoyant des CGO (Amount)
	ActionCancel  = "cancel"  // streaming/presale : le créateur clôt et récupère le reste
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
	Signers     []string `json:"signers,omitempty"`     // multisig/dao : signataires/membres ; airdrop : destinataires
	Threshold   uint64   `json:"threshold,omitempty"`   // multisig : nb d'approbations requis
	// Ajouté EN FIN (l'ordre EST le format de signature) :
	Price uint64 `json:"price,omitempty"` // presale : prix en ucgo par unité de base du token vendu
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
	// Champs AJOUTÉS en fin de struct (l'ordre EST le format de signature JSON
	// canonique : ne jamais réordonner ; les nouveaux champs vont à la fin, en
	// omitempty pour que les anciennes tx produisent des octets identiques).
	MaxSupply   uint64 `json:"max_supply,omitempty"`  // plafond dur (0 = illimité) ; exige Mintable
	Burnable    bool   `json:"burnable,omitempty"`    // tout détenteur peut brûler ses jetons
	LogoURI     string `json:"logo_uri,omitempty"`    // métadonnée d'affichage (wallet/explorer)
	Description string `json:"description,omitempty"` // métadonnée d'affichage
	Website     string `json:"website,omitempty"`     // métadonnée d'affichage
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
	ContractID string          `json:"contract_id,omitempty"` // contract_exec / wasm_call : adresse du contrat
	Action     string          `json:"action,omitempty"`      // contract_exec / wasm_call : nom de la fonction
	Proposal   uint64          `json:"proposal,omitempty"`    // multisig approve : index de la proposition
	// Contrats WASM arbitraires :
	Code []byte   `json:"code,omitempty"` // wasm_deploy : bytecode du contrat
	Args []uint64 `json:"args,omitempty"` // wasm_call : arguments (i64) passés à la fonction
	Gas  uint64   `json:"gas,omitempty"`  // wasm_call : plafond de gas (borne d'arrêt déterministe)
	// Pool blindé (zk-STARK maison) :
	//   - ShieldCommitment : engagement de note (cm = [4]Felt sérialisé) — shield.
	//   - ShieldNote       : blob chiffré destiné au bénéficiaire (opaque au
	//                        consensus) — shield / shielded_transfer / unshield.
	//   - SpendProof       : preuve de dépense sérialisée (stark.MarshalSpendProof)
	//                        — shielded_transfer / unshield.
	//   - SpendPublic      : énoncé public sérialisé (stark.MarshalSpendPublic)
	//                        — shielded_transfer / unshield.
	ShieldCommitment []byte `json:"shield_commitment,omitempty"`
	ShieldNote       []byte `json:"shield_note,omitempty"`
	SpendProof       []byte `json:"spend_proof,omitempty"`
	SpendPublic      []byte `json:"spend_public,omitempty"`
	Timestamp int64  `json:"timestamp"`
	Signature []byte `json:"signature,omitempty"`
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
		// Plafond max-supply : optionnel, mais n'a de sens que pour un token
		// mintable, et doit pouvoir contenir au moins le supply initial.
		if tx.Token.MaxSupply > 0 {
			if !tx.Token.Mintable {
				return errors.New("max_supply requires a mintable token")
			}
			if tx.Token.MaxSupply < tx.Token.Supply {
				return errors.New("max_supply must be >= initial supply")
			}
		}
		// Métadonnées : bornées pour éviter le ballonnement de l'état.
		if len(tx.Token.LogoURI) > 256 || len(tx.Token.Website) > 256 {
			return errors.New("logo_uri/website max 256 chars")
		}
		if len(tx.Token.Description) > 512 {
			return errors.New("description max 512 chars")
		}
	case TxMint:
		if tx.TokenID == "" || tx.TokenID == NativeToken {
			return errors.New("mint requires a non-native token_id")
		}
		if tx.Amount == 0 {
			return errors.New("amount must be > 0")
		}
	case TxBurn:
		if tx.TokenID == "" || tx.TokenID == NativeToken {
			return errors.New("burn requires a non-native token_id")
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
		case TemplateDAO:
			// Membres = Signers, quorum POUR = Threshold (même validation que multisig).
			if len(c.Signers) < 1 {
				return errors.New("dao: at least one member required")
			}
			seen := map[string]bool{}
			for _, m := range c.Signers {
				if !crypto.ValidAddress(m) {
					return errors.New("dao: invalid member address")
				}
				if seen[m] {
					return errors.New("dao: duplicate member")
				}
				seen[m] = true
			}
			if c.Threshold < 1 || c.Threshold > uint64(len(c.Signers)) {
				return errors.New("dao: threshold (quorum) must be between 1 and the number of members")
			}
		case TemplateTimelock:
			if !crypto.ValidAddress(c.Beneficiary) {
				return errors.New("timelock: invalid beneficiary address")
			}
			if c.EndMs <= 0 {
				return errors.New("timelock: end_ms (unlock time) must be > 0")
			}
		case TemplateStreaming:
			if !crypto.ValidAddress(c.Beneficiary) {
				return errors.New("streaming: invalid beneficiary address")
			}
			if c.StartMs <= 0 || c.EndMs <= c.StartMs {
				return errors.New("streaming: end_ms must be after start_ms (> 0)")
			}
		case TemplateAirdrop:
			if len(c.Signers) < 1 {
				return errors.New("airdrop: at least one recipient required")
			}
			seen := map[string]bool{}
			for _, r := range c.Signers {
				if !crypto.ValidAddress(r) {
					return errors.New("airdrop: invalid recipient address")
				}
				if seen[r] {
					return errors.New("airdrop: duplicate recipient")
				}
				seen[r] = true
			}
			if c.Amount < uint64(len(c.Signers)) {
				return errors.New("airdrop: amount must cover at least 1 base unit per recipient")
			}
		case TemplatePresale:
			if c.TokenID == NativeToken {
				return errors.New("presale: token_id must be a non-native token (the one being sold)")
			}
			if c.Price == 0 {
				return errors.New("presale: price (ucgo per token base unit) must be > 0")
			}
		default:
			return fmt.Errorf("unknown contract template %q", c.Template)
		}
	case TxContractExec:
		if tx.ContractID == "" {
			return errors.New("contract_id required")
		}
		switch tx.Action {
		case ActionClaim, ActionRelease, ActionRefund, ActionApprove, ActionReject, ActionCancel:
		case ActionBuy:
			if tx.Amount == 0 {
				return errors.New("buy: amount (CGO to spend) must be > 0")
			}
		case ActionPropose:
			if !crypto.ValidAddress(tx.To) {
				return errors.New("propose: invalid recipient address")
			}
			if tx.Amount == 0 {
				return errors.New("propose: amount must be > 0")
			}
		default:
			return fmt.Errorf("unknown action %q (claim|release|refund|propose|approve|reject)", tx.Action)
		}
	case TxShield:
		// Dépôt de CGO publics dans le pool : montant public + note à insérer.
		// La VÉRIFICATION de la preuve n'a pas lieu ici (shield n'en a pas) ;
		// les structures fines (taille du commitment) sont vérifiées à l'exécution.
		if tx.Amount == 0 {
			return errors.New("shield: amount must be > 0")
		}
		if len(tx.ShieldCommitment) == 0 {
			return errors.New("shield: shield_commitment required")
		}
		if len(tx.ShieldNote) == 0 {
			return errors.New("shield: shield_note required")
		}
	case TxShieldedTransfer:
		// Transfert blindé : montants cachés, prouvés. On exige juste la présence
		// des blobs ; la VÉRIFICATION de la preuve (VerifySpend) est à l'exécution.
		if len(tx.SpendProof) == 0 {
			return errors.New("shielded_transfer: spend_proof required")
		}
		if len(tx.SpendPublic) == 0 {
			return errors.New("shielded_transfer: spend_public required")
		}
		if len(tx.ShieldNote) == 0 {
			return errors.New("shielded_transfer: shield_note required")
		}
	case TxUnshield:
		// Sortie du pool vers une adresse PUBLIQUE : To requis. Le montant public
		// est porté par la preuve (SpendPublic.Fee) — pas dans Amount. La preuve
		// est vérifiée à l'exécution.
		if !crypto.ValidAddress(tx.To) {
			return errors.New("unshield: invalid to address")
		}
		if len(tx.SpendProof) == 0 {
			return errors.New("unshield: spend_proof required")
		}
		if len(tx.SpendPublic) == 0 {
			return errors.New("unshield: spend_public required")
		}
	case TxWasmDeploy:
		if len(tx.Code) == 0 {
			return errors.New("wasm_deploy: code (bytecode) required")
		}
		// La validation fine du bytecode (opcodes, taille max, gate WasmEnabled)
		// est faite à l'exécution avec les Params de la chaîne.
	case TxWasmCall:
		if tx.ContractID == "" {
			return errors.New("wasm_call: contract_id required")
		}
		if tx.Action == "" {
			return errors.New("wasm_call: action (nom de la fonction) required")
		}
	case TxValidatorProfile:
		// Le profil (nom/site/description) est porté par Memo (≤ 256, déjà vérifié).
		if len(tx.Memo) == 0 {
			return errors.New("validator_profile: memo (profil public) requis")
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
