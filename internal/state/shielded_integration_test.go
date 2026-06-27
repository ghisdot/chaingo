package state

import (
	"sync"
	"testing"

	"chaingo/internal/shieldedwallet"
	"chaingo/internal/stark"
	"chaingo/internal/types"
)

// ============================================================================
// TEST D'INTÉGRATION BOUT-EN-BOUT du pool blindé (étage 5.3)
//
// Cycle complet shield -> shielded_transfer -> unshield, avec PrivacyEnabled=true,
// en réutilisant UNE SEULE preuve zk-STARK (génération ~90 s) via sync.Once.
//
// La même preuve sert deux fois : elle est valide pour shielded_transfer ET pour
// unshield (les deux passent par verifySpendLocked, même vérification ; seul
// l'effet du montant public diffère — brûlé vs rendu). On vérifie donc, à partir
// d'une preuve unique :
//   1. conservation : les CGO shieldés == pool.Balance ;
//   2. shielded_transfer : nullifier marqué, Fee brûlé, double-dépense rejetée ;
//   3. unshield : montant public rendu à To, nullifier marqué, double-dépense
//      rejetée ;
//   4. la racine reconstruite côté wallet == pool.Root (alignement arbre/circuit).
// ============================================================================

// spendFixture : tout ce qui dépend de la preuve unique, mis en cache au niveau
// paquet. inValue/outValue/fee décrivent la dépense prouvée.
type spendFixture struct {
	inNote   shieldedwallet.Note // note d'entrée (dépensée)
	inValue  uint64
	outValue uint64
	fee      uint64
	public   stark.SpendNPublic
	proof    stark.AirProof
	poolRoot []byte // racine du pool au moment de la preuve (= public.MerkleRoot sérialisée)
}

var (
	spendOnce sync.Once
	spendFix  *spendFixture
)

// buildSpendFixture amorce un pool avec UNE note (via un shield réel), construit le
// témoin de dépense aligné sur la racine du pool, puis génère l'unique preuve. Le
// résultat est mis en cache : sharedSpend() ne génère JAMAIS plus d'une preuve.
func buildSpendFixture(t *testing.T) *spendFixture {
	t.Helper()
	spendOnce.Do(func() {
		const inValue = uint64(1_000_000)
		const fee = uint64(2_500)
		const outValue = inValue - fee

		// Propriétaire (nk) + aléa (rho) déterministes — la note dépensée.
		nk := shieldedwallet.DeriveNk([]byte("integration/spender"), 0)
		rho := shieldedwallet.DeriveRho([]byte("integration/spender"), 0)
		inNote := shieldedwallet.Note{Value: inValue, Nk: nk, Rho: rho}

		// Pool jetable : on y dépose la note (shield public) pour obtenir EXACTEMENT
		// la racine que la machine d'état recalcule, puis on lit ses engagements.
		st := New()
		st.SetParams(privacyParams())
		st.Mint("cg0000000000000000000000000000000000000001", inValue+10)
		seedShieldRaw(t, st, "cg0000000000000000000000000000000000000001", inValue, inNote.CommitmentBytes())

		pool := st.GetShieldedPool()
		if pool == nil || len(pool.Commitments) != 1 {
			t.Fatalf("pool d'amorçage incohérent")
		}

		// Note de sortie : reste de la valeur, propriétaire arbitraire (bénéficiaire).
		outNk := shieldedwallet.DeriveNk([]byte("integration/recipient"), 0)
		outRho := shieldedwallet.DeriveRho([]byte("integration/recipient"), 0)
		outNote := shieldedwallet.Note{Value: outValue, Nk: outNk, Rho: outRho}

		w, feeFelt, err := shieldedwallet.BuildWitness(pool.Commitments, shieldedwallet.SpendPlan{
			In: inNote, Out: outNote, Fee: fee,
		})
		if err != nil {
			t.Fatalf("BuildWitness: %v", err)
		}

		// UNIQUE génération de preuve (lente).
		public, proof := stark.ProveSpendN(w, feeFelt)

		spendFix = &spendFixture{
			inNote:   inNote,
			inValue:  inValue,
			outValue: outValue,
			fee:      fee,
			public:   public,
			proof:    proof,
			poolRoot: append([]byte(nil), pool.Root...),
		}
	})
	return spendFix
}

