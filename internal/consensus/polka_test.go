package consensus

import (
	"testing"

	"chaingo/internal/crypto"
	"chaingo/internal/state"
	"chaingo/internal/types"
)

// addPrevote : émet un prevote signé d'un round donné dans le pool (via le
// chemin réseau AddVote, qui vérifie signature + appartenance au set).
func addPrevote(e *Engine, kp *keyPair, h uint64, round uint32, hash string) {
	v := &types.Vote{ChainID: "test", Height: h, Round: round, Kind: types.PrevoteKind, BlockHash: hash}
	v.SignWith(kp)
	e.AddVote(v)
}

// keyPair alias pour lisibilité locale (mkValidators renvoie des *crypto.KeyPair).
type keyPair = crypto.KeyPair

// TestPolkaDetection : une polka (≥ 2/3 de prevotes pour un (hauteur, round,
// hash)) est détectée à partir du quorum strict, mesurée contre le set FIGÉ.
func TestPolkaDetection(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	vs := mkValidators(st, 4) // 4 × 1M
	e := newEngine(st, vs[0])
	const h, hash = uint64(3), "BLOCK3"
	e.freezeSetLocked(h)

	// 2/4 de prevotes au round 0 → pas de polka (2/3 strict).
	addPrevote(e, vs[0], h, 0, hash)
	addPrevote(e, vs[1], h, 0, hash)
	if e.hasPolka(h, 0, hash) {
		t.Fatal("2/4 ne doit pas former de polka")
	}

	// 3/4 au round 0 → polka.
	addPrevote(e, vs[2], h, 0, hash)
	if !e.hasPolka(h, 0, hash) {
		t.Fatal("3/4 doit former une polka au round 0")
	}

	// La polka est SPÉCIFIQUE au round : rien au round 1.
	if e.hasPolka(h, 1, hash) {
		t.Fatal("aucune polka au round 1 (les prevotes étaient au round 0)")
	}

	// La polka est SPÉCIFIQUE au hash : rien sur un autre bloc.
	if e.hasPolka(h, 0, "AUTRE") {
		t.Fatal("aucune polka sur un hash différent")
	}
}

// TestPolkaIgnoresNonValidators : un prevote d'une adresse hors du set figé ne
// compte pas, même si l'état vivant lui donnerait du pouvoir.
func TestPolkaIgnoresNonValidators(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	vs := mkValidators(st, 3)
	e := newEngine(st, vs[0])
	const h, hash = uint64(3), "BLOCK3"
	e.freezeSetLocked(h) // set figé = 3 validateurs

	// 2/3 figés prevotent → pas encore quorum strict.
	addPrevote(e, vs[0], h, 0, hash)
	addPrevote(e, vs[1], h, 0, hash)
	if e.hasPolka(h, 0, hash) {
		t.Fatal("2/3 pile ne forme pas de polka")
	}
	// Une baleine rejoint l'état vivant et prevote — mais elle est hors du set
	// figé de la hauteur, donc son prevote ne compte pas.
	whale, _ := crypto.GenerateKeyPair()
	st.BootstrapStake(whale.Address(), 100_000_000*types.Unit)
	addPrevote(e, whale, h, 0, hash)
	if e.hasPolka(h, 0, hash) {
		t.Fatal("le prevote d'un non-membre du set figé ne doit pas créer de polka")
	}
	// Le 3e validateur figé complète le quorum.
	addPrevote(e, vs[2], h, 0, hash)
	if !e.hasPolka(h, 0, hash) {
		t.Fatal("3/3 du set figé doit former la polka")
	}
}
