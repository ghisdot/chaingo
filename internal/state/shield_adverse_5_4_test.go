package state

import (
	"encoding/json"
	"testing"

	"chaingo/internal/crypto"
	"chaingo/internal/stark"
	"chaingo/internal/types"
)

// ============================================================================
// ÉTAGE 5.4 — VÉRIFICATION ADVERSE de l'intégration blindée on-chain.
//
// Tests SANS génération de preuve (rapides) : ils renforcent les deux invariants
// de sûreté exigés et attaquent les chemins de conservation/atomicité que le test
// d'intégration (qui consomme l'unique preuve) ne couvre pas explicitement.
// ============================================================================

// stateRootShapeNoShieldField réplique EXACTEMENT le schéma de rootLocked() MAIS
// sans le champ Shielded — c'est le JSON qu'aurait produit le binaire d'AVANT
// l'ajout du pool blindé. Sert de témoin "octet-pour-octet" pour l'invariant (b).
func stateRootShapeNoShieldField(s *State) string {
	b, _ := json.Marshal(struct {
		Accounts      map[string]*Account      `json:"accounts"`
		Tokens        map[string]*Token        `json:"tokens"`
		Validators    map[string]*Validator    `json:"validators"`
		Contracts     map[string]*Contract     `json:"contracts"`
		WasmContracts map[string]*WasmContract `json:"wasm_contracts,omitempty"`
		// PAS de champ Shielded ici : c'est tout l'objet du test.
		Slashed   map[string]bool `json:"slashed"`
		Unbonding []*Unbonding    `json:"unbonding"`
		Supply    Supply          `json:"supply"`
		Params    types.Params    `json:"params"`
		BaseFee   uint64          `json:"base_fee"`
	}{s.Accounts, s.Tokens, s.Validators, s.Contracts, s.WasmContracts,
		s.Slashed, s.Unbonding, s.Supply, s.Params, s.BaseFee})
	return crypto.HashHex(b)
}

// TestRootOctetIdenticalToPreShieldBinary : invariant (b), forme la PLUS FORTE.
// Quand le pool n'a jamais servi (Shielded == nil), la racine produite par
// rootLocked() (qui INCLUT le champ `shielded,omitempty`) est OCTET-POUR-OCTET
// celle qu'aurait produite un binaire dépourvu du champ. Donc une chaîne existante
// (pool jamais touché) ne forke PAS à l'upgrade, indépendamment des Params.
//
// NB : on ne touche PAS à PrivacyEnabled/ShieldFee ici car ces champs de Params
// font partie de la genèse SIGNÉE de chaque réseau (comme WasmEnabled avant eux) ;
// l'invariant de non-fork porte sur le POOL, pas sur les Params. On teste donc avec
// des Params réalistes (DefaultParams, privacy off) ET avec privacy on : dans les
// deux cas, tant que le pool est nil, la racine == la forme sans-champ.
func TestRootOctetIdenticalToPreShieldBinary(t *testing.T) {
	for _, on := range []bool{false, true} {
		st := New()
		p := types.DefaultParams()
		p.PrivacyEnabled = on
		st.SetParams(p)
		st.Mint("cg982fbc54bcbe39cc078d1ed519a0cf228309f44a", 12_345)
		st.Mint("cg0000000000000000000000000000000000000abc", 67_890)

		if st.GetShieldedPool() != nil {
			t.Fatalf("on=%v: le pool ne doit pas exister", on)
		}
		got := st.Root()
		want := stateRootShapeNoShieldField(st)
		if got != want {
			t.Fatalf("on=%v: racine (%s) != racine sans champ shielded (%s) — une chaîne existante forkerait",
				on, got, want)
		}
	}
}

// TestStaleRootRejected : une preuve dont la MerkleRoot ne correspond PAS à la
// racine COURANTE du pool est rejetée (anti racine périmée). On n'a pas besoin
// d'une vraie preuve : verifySpendLocked vérifie la racine AVANT VerifySpend ?
// Non — il vérifie VerifySpend d'abord. On exerce donc le chemin via un pool dont
// la racine a changé sous une preuve sérialisée bidon : la borne de décodage /
// VerifySpend rejette avant la racine. Pour ISOLER le contrôle de racine, on teste
// directement bytesEqual + l'ordre des contrôles par lecture de code n'est pas
// suffisant ; on couvre donc la racine périmée dans le test d'intégration (preuve
// réelle rejouée sur un pool muté). Ici on se limite à un contrôle unitaire de la
// comparaison de racine pour garantir qu'une racine différente N'est JAMAIS jugée
// égale.
func TestPoolRootComparisonIsStrict(t *testing.T) {
	a := sampleCommitment(1)
	b := sampleCommitment(2)
	if bytesEqual(a, b) {
		t.Fatal("deux racines distinctes ne doivent jamais être jugées égales")
	}
	if !bytesEqual(a, append([]byte(nil), a...)) {
		t.Fatal("une racine doit être égale à sa copie")
	}
	// nil vs slice vide : considérés égaux (cohérent avec un pool jamais touché).
	if !bytesEqual(nil, []byte{}) {
		t.Fatal("nil et slice vide doivent être égaux")
	}
}

