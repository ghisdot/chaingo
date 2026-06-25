package stark

import "testing"

// snBuildScenario construit un scénario HONNÊTE 1-entrée / numOut-sorties :
// inValue = Σ outValue_j + fee, l'entrée est une feuille d'un arbre spendDepth.
func snBuildScenario(seed string, index, numOut int) (SpendNWitness, Felt) {
	nk := spTestDigest(seed + "/nk")
	inRho := spTestDigest(seed + "/inRho")
	inValue := FromUint64(1_000_000)
	fee := FromUint64(2_500)

	// Répartit (inValue - fee) sur numOut sorties (la dernière absorbe le reste).
	rest := inValue.Sub(fee)
	outs := make([]SpendNOut, numOut)
	per := FromUint64(uint64(7_000))
	acc := Zero()
	for j := 0; j < numOut-1; j++ {
		outs[j] = SpendNOut{Value: per, OwnerTag: spTestDigest(seed + "/out/" + itoaU64(uint64(j))), Rho: spTestDigest(seed + "/rho/" + itoaU64(uint64(j)))}
		acc = acc.Add(per)
	}
	last := rest.Sub(acc)
	outs[numOut-1] = SpendNOut{Value: last, OwnerTag: spTestDigest(seed + "/out/last"), Rho: spTestDigest(seed + "/rho/last")}

	ownerTag := SpendOwnerTag(nk)
	inCm := SpendCommit(inValue, ownerTag, inRho)
	numLeaves := 1 << spendDepth
	leaves := make([][poseidonDigestLen]Felt, numLeaves)
	for i := 0; i < numLeaves; i++ {
		leaves[i] = spTestDigest(seed + "/leaf/" + itoaU64(uint64(i)))
	}
	leaves[index] = inCm
	_, tree := PoseidonCommit(leaves)
	path := spPathFromIndex(tree, index)

	return SpendNWitness{InValue: inValue, InRho: inRho, Nk: nk, Path: path, Outs: outs}, fee
}

// snTraceSatisfies vérifie NATIVEMENT (sans STARK) que la trace annule toutes les
// contraintes de transition et de bord — rapide, isole l'arithmétisation.
func snTraceSatisfies(t *testing.T, air spendNAIR, trace [][]Felt) {
	t.Helper()
	n := air.NumSteps()
	if len(trace) != n {
		t.Fatalf("trace hauteur %d, attendu %d", len(trace), n)
	}
	for i := 0; i < n-1; i++ {
		res := air.EvalTransition(trace[i], trace[i+1])
		for k, r := range res {
			if !r.IsZero() {
				t.Fatalf("transition non nulle: ligne %d, résidu %d = %v", i, k, r)
			}
		}
	}
	for _, bc := range air.Boundaries() {
		if !trace[bc.Row][bc.Col].Equal(bc.Value) {
			t.Fatalf("bord violé: (row=%d,col=%d)=%v attendu %v", bc.Row, bc.Col, trace[bc.Row][bc.Col], bc.Value)
		}
	}
}

// La trace honnête satisfait l'AIR pour plusieurs numOut (dont des cas avec
// complétion en puissance de 2 : numOut=2,3,5).
func TestSpendN_TraceHonnete(t *testing.T) {
	for _, numOut := range []int{1, 2, 3, 5} {
		w, fee := snBuildScenario("trace", 5, numOut)
		trace, public := buildSpendNTrace(w, fee)
		air := spendNAirOf(public)
		if len(public.OutCms) != numOut {
			t.Fatalf("numOut=%d: %d outCms", numOut, len(public.OutCms))
		}
		snTraceSatisfies(t, air, trace)
	}
}

// Preuve STARK honnête : ProveSpendN -> VerifySpendN accepte (numOut variés).
func TestSpendN_PreuveHonnete(t *testing.T) {
	for _, numOut := range []int{1, 2, 4} {
		w, fee := snBuildScenario("preuve", 3, numOut)
		public, proof := ProveSpendN(w, fee)
		if !VerifySpendN(public, proof) {
			t.Fatalf("numOut=%d: preuve honnête rejetée", numOut)
		}
	}
}

// SOUNDNESS : non-conservation (Σ outValue + fee != inValue) => la trace ne
// satisfait PAS l'AIR (résidu de conservation non nul).
func TestSpendN_NonConservationRejetee(t *testing.T) {
	w, fee := snBuildScenario("cons", 7, 3)
	// On gonfle une sortie : la somme dépasse inValue - fee.
	w.Outs[0].Value = w.Outs[0].Value.Add(One())
	trace, public := buildSpendNTrace(w, fee)
	air := spendNAirOf(public)

	// La contrainte de conservation DOIT être violée quelque part.
	violated := false
	for i := 0; i < air.NumSteps()-1; i++ {
		for _, r := range air.EvalTransition(trace[i], trace[i+1]) {
			if !r.IsZero() {
				violated = true
			}
		}
	}
	if !violated {
		t.Fatal("SOUNDNESS : non-conservation non détectée par l'AIR")
	}
	// La preuve issue de ce témoin non conservateur ne doit PAS vérifier, même
	// contre son PROPRE énoncé reconstruit : la trace viole la conservation, donc
	// la composition n'est pas de bas degré et VerifyAIR rejette.
	_, proof := ProveSpendN(w, fee)
	if VerifySpendN(public, proof) {
		t.Fatal("SOUNDNESS : preuve non conservatrice acceptée")
	}
}

// Un outCm public falsifié est rejeté.
func TestSpendN_OutCmFalsifie(t *testing.T) {
	w, fee := snBuildScenario("outcm", 2, 3)
	public, proof := ProveSpendN(w, fee)
	bad := SpendNPublic{MerkleRoot: public.MerkleRoot, Nf: public.Nf, Fee: public.Fee}
	bad.OutCms = make([][poseidonDigestLen]Felt, len(public.OutCms))
	copy(bad.OutCms, public.OutCms)
	bad.OutCms[1][0] = bad.OutCms[1][0].Add(One())
	if VerifySpendN(bad, proof) {
		t.Fatal("SOUNDNESS : outCm falsifié accepté")
	}
}

// Déterminisme : deux preuves du même témoin partagent les mêmes engagements.
func TestSpendN_Deterministe(t *testing.T) {
	w, fee := snBuildScenario("det", 1, 2)
	_, pr1 := ProveSpendN(w, fee)
	_, pr2 := ProveSpendN(w, fee)
	if pr1.CompRoot != pr2.CompRoot {
		t.Fatal("CompRoot non déterministe")
	}
	if len(pr1.ColRoots) != len(pr2.ColRoots) {
		t.Fatal("nombre de colonnes non déterministe")
	}
	for i := range pr1.ColRoots {
		if pr1.ColRoots[i] != pr2.ColRoots[i] {
			t.Fatalf("ColRoot[%d] non déterministe", i)
		}
	}
	if !pr1.OodHz.Equal(pr2.OodHz) {
		t.Fatal("OodHz non déterministe")
	}
}
