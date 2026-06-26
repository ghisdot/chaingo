package state

import (
	"testing"

	"chaingo/internal/shieldedwallet"
	"chaingo/internal/stark"
	"chaingo/internal/types"
)

const multiPayer = "cg0000000000000000000000000000000000000002"

// seedPoolWithNotes amorce un pool blindé avec les notes données (via des shields
// réels) et renvoie l'état (PrivacyEnabled). Le payeur est crédité largement.
func seedPoolWithNotes(t *testing.T, notes []shieldedwallet.Note) *State {
	t.Helper()
	st := New()
	st.SetParams(privacyParams())
	var total uint64
	for _, n := range notes {
		total += n.Value
	}
	st.Mint(multiPayer, total+1_000_000) // marge pour les frais réseau
	for i, n := range notes {
		// Nonce explicite : plusieurs shields successifs depuis le MÊME payeur.
		tx := &types.Transaction{
			Type: types.TxShield, From: multiPayer, Nonce: uint64(i), Amount: n.Value, MaxBaseFee: 1,
			ShieldCommitment: n.CommitmentBytes(), ShieldNote: []byte("seed"),
		}
		if _, _, _, err := st.Execute([]*types.Transaction{tx}, nil, nil, "", 1_000, true); err != nil {
			t.Fatalf("seed shield #%d: %v", i, err)
		}
	}
	return st
}

// TestShieldedTransfer_MultiInput : une dépense 2-entrées / 1-sortie RÉELLE passe le
// consensus — les DEUX nullifiers sont marqués et la note de sortie insérée.
func TestShieldedTransfer_MultiInput(t *testing.T) {
	in0 := shieldedwallet.Note{Value: 600_000, Nk: shieldedwallet.DeriveNk([]byte("multi/0"), 0), Rho: shieldedwallet.DeriveRho([]byte("multi/0"), 0)}
	in1 := shieldedwallet.Note{Value: 400_000, Nk: shieldedwallet.DeriveNk([]byte("multi/1"), 0), Rho: shieldedwallet.DeriveRho([]byte("multi/1"), 0)}
	fee := uint64(2_500)
	out := shieldedwallet.Note{Value: 600_000 + 400_000 - fee, Nk: shieldedwallet.DeriveNk([]byte("multi/out"), 0), Rho: shieldedwallet.DeriveRho([]byte("multi/out"), 0)}

	st := seedPoolWithNotes(t, []shieldedwallet.Note{in0, in1})
	pool := st.GetShieldedPool()

	w, feeFelt, err := shieldedwallet.BuildWitnessMulti(pool.Commitments, shieldedwallet.SpendPlanN{
		Ins: []shieldedwallet.Note{in0, in1}, Outs: []shieldedwallet.Note{out}, Fee: fee,
	})
	if err != nil {
		t.Fatalf("BuildWitnessMulti: %v", err)
	}
	public, proof := stark.ProveSpendN(w, feeFelt)

	tx := &types.Transaction{
		Type: types.TxShieldedTransfer, From: multiPayer, Nonce: 2, MaxBaseFee: 1,
		SpendProof: stark.MarshalSpendProof(proof), SpendPublic: stark.MarshalSpendNPublic(public),
		ShieldNote: []byte("multi-out"),
	}
	if _, _, _, err := st.Execute([]*types.Transaction{tx}, nil, nil, "", 5_000, true); err != nil {
		t.Fatalf("transfert 2-in/1-out rejeté: %v", err)
	}

	pool = st.GetShieldedPool()
	if len(public.Nfs) != 2 {
		t.Fatalf("attendu 2 nullifiers, obtenu %d", len(public.Nfs))
	}
	for i, nf := range public.Nfs {
		if !pool.Nullifiers[nullifierKey(nf)] {
			t.Fatalf("nullifier d'entrée %d non marqué après le transfert", i)
		}
	}
	// 2 notes shieldées + 1 note de sortie = 3 engagements.
	if len(pool.Commitments) != 3 {
		t.Fatalf("attendu 3 engagements après le transfert, obtenu %d", len(pool.Commitments))
	}
}

// TestShieldedTransfer_DoublonEntreeRejete : dépenser DEUX FOIS la même note en
// entrée produit des nullifiers IDENTIQUES — le circuit l'accepte (il l'ignore),
// mais la couche état DOIT rejeter (sinon création de valeur). Invariant porté par
// l'état, pas par le circuit.
func TestShieldedTransfer_DoublonEntreeRejete(t *testing.T) {
	in := shieldedwallet.Note{Value: 500_000, Nk: shieldedwallet.DeriveNk([]byte("dup/in"), 0), Rho: shieldedwallet.DeriveRho([]byte("dup/in"), 0)}
	fee := uint64(2_500)
	out := shieldedwallet.Note{Value: 2*500_000 - fee, Nk: shieldedwallet.DeriveNk([]byte("dup/out"), 0), Rho: shieldedwallet.DeriveRho([]byte("dup/out"), 0)}

	st := seedPoolWithNotes(t, []shieldedwallet.Note{in}) // UNE seule note dans le pool
	pool := st.GetShieldedPool()

	w, feeFelt, err := shieldedwallet.BuildWitnessMulti(pool.Commitments, shieldedwallet.SpendPlanN{
		Ins: []shieldedwallet.Note{in, in}, Outs: []shieldedwallet.Note{out}, Fee: fee,
	})
	if err != nil {
		t.Fatalf("BuildWitnessMulti: %v", err)
	}
	public, proof := stark.ProveSpendN(w, feeFelt)
	// Le circuit accepte (il ne sait pas que c'est la même note)…
	if !stark.VerifySpendN(public, proof) {
		t.Fatal("la preuve devrait être valide au niveau circuit")
	}
	if public.Nfs[0] != public.Nfs[1] {
		t.Fatal("attendu deux nullifiers identiques pour une note dupliquée")
	}
	// …mais l'état DOIT rejeter (double-dépense intra-tx).
	tx := &types.Transaction{
		Type: types.TxShieldedTransfer, From: multiPayer, Nonce: 1, MaxBaseFee: 1,
		SpendProof: stark.MarshalSpendProof(proof), SpendPublic: stark.MarshalSpendNPublic(public),
		ShieldNote: []byte("dup"),
	}
	if _, _, _, err := st.Execute([]*types.Transaction{tx}, nil, nil, "", 5_000, true); err == nil {
		t.Fatal("SOUNDNESS : doublon d'entrée (nullifiers identiques) accepté par l'état")
	}
}