// TestShieldedTransferRejectedWhenPoolEmpty : un shielded_transfer/unshield sur un
// pool VIDE (aucune note) est rejeté AVANT tout décodage de preuve (il n'y a rien à
// dépenser). Atomicité : aucune mutation. Pas de preuve requise.
func TestShieldedTransferRejectedWhenPoolEmpty(t *testing.T) {
	st := New()
	st.SetParams(privacyParams())
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000)

	for _, typ := range []types.TxType{types.TxShieldedTransfer, types.TxUnshield} {
		tx := &types.Transaction{
			Type: typ, From: alice.Address(), To: alice.Address(), MaxBaseFee: 1,
			SpendProof: []byte{1, 2, 3}, SpendPublic: []byte{4, 5, 6}, ShieldNote: []byte("n"),
		}
		if _, _, _, err := st.Execute([]*types.Transaction{tx}, nil, nil, "", 1_000, true); err == nil {
			t.Fatalf("%s sur pool vide aurait dû être rejeté", typ)
		}
	}
	if st.GetShieldedPool() != nil {
		t.Fatal("aucun pool ne doit naître d'une dépense sur pool vide")
	}
	if st.GetAccount(alice.Address()).Balances[types.NativeToken] != 1_000 {
		t.Fatal("solde muté par une dépense rejetée (atomicité cassée)")
	}
}

// TestShieldedTransferGarbageProofRejectedAtomically : un shielded_transfer dont la
// preuve est un blob ALÉATOIRE (non décodable) est rejeté à l'exécution, et ne mute
// RIEN (pool inchangé, solde inchangé, nullifier non posé). Le pool doit déjà
// contenir une note (sinon on tombe sur "pool vide" avant le décodage). On amorce
// donc le pool par un shield public (sans preuve), puis on tire la dépense bidon.
func TestShieldedTransferGarbageProofRejectedAtomically(t *testing.T) {
	st := New()
	st.SetParams(privacyParams())
	alice := mustKey(t)
	st.Mint(alice.Address(), 10_000)

	// Amorçage : une note réelle déposée (shield public, pas de preuve nécessaire).
	shield := &types.Transaction{
		Type: types.TxShield, From: alice.Address(), Amount: 1_000, MaxBaseFee: 1, Nonce: 0,
		ShieldCommitment: sampleCommitment(42), ShieldNote: []byte("seed"),
	}
	shield.SignWith(alice)
	executeStateBlock(t, st, "", 1_000, shield)

	poolBefore := st.GetShieldedPool()
	supplyBefore := st.GetSupply()
	balBefore := st.GetAccount(alice.Address()).Balances[types.NativeToken]

	// Dépense bidon : preuve/énoncé non décodables -> rejet au décodage/VerifySpend.
	bad := &types.Transaction{
		Type: types.TxShieldedTransfer, From: alice.Address(), MaxBaseFee: 1, Nonce: 1,
		SpendProof:  []byte{0xde, 0xad, 0xbe, 0xef},
		SpendPublic: []byte{0x01, 0x02},
		ShieldNote:  []byte("out"),
	}
	bad.SignWith(alice)
	if _, _, _, err := st.Execute([]*types.Transaction{bad}, nil, nil, "", 2_000, true); err == nil {
		t.Fatal("une preuve bidon aurait dû être rejetée")
	}

	// Atomicité : pool et soldes STRICTEMENT inchangés, nonce non avancé.
	poolAfter := st.GetShieldedPool()
	if len(poolAfter.Commitments) != len(poolBefore.Commitments) {
		t.Fatalf("commitments mutés par une dépense rejetée : %d -> %d",
			len(poolBefore.Commitments), len(poolAfter.Commitments))
	}
	if string(poolAfter.Root) != string(poolBefore.Root) {
		t.Fatal("racine mutée par une dépense rejetée")
	}
	if poolAfter.Balance != poolBefore.Balance {
		t.Fatalf("pool.Balance muté : %d -> %d", poolBefore.Balance, poolAfter.Balance)
	}
	if len(poolAfter.Nullifiers) != 0 {
		t.Fatal("un nullifier a été posé par une dépense rejetée")
	}
	if st.GetAccount(alice.Address()).Balances[types.NativeToken] != balBefore {
		t.Fatal("solde muté par une dépense rejetée")
	}
	if st.GetSupply().Total != supplyBefore.Total {
		t.Fatal("supply mutée par une dépense rejetée")
	}
	if st.NonceOf(alice.Address()) != 1 {
		t.Fatalf("nonce avancé par une dépense rejetée : %d", st.NonceOf(alice.Address()))
	}
}

