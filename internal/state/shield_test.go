package state

import (
	"testing"

	"chaingo/internal/stark"
	"chaingo/internal/types"
)

// privacyParams : Params de test avec le pool blindé ACTIVÉ et un ShieldFee nul
// (les frais réseau sont testés à part ; ici on isole la mécanique du pool).
func privacyParams() types.Params {
	p := testParams()
	p.PrivacyEnabled = true
	p.ShieldFee = 0
	return p
}

// cmBytes : sérialise un digest [4]Felt en 32 octets big-endian, EXACTEMENT comme
// le fait l'état (digestToBytes) — c'est le format d'un ShieldCommitment.
func cmBytes(d [4]stark.Felt) []byte {
	out := make([]byte, 0, 32)
	for k := 0; k < 4; k++ {
		out = append(out, d[k].Bytes()...)
	}
	return out
}

// sampleCommitment : un engagement de note Poseidon plausible (valeur + ownerTag
// + rho arbitraires). Pas besoin de preuve : shield ne dépense rien.
func sampleCommitment(seed uint64) []byte {
	var ownerTag, rho [4]stark.Felt
	for k := 0; k < 4; k++ {
		ownerTag[k] = stark.FromUint64(seed + uint64(k) + 1)
		rho[k] = stark.FromUint64(seed*1000 + uint64(k) + 1)
	}
	cm := stark.SpendCommit(stark.FromUint64(42+seed), ownerTag, rho)
	return cmBytes(cm)
}

// TestShieldDisabledRejectsAllThreeTxs : avec PrivacyEnabled=false (posture
// mainnet), les trois tx blindées sont REFUSÉES — le verrou de sûreté tient.
func TestShieldDisabledRejectsAllThreeTxs(t *testing.T) {
	st := New()
	p := privacyParams()
	p.PrivacyEnabled = false // verrou fermé
	st.SetParams(p)
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000_000)

	txs := []*types.Transaction{
		{Type: types.TxShield, Amount: 100, MaxBaseFee: 1, ShieldCommitment: sampleCommitment(1), ShieldNote: []byte("n")},
		{Type: types.TxShieldedTransfer, MaxBaseFee: 1, SpendProof: []byte{1}, SpendPublic: []byte{2}, ShieldNote: []byte("n")},
		{Type: types.TxUnshield, To: alice.Address(), MaxBaseFee: 1, SpendProof: []byte{1}, SpendPublic: []byte{2}},
	}
	for i, tx := range txs {
		tx.Nonce = 0
		tx.SignWith(alice)
		if _, _, _, err := st.Execute([]*types.Transaction{tx}, nil, nil, "", 1_000, true); err == nil {
			t.Fatalf("[%d] tx blindée devrait être refusée quand PrivacyEnabled=false", i)
		}
	}
	// Le pool ne doit même pas avoir été créé (aucune mutation).
	if st.GetShieldedPool() != nil {
		t.Fatal("le pool ne doit pas exister après des tx refusées par le gate")
	}
}

// TestEmptyPoolRootByteIdenticalToNoField : l'invariant de NON-FORK. Tant que le
// champ Shielded reste nil (omitempty), il est ABSENT du JSON de la racine, donc
// la racine est OCTET-POUR-OCTET identique à celle d'un état sans le champ. On le
// prouve en montrant que : (a) la racine avec Shielded=nil est stable, et (b) dès
// qu'on attache un pool non vide la racine CHANGE, puis (c) remettre nil restaure
// EXACTEMENT la racine d'origine. Ainsi un état dont le pool n'a jamais servi ne
// peut pas forker une chaîne existante.
func TestEmptyPoolRootByteIdenticalToNoField(t *testing.T) {
	st := New()
	st.SetParams(privacyParams())
	st.Mint("cg982fbc54bcbe39cc078d1ed519a0cf228309f44a", 1_000)

	// (a) Pool jamais touché => Shielded nil.
	if st.GetShieldedPool() != nil {
		t.Fatal("Shielded devrait être nil tant qu'aucune tx blindée n'a servi")
	}
	rootNoPool := st.Root()

	// (b) On attache un pool NON VIDE : la racine doit changer.
	st.mu.Lock()
	st.Shielded = &ShieldedPool{
		Commitments: [][]byte{sampleCommitment(1)},
		Root:        sampleCommitment(1),
		Nullifiers:  map[string]bool{},
		Balance:     100,
	}
	rootWithPool := st.rootLocked()
	st.mu.Unlock()
	if rootWithPool == rootNoPool {
		t.Fatal("un pool non vide DOIT changer la racine")
	}

	// (c) On remet le champ à nil : la racine doit revenir EXACTEMENT à l'origine
	// (preuve que omitempty rend le champ absent, octet-pour-octet, quand nil).
	st.mu.Lock()
	st.Shielded = nil
	rootCleared := st.rootLocked()
	st.mu.Unlock()
	if rootCleared != rootNoPool {
		t.Fatalf("racine après remise à nil (%s) != racine d'origine (%s) — l'invariant omitempty est cassé",
			rootCleared, rootNoPool)
	}
}

