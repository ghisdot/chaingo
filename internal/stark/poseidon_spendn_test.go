package stark

import "testing"

// snBuildScenario construit un scénario HONNÊTE M-entrées / N-sorties : chaque
// entrée i est la feuille d'indice i d'un même arbre spendDepth, et
// Σ inValue_i = Σ outValue_j + fee.
func snBuildScenario(seed string, numIn, numOut int) (SpendNWitness, Felt) {
	fee := FromUint64(2_500)

	// Feuilles de l'arbre : inCm_i en position i, remplissage ailleurs.
	numLeaves := 1 << spendDepth
	leaves := make([][poseidonDigestLen]Felt, numLeaves)
	for i := 0; i < numLeaves; i++ {
		leaves[i] = spTestDigest(seed + "/leaf/" + itoaU64(uint64(i)))
	}

	ins := make([]SpendNIn, numIn)
	totalIn := Zero()
	for i := 0; i < numIn; i++ {
		nk := spTestDigest(seed + "/nk/" + itoaU64(uint64(i)))
		rho := spTestDigest(seed + "/rho/" + itoaU64(uint64(i)))
		val := FromUint64(1_000_000 + uint64(i)*1000)
		inCm := SpendCommit(val, SpendOwnerTag(nk), rho)
		leaves[i] = inCm
		ins[i] = SpendNIn{Value: val, Rho: rho, Nk: nk}
		totalIn = totalIn.Add(val)
	}
	_, tree := PoseidonCommit(leaves)
	for i := 0; i < numIn; i++ {
		ins[i].Path = spPathFromIndex(tree, i)
	}

	// Sorties : distribue (totalIn - fee) sur numOut (la dernière absorbe le reste).
	rest := totalIn.Sub(fee)
	outs := make([]SpendNOut, numOut)
	per := FromUint64(7_000)
	acc := Zero()
	for j := 0; j < numOut-1; j++ {
		outs[j] = SpendNOut{Value: per, OwnerTag: spTestDigest(seed + "/oo/" + itoaU64(uint64(j))), Rho: spTestDigest(seed + "/or/" + itoaU64(uint64(j)))}
		acc = acc.Add(per)
	}
	outs[numOut-1] = SpendNOut{Value: rest.Sub(acc), OwnerTag: spTestDigest(seed + "/oo/last"), Rho: spTestDigest(seed + "/or/last")}

	return SpendNWitness{Ins: ins, Outs: outs}, fee
}

// snTraceSatisfies vérifie NATIVEMENT (sans STARK) que la trace annule transitions
// et bords — rapide, isole l'arithmétisation.
func snTraceSatisfies(t *testing.T, air spendNAIR, trace [][]Felt) {
	t.Helper()
	n := air.NumSteps()
	if len(trace) != n {
		t.Fatalf("trace hauteur %d, attendu %d", len(trace), n)
	}
	for i := 0; i < n-1; i++ {
		for k, r := range air.EvalTransition(trace[i], trace[i+1]) {
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

// Trace honnête satisfaite pour divers (numIn, numOut), dont cas complétés en pow2.
func TestSpendN_TraceHonnete(t *testing.T) {
	cases := [][2]int{{1, 1}, {1, 2}, {2, 1}, {2, 2}, {3, 2}, {1, 5}, {2, 3}}
	for _, c := range cases {
		numIn, numOut := c[0], c[1]
		w, fee := snBuildScenario("trace", numIn, numOut)
		trace, public := buildSpendNTrace(w, fee)
		air := spendNAirOf(public)
		if len(public.Nfs) != numIn || len(public.OutCms) != numOut {
			t.Fatalf("(%d,%d): nfs=%d outcms=%d", numIn, numOut, len(public.Nfs), len(public.OutCms))
		}
		snTraceSatisfies(t, air, trace)
	}
}

// Preuve STARK honnête M-in/N-out.
func TestSpendN_PreuveHonnete(t *testing.T) {
	cases := [][2]int{{1, 1}, {1, 2}, {2, 1}, {2, 2}}
	for _, c := range cases {
		w, fee := snBuildScenario("preuve", c[0], c[1])
		public, proof := ProveSpendN(w, fee)
		if !VerifySpendN(public, proof) {
			t.Fatalf("(%d,%d): preuve honnête rejetée", c[0], c[1])
		}
	}
}

// SOUNDNESS : non-conservation (Σ in != Σ out + fee) => trace non satisfaisante et
// preuve rejetée.
func TestSpendN_NonConservationRejetee(t *testing.T) {
	w, fee := snBuildScenario("cons", 2, 2)
	w.Outs[0].Value = w.Outs[0].Value.Add(One()) // casse l'équilibre
	trace, public := buildSpendNTrace(w, fee)
	air := spendNAirOf(public)

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
	_, proof := ProveSpendN(w, fee)
	if VerifySpendN(public, proof) {
		t.Fatal("SOUNDNESS : preuve non conservatrice acceptée")
	}
}

// Un nullifier public falsifié est rejeté.
func TestSpendN_NfFalsifie(t *testing.T) {
	w, fee := snBuildScenario("nf", 2, 2)
	public, proof := ProveSpendN(w, fee)
	bad := clonePublic(public)
	bad.Nfs[1][0] = bad.Nfs[1][0].Add(One())
	if VerifySpendN(bad, proof) {
		t.Fatal("SOUNDNESS : nullifier falsifié accepté")
	}
}

// Un outCm public falsifié est rejeté.
func TestSpendN_OutCmFalsifie(t *testing.T) {
	w, fee := snBuildScenario("outcm", 2, 3)
	public, proof := ProveSpendN(w, fee)
	bad := clonePublic(public)
	bad.OutCms[1][0] = bad.OutCms[1][0].Add(One())
	if VerifySpendN(bad, proof) {
		t.Fatal("SOUNDNESS : outCm falsifié accepté")
	}
}

// Déterminisme : deux preuves du même témoin partagent les mêmes engagements.
func TestSpendN_Deterministe(t *testing.T) {
	w, fee := snBuildScenario("det", 2, 2)
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

func clonePublic(p SpendNPublic) SpendNPublic {
	out := SpendNPublic{MerkleRoot: p.MerkleRoot, Fee: p.Fee}
	out.Nfs = make([][poseidonDigestLen]Felt, len(p.Nfs))
	copy(out.Nfs, p.Nfs)
	out.OutCms = make([][poseidonDigestLen]Felt, len(p.OutCms))
	copy(out.OutCms, p.OutCms)
	return out
}
