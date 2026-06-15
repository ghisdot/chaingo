package consensus

import (
	"testing"

	"chaingo/internal/crypto"
	"chaingo/internal/state"
	"chaingo/internal/types"
)

// TestValidatorSetFrozenPerHeight : le quorum d'une hauteur se mesure contre le
// set FIGÉ à cette hauteur, jamais contre l'état vivant qui a pu changer (#5).
//
// Scénario : on fige le set de la hauteur h (3 validateurs égaux, total 3M).
// Ensuite l'état vivant change radicalement (une « baleine » de 100M rejoint).
// Les 3 validateurs figés doivent toujours pouvoir former le commit, et la
// baleine — absente du set figé — ne doit pas pouvoir voter pour h.
func TestValidatorSetFrozenPerHeight(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	vs := mkValidators(st, 3) // 3 × 1M = 3M de pouvoir total
	e := newEngine(st, vs[0])
	const h, hash = uint64(5), "HASHFROZEN"

	// Fige le set votant de h : 3 validateurs, total 3M.
	e.freezeSetLocked(h)
	frozen := e.setForHeight(h)
	if frozen.Total != 3_000_000*types.Unit {
		t.Fatalf("total figé attendu 3M, got %d", frozen.Total)
	}

	// L'état VIVANT change APRÈS le gel : une baleine de 100M rejoint.
	whale, _ := crypto.GenerateKeyPair()
	st.BootstrapStake(whale.Address(), 100_000_000*types.Unit)
	if st.TotalPower() <= frozen.Total {
		t.Fatal("préalable du test : l'état vivant doit avoir grossi")
	}

	// 2/3 du set figé (exactement) → insuffisant (règle stricte > 2/3).
	addVote(e, vs[0], h, hash)
	addVote(e, vs[1], h, hash)
	if c := e.buildLastCommit(h, hash); c != nil {
		t.Fatal("2/3 pile du set figé ne doit pas former de commit")
	}

	// 3/3 du set figé → commit. Si le quorum était (à tort) mesuré contre le
	// set vivant (103M), 3M ne dépasserait jamais 2/3 de 103M → pas de commit.
	// Le commit prouve donc qu'on utilise bien le set FIGÉ.
	addVote(e, vs[2], h, hash)
	c := e.buildLastCommit(h, hash)
	if len(c) != 3 {
		t.Fatalf("les 3 validateurs figés doivent former le commit, got %v", c)
	}
	power, err := e.verifyCommit(c, h, hash)
	if err != nil {
		t.Fatalf("verifyCommit a échoué: %v", err)
	}
	if !hasQuorum(power, frozen.Total) {
		t.Fatalf("3/3 du set figé doit atteindre le quorum (power=%d total=%d)", power, frozen.Total)
	}

	// La baleine, absente du set figé de h, ne peut pas voter pour h même si
	// son pouvoir VIVANT est énorme.
	wv := &types.Vote{ChainID: "test", Height: h, Kind: types.PrecommitKind, BlockHash: hash}
	wv.SignWith(whale)
	if isNew, err := e.AddVote(wv); err == nil || isNew {
		t.Fatal("un validateur absent du set figé ne doit pas pouvoir voter pour cette hauteur")
	}
}

// TestSetForHeightFallsBackToLive : si une hauteur n'a pas été figée
// explicitement (vote reçu sur la hauteur suivante, ou juste après un
// redémarrage), setForHeight retombe sur une photo de l'état courant — jamais
// pire que l'ancien comportement.
func TestSetForHeightFallsBackToLive(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	vs := mkValidators(st, 2) // 2M de pouvoir
	e := newEngine(st, vs[0])

	// Hauteur 9 jamais figée → repli sur l'état vivant.
	got := e.setForHeight(9)
	if got.Total != st.TotalPower() {
		t.Fatalf("repli attendu = état vivant (%d), got %d", st.TotalPower(), got.Total)
	}
	if got.PowerOf(vs[1].Address()) == 0 {
		t.Fatal("le repli doit contenir les validateurs actifs courants")
	}
}

// TestPruneSetsLocked : les sets figés des hauteurs finalisées sont oubliés
// (pas de fuite mémoire au fil des hauteurs).
func TestPruneSetsLocked(t *testing.T) {
	st := state.New()
	st.SetParams(types.DefaultParams())
	mkValidators(st, 2)
	e := newEngine(st, nil)

	for h := uint64(1); h <= 5; h++ {
		e.freezeSetLocked(h)
	}
	if len(e.setByHeight) != 5 {
		t.Fatalf("5 sets figés attendus, got %d", len(e.setByHeight))
	}
	e.pruneSetsLocked(3) // finalise jusqu'à 3
	if len(e.setByHeight) != 2 {
		t.Fatalf("après purge ≤3, il doit rester {4,5}, got %d entrées", len(e.setByHeight))
	}
	if _, ok := e.setByHeight[3]; ok {
		t.Fatal("le set de la hauteur 3 (finalisée) aurait dû être purgé")
	}
	if _, ok := e.setByHeight[4]; !ok {
		t.Fatal("le set de la hauteur 4 (non finalisée) doit subsister")
	}
}