// seedShieldRaw applique un shield réel (note publique -> pool) et avance le bloc.
func seedShieldRaw(t *testing.T, st *State, from string, amount uint64, cm []byte) {
	t.Helper()
	tx := &types.Transaction{
		Type: types.TxShield, From: from, Amount: amount, MaxBaseFee: 1,
		ShieldCommitment: cm, ShieldNote: []byte("note-blob"),
	}
	// shield ne vérifie pas de signature au niveau Execute (ValidateBasic est fait
	// par le mempool/API, pas par Execute) ; on appelle Execute directement.
	if _, _, _, err := st.Execute([]*types.Transaction{tx}, nil, nil, "", 1_000, true); err != nil {
		t.Fatalf("seed shield: %v", err)
	}
}

// freshPoolWithProvedNote crée un état neuf dont le pool contient EXACTEMENT la note
// prouvée par la fixture (même racine que public.MerkleRoot), et finance `spender`
// pour qu'il puisse payer le ShieldFee de la dépense. Renvoie l'état prêt.
func freshPoolWithProvedNote(t *testing.T, fix *spendFixture, spender string) *State {
	t.Helper()
	st := New()
	p := privacyParams()
	p.ShieldFee = 1 // frais réseau non nuls : on vérifie qu'ils sont bien brûlés
	st.SetParams(p)
	st.Mint(spender, 1_000) // de quoi payer le ShieldFee
	// On dépose la note prouvée via un shield public (from = compte technique séparé
	// financé pour le dépôt), de sorte que la racine soit identique à la preuve.
	st.Mint("cg00000000000000000000000000000000000000ff", fix.inValue+10)
	seedShieldRaw(t, st, "cg00000000000000000000000000000000000000ff", fix.inValue, fix.inNote.CommitmentBytes())

	pool := st.GetShieldedPool()
	if string(pool.Root) != string(digestToBytes(fix.public.MerkleRoot)) {
		t.Fatalf("racine du pool reconstruit != racine de la preuve (arbre désaligné)")
	}
	return st
}

