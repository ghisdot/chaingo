package genesis

import (
	"testing"

	"chaingo/internal/crypto"
	"chaingo/internal/state"
	"chaingo/internal/types"
)

// TestMainnetDistributionPattern : vérifie que la distribution mainnet
// annoncée (50/20/15/10/5 sur 1 Md CGO) tient on-chain — supply totale
// correcte, validateurs au-dessus du minimum, vesting verrouillé puis
// réclamable, empreinte de genèse déterministe.
//
// Schéma testé :
//   - 500 M alloc communauté
//   - 200 M vesting trésorerie (2 ans)
//   - 150 M vesting équipe (4 ans)
//   - 100 M alloc écosystème
//   -  20 M alloc + 30 M stakes pour les validateurs de genèse
func TestMainnetDistributionPattern(t *testing.T) {
	const (
		oneCGO    = uint64(types.Unit)            // 1 CGO = 10^9 ucgo
		oneMCGO   = uint64(1_000_000) * oneCGO    // 1 M  CGO
		oneBnCGO  = uint64(1_000_000_000) * oneCGO // 1 Md CGO
		startMs   = int64(1_750_000_000_000)
		twoYears  = int64(2 * 365 * 24 * 3600 * 1000)
		fourYears = int64(4 * 365 * 24 * 3600 * 1000)
	)

	// 4 validateurs de genèse (réseau réel : 3f+1 = 4 tolère 1 panne).
	const nVal = 4
	vKeys := make([]*crypto.KeyPair, nVal)
	for i := range vKeys {
		vKeys[i], _ = crypto.GenerateKeyPair()
	}
	community, _ := crypto.GenerateKeyPair()
	treasury, _ := crypto.GenerateKeyPair()
	team, _ := crypto.GenerateKeyPair()
	ecosystem, _ := crypto.GenerateKeyPair()

	p := types.DefaultParams()
	// Petit solde liquide aux bénéficiaires de vesting pour payer les frais
	// de claim (cf. note dans MAINNET.md). Soustrait de la part communauté
	// pour rester pile à 1 Md de supply totale.
	const dustPerVesting = 1 * oneCGO
	alloc := map[string]uint64{
		community.Address(): 500*oneMCGO - 2*dustPerVesting,
		ecosystem.Address(): 100 * oneMCGO,
		treasury.Address():  dustPerVesting,
		team.Address():      dustPerVesting,
	}
	stakes := map[string]uint64{}
	const liquidPerValidator = uint64(5 * 1_000_000) * oneCGO // 5 M CGO liquide
	const stakedPerValidator = uint64(7_500_000) * oneCGO     // 7,5 M CGO staké
	for _, k := range vKeys {
		alloc[k.Address()] = liquidPerValidator
		stakes[k.Address()] = stakedPerValidator
	}
	g := &Genesis{
		ChainID:   "chaingo-mainnet-test",
		Timestamp: startMs,
		Params:    &p,
		Alloc:     alloc,
		Stakes:    stakes,
		Vesting: []VestingGrant{
			{Beneficiary: treasury.Address(), Amount: 200 * oneMCGO, StartMs: startMs, EndMs: startMs + twoYears},
			{Beneficiary: team.Address(), Amount: 150 * oneMCGO, StartMs: startMs, EndMs: startMs + fourYears},
		},
	}

	// 1) Validation et résumé
	sum, err := g.Validate()
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	t.Logf("supply totale = %d M CGO (liquide=%d M, staké=%d M, vesting=%d M)",
		sum.TotalSupply/oneMCGO, sum.Liquid/oneMCGO, sum.Staked/oneMCGO, sum.Vested/oneMCGO)

	if sum.TotalSupply != oneBnCGO {
		t.Fatalf("supply totale = %d, attendu %d (1 Md CGO)", sum.TotalSupply, oneBnCGO)
	}
	if sum.Validators != nVal {
		t.Fatalf("nb validateurs = %d, attendu %d", sum.Validators, nVal)
	}

	// 2) Application : la supply on-chain doit correspondre.
	st := state.New()
	gb := g.Apply(st)
	supply := st.GetSupply()
	if supply.Total != oneBnCGO {
		t.Fatalf("supply on-chain = %d, attendu %d", supply.Total, oneBnCGO)
	}

	// 3) Communauté + écosystème immédiatement disponibles (alloc liquide).
	wantCommunity := 500*oneMCGO - 2*dustPerVesting
	if got := st.GetAccount(community.Address()).Balances[types.NativeToken]; got != wantCommunity {
		t.Fatalf("solde communauté = %d, attendu %d (~500 M − dust)", got, wantCommunity)
	}
	if got := st.GetAccount(ecosystem.Address()).Balances[types.NativeToken]; got != 100*oneMCGO {
		t.Fatalf("solde écosystème = %d, attendu %d (100 M)", got, 100*oneMCGO)
	}

	// 4) Équipe et trésorerie ne doivent PAS avoir le vesting dans leur solde
	// liquide (seulement le 1 CGO d'amorce pour les frais).
	if got := st.GetAccount(team.Address()).Balances[types.NativeToken]; got != dustPerVesting {
		t.Fatalf("solde équipe = %d, attendu %d (dust d'amorce)", got, dustPerVesting)
	}
	if got := st.GetAccount(treasury.Address()).Balances[types.NativeToken]; got != dustPerVesting {
		t.Fatalf("solde trésorerie = %d, attendu %d (dust d'amorce)", got, dustPerVesting)
	}

	// 5) Validateurs : stake ≥ min, dans le set actif, poids cumulé = 30 M.
	if got := st.TotalPower(); got != stakedPerValidator*nVal {
		t.Fatalf("poids actif total = %d, attendu %d", got, stakedPerValidator*nVal)
	}
	for _, k := range vKeys {
		if w := st.PowerOf(k.Address()); w != stakedPerValidator {
			t.Fatalf("poids du validateur %s = %d, attendu %d", k.Address(), w, stakedPerValidator)
		}
		if stakedPerValidator < p.MinValidatorStake {
			t.Fatalf("stake par validateur (%d) sous le minimum (%d)", stakedPerValidator, p.MinValidatorStake)
		}
	}

	// 6) Empreinte déterministe : Apply sur deux états neufs → même hash.
	st2 := state.New()
	gb2 := g.Apply(st2)
	if gb.Hash != gb2.Hash {
		t.Fatalf("genèse non déterministe : %s vs %s", gb.Hash, gb2.Hash)
	}
	if gb.Header.StateRoot != gb2.Header.StateRoot {
		t.Fatal("racine d'état de genèse non déterministe")
	}

	// 7) Vesting équipe : à mi-parcours (2 ans), réclamable = moitié.
	contracts := st.ListContracts()
	if len(contracts) != 2 {
		t.Fatalf("2 contrats de vesting attendus, got %d", len(contracts))
	}
	// Trouver le contrat équipe (durée 4 ans = vesting le plus long).
	var teamContract *state.Contract
	for _, c := range contracts {
		if c.Beneficiary == team.Address() {
			teamContract = c
			break
		}
	}
	if teamContract == nil {
		t.Fatal("contrat de vesting équipe introuvable")
	}
	midTeam := teamContract.StartMs + (teamContract.EndMs-teamContract.StartMs)/2

	claim := &types.Transaction{
		Type: types.TxContractExec, ContractID: teamContract.ID,
		Action: types.ActionClaim, MaxBaseFee: 1 * oneCGO,
	}
	claim.SignWith(team)
	if _, _, _, err := st.Execute([]*types.Transaction{claim}, nil, nil, "", midTeam, true); err != nil {
		t.Fatalf("vesting claim équipe à mi-parcours : %v", err)
	}
	teamLiquid := st.GetAccount(team.Address()).Balances[types.NativeToken]
	halfTeam := 75 * oneMCGO // moitié de 150 M
	if teamLiquid < halfTeam-1_000_000 || teamLiquid > halfTeam+oneCGO {
		t.Fatalf("réclamé à mi-parcours = %d, attendu ~%d (75 M)", teamLiquid, halfTeam)
	}
	t.Logf("vesting équipe à T+2ans : réclamé %d M CGO", teamLiquid/oneMCGO)

	// 8) Au-delà de end_ms : tout débloqué.
	claim2 := &types.Transaction{
		Type: types.TxContractExec, ContractID: teamContract.ID,
		Action: types.ActionClaim, Nonce: 1, MaxBaseFee: 1 * oneCGO,
	}
	claim2.SignWith(team)
	endPlus := teamContract.EndMs + 1
	if _, _, _, err := st.Execute([]*types.Transaction{claim2}, nil, nil, "", endPlus, true); err != nil {
		t.Fatalf("vesting claim final équipe : %v", err)
	}
	teamFinal := st.GetAccount(team.Address()).Balances[types.NativeToken]
	if teamFinal < 150*oneMCGO {
		t.Fatalf("réclamé final = %d, attendu ≥ 150 M", teamFinal/oneMCGO)
	}
	t.Logf("vesting équipe à T+4ans : total réclamé %d M CGO ✓", teamFinal/oneMCGO)
}
