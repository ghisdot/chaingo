package stark

import "testing"

// snTraceViolated indique si la trace viole AU MOINS une contrainte de transition
// (rapide, sans STARK).
func snTraceViolated(air spendNAIR, trace [][]Felt) bool {
	for i := 0; i < air.NumSteps()-1; i++ {
		for _, r := range air.EvalTransition(trace[i], trace[i+1]) {
			if !r.IsZero() {
				return true
			}
		}
	}
	// Bords.
	for _, bc := range air.Boundaries() {
		if !trace[bc.Row][bc.Col].Equal(bc.Value) {
			return true
		}
	}
	return false
}

// SOUNDNESS : toutes les entrées doivent appartenir au MÊME arbre. Si une entrée
// a un chemin menant à une autre racine, la contrainte de bord
// (racine_i == merkleRoot) est violée → trace insatisfaisante, preuve rejetée.
func TestSpendN_EntreeHorsArbre(t *testing.T) {
	w, fee := snBuildScenario("hors-arbre", 2, 1)
	// Corrompt le chemin de la 2e entrée : sa racine d'appartenance diffère.
	w.Ins[1].Path.Siblings[0][0] = w.Ins[1].Path.Siblings[0][0].Add(One())

	trace, public := buildSpendNTrace(w, fee)
	air := spendNAirOf(public)
	if !snTraceViolated(air, trace) {
		t.Fatal("SOUNDNESS : entrée hors-arbre non détectée par l'AIR")
	}
	_, proof := ProveSpendN(w, fee)
	if VerifySpendN(public, proof) {
		t.Fatal("SOUNDNESS : preuve avec entrée hors-arbre acceptée")
	}
}

// SOUNDNESS : voler la note d'autrui sans sa clé. On dépense une note présente
// dans l'arbre mais avec un mauvais `nk` pour l'une des entrées : l'engagement
// recalculé (via ownerTag=Hash(nk)) ne coïncide plus avec la feuille, donc sa
// racine d'appartenance diverge → rejet.
func TestSpendN_VolNkFaux(t *testing.T) {
	w, fee := snBuildScenario("vol", 2, 2)
	// On garde le chemin (vers la feuille de la vraie note) mais on change nk :
	// l'inCm recalculé ne sera plus celui de la feuille → racine != merkleRoot.
	w.Ins[0].Nk[0] = w.Ins[0].Nk[0].Add(One())

	trace, public := buildSpendNTrace(w, fee)
	air := spendNAirOf(public)
	// L'énoncé public est reconstruit pour CE témoin ; mais la note (feuille) de
	// l'arbre n'a pas changé, donc l'appartenance casse.
	if !snTraceViolated(air, trace) {
		// Si la trace est cohérente avec elle-même, alors au moins la racine
		// reconstruite differe de la vraie : on le vérifie ci-dessous.
		t.Log("trace auto-cohérente — la divergence est dans la racine publique")
	}
	// Recoupement : la preuve ne doit pas vérifier contre la VRAIE racine de l'arbre.
	wGood, _ := snBuildScenario("vol", 2, 2)
	trueRoot := spChainRoot(
		SpendCommit(wGood.Ins[0].Value, SpendOwnerTag(wGood.Ins[0].Nk), wGood.Ins[0].Rho),
		wGood.Ins[0].Path,
	)
	_, proof := ProveSpendN(w, fee)
	forged := clonePublic(public)
	forged.MerkleRoot = trueRoot // exiger l'appartenance au VRAI arbre
	if VerifySpendN(forged, proof) {
		t.Fatal("SOUNDNESS : vol de note (nk faux) accepté contre la vraie racine")
	}
}

// CONSTAT D'AUDIT : le CIRCUIT n'interdit pas d'utiliser DEUX FOIS la même note en
// entrée — il produit alors DEUX nullifiers IDENTIQUES. La preuve « vérifie » (le
// circuit ne sait pas que c'est la même note), donc la défense anti-création-de-
// valeur repose ENTIÈREMENT sur la couche état : elle DOIT rejeter toute tx
// contenant des nullifiers en double (entre elles ET vs l'ensemble déjà dépensé).
// Ce test documente l'invariant que le câblage on-chain devra garantir.
func TestSpendN_DoublonEntreeProduitNfIdentiques(t *testing.T) {
	nk := spTestDigest("dup/nk")
	rho := spTestDigest("dup/rho")
	val := FromUint64(1_000_000)
	fee := FromUint64(2_500)
	inCm := SpendCommit(val, SpendOwnerTag(nk), rho)

	numLeaves := 1 << spendDepth
	leaves := make([][poseidonDigestLen]Felt, numLeaves)
	for i := 0; i < numLeaves; i++ {
		leaves[i] = spTestDigest("dup/leaf/" + itoaU64(uint64(i)))
	}
	leaves[0] = inCm
	_, tree := PoseidonCommit(leaves)
	path := spPathFromIndex(tree, 0)

	in := SpendNIn{Value: val, Rho: rho, Nk: nk, Path: path}
	// DEUX entrées = la MÊME note. Conservation : 2·val = Σ out + fee.
	out := SpendNOut{Value: val.Add(val).Sub(fee), OwnerTag: spTestDigest("dup/oo"), Rho: spTestDigest("dup/or")}
	w := SpendNWitness{Ins: []SpendNIn{in, in}, Outs: []SpendNOut{out}}

	public, proof := ProveSpendN(w, fee)
	// Le circuit accepte (il ignore que c'est la même note)…
	if !VerifySpendN(public, proof) {
		t.Fatal("le circuit devrait accepter un doublon d'entrée (il ne sait pas)")
	}
	// …mais les DEUX nullifiers sont IDENTIQUES : la couche état DOIT dédupliquer.
	if public.Nfs[0] != public.Nfs[1] {
		t.Fatal("attendu deux nullifiers identiques pour une note dupliquée")
	}
	t.Log("CONSTAT : doublon d'entrée -> nullifiers identiques ; le câblage état " +
		"DOIT rejeter les nullifiers en double dans une même tx (sinon création de valeur).")
}
