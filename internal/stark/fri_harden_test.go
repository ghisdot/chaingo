package stark

import "testing"

// friLowDegree construit des évaluations LDE d'un polynôme de bas degré honnête.
func friLowDegree(deg, n int, seed uint64) []Felt {
	rng := newPRNG(seed)
	coeffs := make([]Felt, deg+1)
	for i := range coeffs {
		coeffs[i] = rng.felt()
	}
	return evalOnLDE(coeffs, n)
}

// Une preuve FRI avec grinding vérifie, et un nonce de PoW falsifié est rejeté.
func TestFriGrinding_NonceFalsifieRejete(t *testing.T) {
	params := FriParams{Blowup: 8, NumQueries: 24, GrindBits: 12}
	evals := friLowDegree(20, 256, 99)

	proof := Prove(evals, params)
	if !Verify(proof, params) {
		t.Fatal("preuve FRI honnête avec grinding rejetée")
	}
	// Le nonce de PoW est public : on le falsifie, la vérification DOIT échouer
	// (soit la PoW ne tient plus, soit le transcript diverge).
	bad := proof
	bad.PowNonce = proof.PowNonce + 1
	if Verify(bad, params) {
		t.Fatal("SOUNDNESS : nonce de grinding falsifié accepté")
	}
}

// Sans grinding (GrindBits=0) le nonce vaut 0 et la preuve vérifie quand même
// (rétrocompatibilité du champ par défaut).
func TestFriGrinding_DesactiveParDefaut(t *testing.T) {
	params := FriParams{Blowup: 8, NumQueries: 24} // GrindBits=0
	evals := friLowDegree(20, 256, 7)
	proof := Prove(evals, params)
	if proof.PowNonce != 0 {
		t.Fatalf("grinding off: PowNonce devrait être 0, obtenu %d", proof.PowNonce)
	}
	if !Verify(proof, params) {
		t.Fatal("preuve sans grinding rejetée")
	}
}

// Les positions d'interrogation FRI sont DEUX À DEUX DISTINCTES (sans remise) dès
// que le domaine le permet (count <= firstHalf).
func TestFriQueryPositions_Distinctes(t *testing.T) {
	params := FriParams{Blowup: 8, NumQueries: 32, GrindBits: 0}
	n := 1024 // firstHalf = 512 >= 32
	evals := friLowDegree(40, n, 1234)
	proof := Prove(evals, params)

	// On rejoue le tirage des positions exactement comme le vérifieur.
	tr := NewTranscript(friDomain)
	absorbParams(tr, params, proof.LogDomain)
	for c := 0; c < len(proof.LayerRoots); c++ {
		tr.Absorb("fri/layer-root", proof.LayerRoots[c][:])
		tr.Challenge("fri/fold")
	}
	absorbFinal(tr, proof.FinalCoeffs)
	positions := queryPositions(tr, "fri/query", params.NumQueries, n/2)

	seen := map[int]bool{}
	for _, p := range positions {
		if seen[p] {
			t.Fatalf("position FRI répétée (remise) : %d", p)
		}
		seen[p] = true
	}
	if len(positions) != params.NumQueries {
		t.Fatalf("attendu %d positions, obtenu %d", params.NumQueries, len(positions))
	}
}
