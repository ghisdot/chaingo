package types

import "math/big"

// Params : les règles économiques et de consensus de la chaîne.
// Elles vivent dans le document de genèse (et donc dans l'état) — chaque
// réseau ChainGO choisit les siennes, rien n'est codé en dur.
type Params struct {
	BlockIntervalMs    int64  `json:"block_interval_ms"`
	MaxBlockTxs        uint64 `json:"max_block_txs"`
	InflationRateBps   uint64 `json:"inflation_rate_bps"`    // points de base/an sur le stake total (300 = 3 %)
	MinBaseFee         uint64 `json:"min_base_fee"`          // plancher du base fee (ucgo)
	TargetBlockTxs     uint64 `json:"target_block_txs"`      // cible EIP-1559 : remplissage « normal » d'un bloc
	BaseFeeChangeDenom uint64 `json:"base_fee_change_denom"` // 8 → variation max ±12.5 % par bloc
	PrivacyFeeMult     uint64 `json:"privacy_fee_mult"`      // burn supplémentaire mode private = mult × base fee
	TokenCreateFee     uint64 `json:"token_create_fee"`      // brûlé à la création d'un token
	MinValidatorStake  uint64 `json:"min_validator_stake"`
	UnbondingSeconds   int64  `json:"unbonding_seconds"`
	// Délégation : les holders sous le stake minimum délèguent à un
	// validateur et touchent leur part des récompenses, moins la
	// commission du validateur.
	MinDelegation           uint64 `json:"min_delegation"`
	DelegationCommissionBps uint64 `json:"delegation_commission_bps"` // 1000 = 10 %
}

func DefaultParams() Params {
	return Params{
		BlockIntervalMs:         500,
		MaxBlockTxs:             2000,
		InflationRateBps:        300,
		MinBaseFee:              100_000, // 0.0001 CGO
		TargetBlockTxs:          1000,
		BaseFeeChangeDenom:      8,
		PrivacyFeeMult:          2,
		TokenCreateFee:          10 * Unit,
		MinValidatorStake:       10_000 * Unit,
		UnbondingSeconds:        21 * 24 * 3600, // 21 jours
		MinDelegation:           1 * Unit,
		DelegationCommissionBps: 1000, // 10 % pour le validateur
	}
}

// MulDiv : a × b / c sans débordement (big.Int) — utilisé pour les
// partages de récompenses, qui doivent être identiques sur tous les nœuds.
func MulDiv(a, b, c uint64) uint64 {
	if c == 0 {
		return 0
	}
	r := new(big.Int).SetUint64(a)
	r.Mul(r, new(big.Int).SetUint64(b))
	r.Div(r, new(big.Int).SetUint64(c))
	return r.Uint64()
}

const msPerYear = int64(365 * 24 * 3600 * 1000)

// RewardPerBlock : émission par bloc ≈ stake_total × taux_annuel × durée
// du bloc / an. Le proposeur étant tiré au sort proportionnellement à son
// stake, le rendement attendu de chaque validateur est ~InflationRateBps/an.
// big.Int pour éviter tout débordement : le calcul doit être identique sur
// tous les nœuds.
func RewardPerBlock(totalStaked uint64, p Params) uint64 {
	r := new(big.Int).SetUint64(totalStaked)
	r.Mul(r, new(big.Int).SetUint64(p.InflationRateBps))
	r.Mul(r, big.NewInt(p.BlockIntervalMs))
	r.Div(r, big.NewInt(10_000))
	r.Div(r, big.NewInt(msPerYear))
	return r.Uint64()
}

// NextBaseFee : ajustement EIP-1559 — le base fee monte quand les blocs
// dépassent la cible, descend sinon, sans jamais passer sous le plancher.
func NextBaseFee(current uint64, blockTxs int, p Params) uint64 {
	if p.TargetBlockTxs == 0 {
		return current
	}
	delta := new(big.Int).SetUint64(current)
	delta.Mul(delta, big.NewInt(int64(blockTxs)-int64(p.TargetBlockTxs)))
	delta.Div(delta, new(big.Int).SetUint64(p.TargetBlockTxs))
	delta.Div(delta, new(big.Int).SetUint64(p.BaseFeeChangeDenom))
	next := new(big.Int).SetUint64(current)
	next.Add(next, delta)
	min := new(big.Int).SetUint64(p.MinBaseFee)
	if next.Cmp(min) < 0 {
		return p.MinBaseFee
	}
	return next.Uint64()
}