// TestShieldPoolBalanceConservation : sur un cycle de shields publics, pool.Balance
// == somme des CGO déposés (conservation : la valeur n'apparaît ni ne disparaît).
// Le pool ne peut pas devenir négatif (uint64 + on ne soustrait jamais plus que le
// solde, garanti par les contrôles `pool.Balance < fee/amount`).
func TestShieldPoolBalanceConservation(t *testing.T) {
	st := New()
	st.SetParams(privacyParams())
	alice := mustKey(t)
	st.Mint(alice.Address(), 1_000_000)

	var deposited uint64
	for i := uint64(0); i < 5; i++ {
		amt := 100 + i*7
		tx := &types.Transaction{
			Type: types.TxShield, From: alice.Address(), Amount: amt, MaxBaseFee: 1, Nonce: i,
			ShieldCommitment: sampleCommitment(i), ShieldNote: []byte("n"),
		}
		tx.SignWith(alice)
		executeStateBlock(t, st, "", int64(1_000+i), tx)
		deposited += amt
	}
	pool := st.GetShieldedPool()
	if pool.Balance != deposited {
		t.Fatalf("pool.Balance = %d, want %d (conservation des dépôts)", pool.Balance, deposited)
	}
	if uint64(len(pool.Commitments)) != 5 {
		t.Fatalf("commitments = %d, want 5", len(pool.Commitments))
	}
}

// TestShieldPoolCapacityEnforced : on ne peut pas insérer plus de 2^SpendDepth
// notes (sinon la racine ne serait plus dépensable par le circuit). La (capacité+1)-
// ième est rejetée, sans mutation partielle.
func TestShieldPoolCapacityEnforced(t *testing.T) {
	st := New()
	st.SetParams(privacyParams())
	alice := mustKey(t)
	st.Mint(alice.Address(), 100_000_000)

	capacity := 1 << uint(stark.SpendDepth())
	// Remplir EXACTEMENT à capacité.
	for i := 0; i < capacity; i++ {
		tx := &types.Transaction{
			Type: types.TxShield, From: alice.Address(), Amount: 1, MaxBaseFee: 1, Nonce: uint64(i),
			ShieldCommitment: sampleCommitment(uint64(i)), ShieldNote: []byte("n"),
		}
		tx.SignWith(alice)
		executeStateBlock(t, st, "", int64(1_000+i), tx)
	}
	pool := st.GetShieldedPool()
	if len(pool.Commitments) != capacity {
		t.Fatalf("pool rempli à %d, attendu %d", len(pool.Commitments), capacity)
	}
	balBefore := st.GetAccount(alice.Address()).Balances[types.NativeToken]

	// La note de trop est refusée.
	over := &types.Transaction{
		Type: types.TxShield, From: alice.Address(), Amount: 1, MaxBaseFee: 1, Nonce: uint64(capacity),
		ShieldCommitment: sampleCommitment(uint64(capacity)), ShieldNote: []byte("n"),
	}
	over.SignWith(alice)
	if _, _, _, err := st.Execute([]*types.Transaction{over}, nil, nil, "", 9_999, true); err == nil {
		t.Fatal("une note au-delà de la capacité aurait dû être refusée")
	}
	// Atomicité : ni note insérée, ni solde débité.
	if len(st.GetShieldedPool().Commitments) != capacity {
		t.Fatal("une note a été insérée au-delà de la capacité")
	}
	if st.GetAccount(alice.Address()).Balances[types.NativeToken] != balBefore {
		t.Fatal("solde débité par une note refusée (atomicité)")
	}
}