// TestShieldedCycleEndToEnd : le test d'intégration principal. Une preuve, deux
// usages (transfer puis unshield), plus conservation et anti double-dépense.
func TestShieldedCycleEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("preuve zk lourde (depth 12) — exclue en -short")
	}
	fix := buildSpendFixture(t)

	// --- Alignement arbre/circuit : la racine reconstruite côté wallet (via le
	// shield qui amorce le pool) coïncide avec public.MerkleRoot. ---
	if string(fix.poolRoot) != string(digestToBytes(fix.public.MerkleRoot)) {
		t.Fatal("racine d'amorçage != racine de la preuve")
	}

	// ===================================================================
	// LEG 1 — SHIELDED_TRANSFER (montant public Fee BRÛLÉ)
	// ===================================================================
	t.Run("shielded_transfer", func(t *testing.T) {
		spender := "cg0000000000000000000000000000000000000010"
		st := freshPoolWithProvedNote(t, fix, spender)

		poolBefore := st.GetShieldedPool()
		supplyBefore := st.GetSupply()
		// Conservation initiale : les CGO shieldés == pool.Balance.
		if poolBefore.Balance != fix.inValue {
			t.Fatalf("pool.Balance = %d, want %d (CGO shieldés)", poolBefore.Balance, fix.inValue)
		}

		tx := provedTx(types.TxShieldedTransfer, spender, "", fix)
		executeStateBlock(t, st, "", 2_000, tx)

		pool := st.GetShieldedPool()
		// 1) Nullifier marqué.
		nfKey := nullifierKey(fix.public.Nfs[0])
		if !pool.Nullifiers[nfKey] {
			t.Fatal("nullifier non marqué après shielded_transfer")
		}
		// 2) Note de sortie insérée (commitments passe de 1 à 2).
		if len(pool.Commitments) != 2 {
			t.Fatalf("commitments = %d, want 2 (note de sortie insérée)", len(pool.Commitments))
		}
		// 3) Conservation : Fee (montant public) brûlé depuis le pool ET la supply.
		if pool.Balance != fix.inValue-fix.fee {
			t.Fatalf("pool.Balance = %d, want %d (Fee brûlé)", pool.Balance, fix.inValue-fix.fee)
		}
		supplyAfter := st.GetSupply()
		// burn = Fee de la preuve (brûlé du pool) + ShieldFee réseau (1).
		if got := supplyBefore.Total - supplyAfter.Total; got != fix.fee+1 {
			t.Fatalf("baisse de supply = %d, want %d (Fee+ShieldFee)", got, fix.fee+1)
		}

		// 4) Double-dépense : rejouer la MÊME preuve (même nullifier) est refusé.
		dup := provedTx(types.TxShieldedTransfer, spender, "", fix)
		dup.Nonce = 1
		if _, _, _, err := st.Execute([]*types.Transaction{dup}, nil, nil, "", 2_100, true); err == nil {
			t.Fatal("double-dépense (nullifier déjà dépensé) aurait dû être rejetée")
		}
	})

	// ===================================================================
	// LEG 2 — UNSHIELD (montant public RENDU à To, pas brûlé)
	// ===================================================================
	t.Run("unshield", func(t *testing.T) {
		spender := "cg0000000000000000000000000000000000000020"
		recipient := "cg0000000000000000000000000000000000000021"
		st := freshPoolWithProvedNote(t, fix, spender)

		poolBefore := st.GetShieldedPool()
		supplyBefore := st.GetSupply()
		recipientBefore := st.GetAccount(recipient).Balances[types.NativeToken]

		tx := provedTx(types.TxUnshield, spender, recipient, fix)
		executeStateBlock(t, st, "", 3_000, tx)

		pool := st.GetShieldedPool()
		// 1) Nullifier marqué.
		if !pool.Nullifiers[nullifierKey(fix.public.Nfs[0])] {
			t.Fatal("nullifier non marqué après unshield")
		}
		// 2) Montant public (= Fee de la preuve) sorti du pool vers To.
		if pool.Balance != poolBefore.Balance-fix.fee {
			t.Fatalf("pool.Balance = %d, want %d (montant sorti)", pool.Balance, poolBefore.Balance-fix.fee)
		}
		recipientAfter := st.GetAccount(recipient).Balances[types.NativeToken]
		if recipientAfter-recipientBefore != fix.fee {
			t.Fatalf("To a reçu %d, want %d (montant public rendu)", recipientAfter-recipientBefore, fix.fee)
		}
		// 3) Conservation : le montant rendu n'est PAS brûlé (déplacement). Seul le
		// ShieldFee réseau (1) quitte la supply.
		supplyAfter := st.GetSupply()
		if got := supplyBefore.Total - supplyAfter.Total; got != 1 {
			t.Fatalf("baisse de supply = %d, want 1 (ShieldFee seul ; montant rendu non brûlé)", got)
		}

		// 4) Double-dépense rejetée.
		dup := provedTx(types.TxUnshield, spender, recipient, fix)
		dup.Nonce = 1
		if _, _, _, err := st.Execute([]*types.Transaction{dup}, nil, nil, "", 3_100, true); err == nil {
			t.Fatal("double-dépense unshield aurait dû être rejetée")
		}
	})

	// ===================================================================
	// GATE — la même tx prouvée est REFUSÉE quand PrivacyEnabled=false
	// ===================================================================
	t.Run("gate_off_rejects", func(t *testing.T) {
		spender := "cg0000000000000000000000000000000000000030"
		st := freshPoolWithProvedNote(t, fix, spender)
		// On ferme le verrou APRÈS l'amorçage (le shield d'amorçage a déjà servi).
		p := st.GetParams()
		p.PrivacyEnabled = false
		st.SetParams(p)

		tx := provedTx(types.TxShieldedTransfer, spender, "", fix)
		if _, _, _, err := st.Execute([]*types.Transaction{tx}, nil, nil, "", 4_000, true); err == nil {
			t.Fatal("tx blindée aurait dû être refusée quand PrivacyEnabled=false")
		}
	})
}

// provedTx construit une tx blindée portant la preuve unique de la fixture.
// `from` paie le ShieldFee ; `to` n'est requis que pour unshield.
func provedTx(typ types.TxType, from, to string, fix *spendFixture) *types.Transaction {
	return &types.Transaction{
		Type:        typ,
		From:        from,
		To:          to,
		MaxBaseFee:  1,
		SpendProof:  stark.MarshalSpendProof(fix.proof),
		SpendPublic: stark.MarshalSpendNPublic(fix.public),
		ShieldNote:  []byte("out-note-blob"),
	}
}