// TestShieldRefusedByGateLeavesPoolUntouched : une tx shield refusée par le gate
// (PrivacyEnabled=false) ne crée AUCUN pool — donc, à elle seule, ne peut pas
// faire diverger la racine d'une chaîne où le pool n'existe pas (au-delà des
// effets de bloc normaux indépendants du pool).
func TestShieldRefusedByGateLeavesPoolUntouched(t *testing.T) {
	st := New()
	p := privacyParams()
	p.PrivacyEnabled = false
	st.SetParams(p)
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000)

	tx := &types.Transaction{Type: types.TxShield, Amount: 1, MaxBaseFee: 1,
		ShieldCommitment: sampleCommitment(1), ShieldNote: []byte("n")}
	tx.SignWith(alice)
	if _, _, _, err := st.Execute([]*types.Transaction{tx}, nil, nil, "", 1_000, true); err == nil {
		t.Fatal("shield devrait être refusé quand PrivacyEnabled=false")
	}
	if st.GetShieldedPool() != nil {
		t.Fatal("aucun pool ne doit être créé par une tx refusée par le gate")
	}
}

// TestShieldPublicDepositMovesCgoIntoPool : le flux PUBLIC d'un dépôt (shield),
// SANS preuve : Amount quitte le compte, entre dans pool.Balance, la note est
// insérée, la racine est recalculée et le pool apparaît dans l'état.
func TestShieldPublicDepositMovesCgoIntoPool(t *testing.T) {
	st := New()
	p := privacyParams()
	p.ShieldFee = 5 // frais réseau brûlés non nuls pour vérifier la comptabilité
	st.SetParams(p)
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000)

	supplyBefore := st.GetSupply()
	balBefore := st.GetAccount(alice.Address()).Balances[types.NativeToken]

	cm := sampleCommitment(7)
	shield := &types.Transaction{
		Type: types.TxShield, Amount: 100, MaxBaseFee: 1,
		ShieldCommitment: cm, ShieldNote: []byte("blob"),
	}
	shield.SignWith(alice)
	executeStateBlock(t, st, "", 1_000, shield)

	pool := st.GetShieldedPool()
	if pool == nil {
		t.Fatal("le pool devrait exister après un shield")
	}
	if pool.Balance != 100 {
		t.Fatalf("pool.Balance = %d, want 100", pool.Balance)
	}
	if len(pool.Commitments) != 1 {
		t.Fatalf("pool a %d engagements, want 1", len(pool.Commitments))
	}
	if len(pool.Notes) != 1 {
		t.Fatalf("pool a %d notes, want 1", len(pool.Notes))
	}
	if len(pool.Root) != 32 {
		t.Fatalf("racine du pool de taille %d, want 32", len(pool.Root))
	}

	// Comptabilité du compte : Amount (100) + base fee (0) + ShieldFee (5) = 105
	// quittent le compte. La supply baisse de 5 (ShieldFee brûlé) ; les 100 sont
	// juste déplacés dans le pool (toujours dans la supply).
	balAfter := st.GetAccount(alice.Address()).Balances[types.NativeToken]
	if balBefore-balAfter != 105 {
		t.Fatalf("débit du compte = %d, want 105 (100 dépôt + 5 ShieldFee)", balBefore-balAfter)
	}
	supplyAfter := st.GetSupply()
	if supplyBefore.Total-supplyAfter.Total != 5 {
		t.Fatalf("baisse de supply = %d, want 5 (ShieldFee brûlé)", supplyBefore.Total-supplyAfter.Total)
	}
	if supplyAfter.Burned-supplyBefore.Burned != 5 {
		t.Fatalf("burn = %d, want 5", supplyAfter.Burned-supplyBefore.Burned)
	}

	// La racine du pool doit coïncider avec un recalcul indépendant (même padding
	// à 2^SpendDepth, même PoseidonCommit) — garantit qu'un wallet reconstruit la
	// même racine et que sa preuve future vérifiera.
	full := 1 << uint(stark.SpendDepth())
	leaves := make([][4]stark.Felt, full)
	d, err := func() ([4]stark.Felt, error) {
		var dd [4]stark.Felt
		for k := 0; k < 4; k++ {
			dd[k] = stark.FeltFromBytes(cm[k*8 : k*8+8])
		}
		return dd, nil
	}()
	if err != nil {
		t.Fatal(err)
	}
	leaves[0] = d
	wantRoot, _ := stark.PoseidonCommit(leaves)
	if string(pool.Root) != string(cmBytes(wantRoot)) {
		t.Fatal("racine du pool != racine recalculée indépendamment")
	}
}

// TestShieldRejectsMalformedCommitment : un engagement de mauvaise taille (≠ 32
// octets) est refusé À L'EXÉCUTION (avant toute mutation : atomicité).
func TestShieldRejectsMalformedCommitment(t *testing.T) {
	st := New()
	st.SetParams(privacyParams())
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000)

	shield := &types.Transaction{
		Type: types.TxShield, Amount: 100, MaxBaseFee: 1,
		ShieldCommitment: []byte{1, 2, 3}, // 3 octets : invalide
		ShieldNote:       []byte("blob"),
	}
	shield.SignWith(alice)
	if _, _, _, err := st.Execute([]*types.Transaction{shield}, nil, nil, "", 1_000, true); err == nil {
		t.Fatal("un commitment de mauvaise taille devrait être refusé")
	}
	// Atomicité : aucune mutation (pool inexistant, solde intact).
	if st.GetShieldedPool() != nil {
		t.Fatal("aucun pool ne doit être créé sur une tx refusée")
	}
	if st.GetAccount(alice.Address()).Balances[types.NativeToken] != 1_000 {
		t.Fatal("le solde ne doit pas bouger sur une tx refusée")
	}
}

// TestShieldRejectsOutOfRangeAmount : un dépôt >= 2^RangeBits produirait une note
// de valeur hors borne, INDÉPENSABLE par le circuit (range-proof insatisfaisable).
// L'état doit le refuser AVANT toute mutation (atomicité), pour ne pas verrouiller
// définitivement les fonds.
func TestShieldRejectsOutOfRangeAmount(t *testing.T) {
	st := New()
	st.SetParams(privacyParams())
	alice := mustKey(t)
	// Provision largement au-dessus de la borne pour isoler le motif du refus.
	st.Mint(alice.Address(), stark.MaxNoteValue()+1_000)

	shield := &types.Transaction{
		Type: types.TxShield, Amount: stark.MaxNoteValue(), MaxBaseFee: 1, // pile la borne (exclue)
		ShieldCommitment: sampleCommitment(9), ShieldNote: []byte("blob"),
	}
	shield.SignWith(alice)
	if _, _, _, err := st.Execute([]*types.Transaction{shield}, nil, nil, "", 1_000, true); err == nil {
		t.Fatal("un montant de shield hors borne de range devrait être refusé")
	}
	// Atomicité : pas de pool, solde intact.
	if st.GetShieldedPool() != nil {
		t.Fatal("aucun pool ne doit être créé sur une tx refusée")
	}
	if st.GetAccount(alice.Address()).Balances[types.NativeToken] != stark.MaxNoteValue()+1_000 {
		t.Fatal("le solde ne doit pas bouger sur une tx refusée")
	}

	// Juste sous la borne : accepté.
	ok := &types.Transaction{
		Type: types.TxShield, Amount: stark.MaxNoteValue() - 1, MaxBaseFee: 1,
		ShieldCommitment: sampleCommitment(10), ShieldNote: []byte("blob"),
	}
	ok.SignWith(alice)
	executeStateBlock(t, st, "", 1_000, ok)
	if p := st.GetShieldedPool(); p == nil || p.Balance != stark.MaxNoteValue()-1 {
		t.Fatal("un montant juste sous la borne doit être accepté")
	}
}

// TestShieldNonceAdvancesAndIsAtomic : un shield valide incrémente le nonce ;
// deux shields successifs s'enchaînent et insèrent deux notes distinctes.
func TestShieldTwoDepositsAccumulate(t *testing.T) {
	st := New()
	st.SetParams(privacyParams())
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000)

	s1 := &types.Transaction{Type: types.TxShield, Amount: 100, MaxBaseFee: 1, Nonce: 0,
		ShieldCommitment: sampleCommitment(1), ShieldNote: []byte("a")}
	s1.SignWith(alice)
	s2 := &types.Transaction{Type: types.TxShield, Amount: 50, MaxBaseFee: 1, Nonce: 1,
		ShieldCommitment: sampleCommitment(2), ShieldNote: []byte("b")}
	s2.SignWith(alice)
	executeStateBlock(t, st, "", 1_000, s1, s2)

	pool := st.GetShieldedPool()
	if pool.Balance != 150 {
		t.Fatalf("pool.Balance = %d, want 150", pool.Balance)
	}
	if len(pool.Commitments) != 2 {
		t.Fatalf("pool a %d engagements, want 2", len(pool.Commitments))
	}
	if st.NonceOf(alice.Address()) != 2 {
		t.Fatalf("nonce = %d, want 2", st.NonceOf(alice.Address()))
	}
}
