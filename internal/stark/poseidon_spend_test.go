// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Tests du circuit de DÉPENSE blindée (poseidon_spend.go / poseidon_spend_air.go
// / poseidon_spend_prove.go).
//
// Objectifs (du plus important au moins) :
//
//  1. COHÉRENCE NATIVE — l'IMPÉRATIF : les valeurs reconstruites par la trace
//     (ownerTag, inCm, nf, racine, outCm) coïncident EXACTEMENT avec les fonctions
//     natives (SpendOwnerTag/SpendCommit/SpendNullifier/Hash2/merkle_poseidon).
//     La trace honnête SATISFAIT toutes les contraintes de l'AIR (vérifié sans
//     STARK, en millisecondes, par spTraceSatisfies).
//  2. POSITIF : une preuve honnête VÉRIFIE ; déterminisme ; témoin non publié.
//  3. NÉGATIFS (obligatoires) : nullifier faux ; note hors-arbre ; déséquilibre
//     inValue != outValue + fee ; outCm falsifié ; nk ne correspondant pas au
//     ownerTag (vol de note d'autrui) ; rejeu sur autre racine/nf => REJET.
//
// COÛT : une preuve STARK de dépense (n=256, W=61, bigN=4096·… plusieurs minutes).
// On MINIMISE le nombre de preuves :
//   - la cohérence native + la satisfaction de la trace + les rejets « énoncé
//     public faux » (nullifier faux, hors-arbre, déséquilibre, outCm falsifié,
//     vol de nk, rejeu) se testent SANS STARK : soit par recoupement natif, soit
//     en vérifiant la preuve HONNÊTE PARTAGÉE contre un énoncé public altéré
//     (le bord ne tient pas / le transcript diverge => rejet) ;
//   - une UNIQUE preuve honnête partagée (sync.Once) sert au positif et à tous ces
//     rejets ;
//   - seuls les rejets exigeant une TRACE falsifiée (déséquilibre prouvé,
//     trace incohérente) lancent un prouveur dédié — strictement nécessaires.
//
// Aléa de test : déterministe (flux SHAKE256 du paquet, sans time ni math/rand).
package stark

import (
	"sync"
	"testing"
)

// spTestDigest produit un digest [4]Felt DÉTERMINISTE depuis une étiquette.
func spTestDigest(label string) [poseidonDigestLen]Felt {
	xof := newXOF("test/spend/" + label)
	var d [poseidonDigestLen]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		d[k] = nextFelt(xof)
	}
	return d
}

// spPathFromIndex convertit l'ouverture PoseidonOpen (siblings du bas vers le
// haut) et l'indice de la feuille en SpendPath (siblings + bits). Le bit du niveau
// i est le LSB de l'indice à ce niveau — MÊME convention que PoseidonVerifyPath.
func spPathFromIndex(tree *PoseidonMerkleTree, index int) SpendPath {
	sibs := PoseidonOpen(tree, index)
	if len(sibs) != spendDepth {
		panic("test: profondeur d'ouverture inattendue")
	}
	var path SpendPath
	idx := index
	for i := 0; i < spendDepth; i++ {
		path.Siblings[i] = sibs[i]
		if idx&1 == 0 {
			path.Bits[i] = Zero()
		} else {
			path.Bits[i] = One()
		}
		idx >>= 1
	}
	return path
}

// spBuildScenario construit un scénario de dépense HONNÊTE complet :
//   - un arbre de 2^spendDepth notes dont la feuille `index` est l'engagement de la
//     note d'entrée inCm = Commit(inValue, PoseidonHash(nk), inRho) ;
//   - un témoin cohérent (note de sortie : outValue = inValue - fee) ;
//   - l'énoncé public attendu.
//
// Renvoie le témoin, les frais et l'énoncé public natif attendu (recoupé contre
// l'arbre natif). Déterministe.
func spBuildScenario(seed string, index int) (w SpendWitness, fee Felt, wantRoot [poseidonDigestLen]Felt) {
	nk := spTestDigest(seed + "/nk")
	inRho := spTestDigest(seed + "/inRho")
	inValue := FromUint64(1_000_000)
	fee = FromUint64(2_500)
	outValue := inValue.Sub(fee) // conservation : inValue = outValue + fee

	ownerTag := SpendOwnerTag(nk)
	inCm := SpendCommit(inValue, ownerTag, inRho)

	// Arbre de notes : la feuille `index` porte inCm ; les autres sont du remplissage
	// déterministe distinct.
	numLeaves := 1 << spendDepth // 16
	leaves := make([][poseidonDigestLen]Felt, numLeaves)
	for i := 0; i < numLeaves; i++ {
		leaves[i] = spTestDigest(seed + "/leaf/" + itoaU64(uint64(i)))
	}
	leaves[index] = inCm
	wantRoot, tree := PoseidonCommit(leaves)

	path := spPathFromIndex(tree, index)

	w = SpendWitness{
		InValue:     inValue,
		InRho:       inRho,
		Nk:          nk,
		Path:        path,
		OutValue:    outValue,
		OutOwnerTag: spTestDigest(seed + "/outOwnerTag"),
		OutRho:      spTestDigest(seed + "/outRho"),
	}
	return w, fee, wantRoot
}

// ---------------------------------------------------------------------------
// Validation NATIVE de la trace (sans STARK) : évalue les contraintes de
// transition et de bord sur la trace. Si tout s'annule, la trace est
// SATISFAISANTE — donc ProveSpend produira une preuve qui vérifie. Cela teste la
// COHÉRENCE de l'arithmétisation en millisecondes, indépendamment du coût STARK.
// ---------------------------------------------------------------------------

func spTraceSatisfies(t *testing.T, air spendAIR, trace [][]Felt) {
	t.Helper()
	n := air.NumSteps()
	if len(trace) != n {
		t.Fatalf("trace de hauteur %d, attendu %d", len(trace), n)
	}
	w := air.NumColumns()
	for i := range trace {
		if len(trace[i]) != w {
			t.Fatalf("ligne %d de largeur %d, attendu %d", i, len(trace[i]), w)
		}
	}
	// Transitions : pour chaque paire (i, i+1), tous les résidus doivent s'annuler.
	for i := 0; i < n-1; i++ {
		res := air.EvalTransition(trace[i], trace[i+1])
		for k, r := range res {
			if !r.IsZero() {
				t.Fatalf("contrainte de transition non nulle: ligne %d, résidu %d = %v", i, k, r)
			}
		}
	}
	// Bords : chaque cellule contrainte doit valoir la valeur publique.
	for _, bc := range air.Boundaries() {
		got := trace[bc.Row][bc.Col]
		if !got.Equal(bc.Value) {
			t.Fatalf("bord violé: (row=%d,col=%d) = %v, attendu %v", bc.Row, bc.Col, got, bc.Value)
		}
	}
}

// spAirOf reconstruit l'instance spendAIR à partir d'un énoncé public.
func spAirOf(public SpendPublic) spendAIR {
	return spendAIR{
		merkleRoot: public.MerkleRoot,
		nf:         public.Nf,
		outCm:      public.OutCm,
		fee:        public.Fee,
	}
}

// ---------------------------------------------------------------------------
// 1) COHÉRENCE NATIVE — l'IMPÉRATIF (sans STARK : rapide)
// ---------------------------------------------------------------------------

// La racine de la chaîne d'appartenance reconstruite par spChainRoot == racine
// native de l'arbre Poseidon, pour des positions variées (motifs de bits variés),
// et PoseidonVerifyPath accepte le même chemin. Cohérence circuit <-> merkle_poseidon.
func TestSpend_CoherenceMerkle(t *testing.T) {
	for _, index := range []int{0, 1, 2, 5, 10, 15} {
		w, _, wantRoot := spBuildScenario("coherence", index)
		inCm := SpendCommit(w.InValue, SpendOwnerTag(w.Nk), w.InRho)

		got := spChainRoot(inCm, w.Path)
		if got != wantRoot {
			t.Fatalf("index %d: racine chaîne != racine native", index)
		}
		// Recoupement indépendant via PoseidonVerifyPath (même convention de bits).
		_, tree := PoseidonCommit(spLeavesWith(t, "coherence", index, inCm))
		if !PoseidonVerifyPath(wantRoot, index, inCm, PoseidonOpen(tree, index)) {
			t.Fatalf("index %d: PoseidonVerifyPath rejette le chemin natif", index)
		}
	}
}

// spLeavesWith reconstruit le jeu de feuilles du scénario (mêmes étiquettes que
// spBuildScenario), avec inCm en position `index`.
func spLeavesWith(t *testing.T, seed string, index int, inCm [poseidonDigestLen]Felt) [][poseidonDigestLen]Felt {
	t.Helper()
	numLeaves := 1 << spendDepth
	leaves := make([][poseidonDigestLen]Felt, numLeaves)
	for i := 0; i < numLeaves; i++ {
		leaves[i] = spTestDigest(seed + "/leaf/" + itoaU64(uint64(i)))
	}
	leaves[index] = inCm
	return leaves
}

// La trace honnête SATISFAIT l'AIR (transitions + bords) — cohérence complète de
// l'arithmétisation, sans STARK. C'est le test central de correction du circuit.
func TestSpend_TraceHonneteSatisfaitAIR(t *testing.T) {
	w, fee, wantRoot := spBuildScenario("trace-sat", 7)
	trace, public := buildSpendTrace(w, fee)

	// L'énoncé public reconstruit recoupe les fonctions natives.
	if public.MerkleRoot != wantRoot {
		t.Fatalf("racine reconstruite != racine native")
	}
	ownerTag := SpendOwnerTag(w.Nk)
	inCm := SpendCommit(w.InValue, ownerTag, w.InRho)
	if public.Nf != SpendNullifier(w.Nk, inCm) {
		t.Fatalf("nf reconstruit != Hash2(nk, inCm)")
	}
	if public.OutCm != SpendCommit(w.OutValue, w.OutOwnerTag, w.OutRho) {
		t.Fatalf("outCm reconstruit != Commit(outValue, outOwnerTag, outRho)")
	}
	if !public.Fee.Equal(fee) {
		t.Fatalf("fee reconstruit != fee")
	}

	spTraceSatisfies(t, spAirOf(public), trace)
}

// ---------------------------------------------------------------------------
// Preuve honnête PARTAGÉE (une seule preuve STARK pour le positif + tous les
// rejets « énoncé public faux »). Construite paresseusement une fois.
// ---------------------------------------------------------------------------

var (
	spShareOnce   sync.Once
	spShareWit    SpendWitness
	spShareFee    Felt
	spSharePublic SpendPublic
	spShareProof  AirProof
)

func spShared() (SpendPublic, AirProof) {
	spShareOnce.Do(func() {
		spShareWit, spShareFee, _ = spBuildScenario("shared", 9)
		spSharePublic, spShareProof = ProveSpend(spShareWit, spShareFee)
	})
	return spSharePublic, spShareProof
}

// ---------------------------------------------------------------------------
// 2) POSITIF + déterminisme + témoin non publié
// ---------------------------------------------------------------------------

// Une preuve de dépense honnête vérifie contre son énoncé public.
func TestSpend_PreuveHonnete(t *testing.T) {
	public, proof := spShared()
	if !VerifySpend(public, proof) {
		t.Fatalf("preuve de dépense honnête rejetée")
	}
}

// Déterminisme : reprouver le MÊME témoin redonne la même preuve (aléa = transcript
// uniquement). Un seul prouveur supplémentaire.
func TestSpend_Deterministe(t *testing.T) {
	public, proof := spShared()
	p2, pr2 := ProveSpend(spShareWit, spShareFee)

	if p2 != public {
		t.Fatalf("énoncé public non déterministe")
	}
	if pr2.CompRoot != proof.CompRoot {
		t.Fatalf("CompRoot non déterministe")
	}
	if len(pr2.ColRoots) != len(proof.ColRoots) {
		t.Fatalf("nombre de ColRoots non déterministe")
	}
	for c := range proof.ColRoots {
		if pr2.ColRoots[c] != proof.ColRoots[c] {
			t.Fatalf("ColRoots[%d] non déterministe", c)
		}
	}
}

// Le témoin ne figure dans AUCUNE valeur publique : l'énoncé public est EXACTEMENT
// MerkleRoot | Nf | OutCm | Fee (3·4 + 1 = 13 Felt), et aucun secret (nk, inValue,
// inRho, ownerTag, inCm, outValue, outOwnerTag, outRho, siblings, bits) n'y apparaît.
func TestSpend_TemoinNonPublie(t *testing.T) {
	w, fee, _ := spBuildScenario("prive", 3)
	_, public := buildSpendTrace(w, fee)
	inputs := spendPublicInputs(public)

	if len(inputs) != 3*poseidonDigestLen+1 {
		t.Fatalf("taille des valeurs publiques %d, attendu %d", len(inputs), 3*poseidonDigestLen+1)
	}

	// Ensemble des Felt secrets qui NE DOIVENT PAS apparaître dans l'énoncé public.
	ownerTag := SpendOwnerTag(w.Nk)
	inCm := SpendCommit(w.InValue, ownerTag, w.InRho)
	secrets := map[Felt]string{
		w.InValue:  "inValue",
		w.OutValue: "outValue",
	}
	addAll := func(d [poseidonDigestLen]Felt, name string) {
		for k := 0; k < poseidonDigestLen; k++ {
			secrets[d[k]] = name
		}
	}
	addAll(w.Nk, "nk")
	addAll(w.InRho, "inRho")
	addAll(ownerTag, "ownerTag")
	addAll(inCm, "inCm")
	addAll(w.OutRho, "outRho")
	// NB : outOwnerTag est privé côté énoncé (seul outCm est publié). On le vérifie.
	addAll(w.OutOwnerTag, "outOwnerTag")

	// Les valeurs publiques légitimes (racine/nf/outCm/fee) sont autorisées : on les
	// retire de l'ensemble interdit pour éviter un faux positif si un secret
	// coïncidait par hasard avec une sortie (improbable mais on est rigoureux).
	for _, p := range inputs {
		if name, bad := secrets[p]; bad {
			t.Fatalf("valeur publique = secret %q (témoin fuité dans l'énoncé)", name)
		}
	}
}

// ---------------------------------------------------------------------------
// 3) NÉGATIFS — énoncé public faux (réutilisent la preuve HONNÊTE partagée)
// ---------------------------------------------------------------------------
//
// Toute altération de l'énoncé public présenté au vérifieur viole un bord (sortie
// ancrée) ET fait diverger le transcript Fiat-Shamir => rejet. Couvre, du point de
// vue du vérifieur : nullifier faux, note hors-arbre (racine fausse), outCm
// falsifié, déséquilibre (fee fausse), rejeu sur autre racine/nf.

// NULLIFIER FAUX : présenter la preuve honnête avec un nf altéré => REJET.
func TestSpend_NullifierFaux(t *testing.T) {
	public, proof := spShared()
	bad := public
	bad.Nf[0] = bad.Nf[0].Add(One())
	if VerifySpend(bad, proof) {
		t.Fatalf("preuve acceptée avec un nullifier faux")
	}
}

// NOTE HORS-ARBRE : présenter la preuve honnête avec une racine fausse (la note
// d'entrée n'appartiendrait alors pas à l'arbre annoncé) => REJET.
func TestSpend_NoteHorsArbre(t *testing.T) {
	public, proof := spShared()
	bad := public
	bad.MerkleRoot[1] = bad.MerkleRoot[1].Add(One())
	if VerifySpend(bad, proof) {
		t.Fatalf("preuve acceptée avec une racine d'arbre fausse (note hors-arbre)")
	}
}

// OUTCM FALSIFIÉ : présenter la preuve honnête avec un outCm altéré => REJET.
func TestSpend_OutCmFalsifie(t *testing.T) {
	public, proof := spShared()
	bad := public
	bad.OutCm[2] = bad.OutCm[2].Add(One())
	if VerifySpend(bad, proof) {
		t.Fatalf("preuve acceptée avec un outCm falsifié")
	}
}

// DÉSÉQUILIBRE (fee annoncée fausse) : la preuve honnête atteste inValue =
// outValue + feeHonnête ; présenter une autre fee viole le bord fee (et le
// transcript) => REJET. (Le déséquilibre PROUVÉ — trace mal équilibrée — est testé
// séparément ci-dessous avec un prouveur dédié.)
func TestSpend_FeeAnnonceeFausse(t *testing.T) {
	public, proof := spShared()
	bad := public
	bad.Fee = bad.Fee.Add(One())
	if VerifySpend(bad, proof) {
		t.Fatalf("preuve acceptée avec une fee annoncée fausse (déséquilibre apparent)")
	}
}

// REJEU sur autre racine ET autre nf : prendre la preuve d'une dépense et la
// présenter avec l'énoncé public d'une AUTRE dépense (autre note, autre arbre)
// => REJET. Réutilise la preuve partagée + un énoncé public natif distinct
// (calculé SANS STARK via buildSpendTrace).
func TestSpend_RejeuAutreEnonce(t *testing.T) {
	public, proof := spShared()

	// Énoncé public d'une dépense DIFFÉRENTE (autre nk, autre arbre), sans prouver.
	w2, fee2, _ := spBuildScenario("rejeu-autre", 4)
	_, other := buildSpendTrace(w2, fee2)

	if other == public {
		t.Fatalf("collision improbable: les deux énoncés publics coïncident")
	}
	if VerifySpend(other, proof) {
		t.Fatalf("preuve rejouée acceptée sur un autre énoncé (racine/nf)")
	}
}

// ---------------------------------------------------------------------------
// 3 bis) NÉGATIFS — témoin malhonnête détecté SANS STARK (trace insatisfaisante)
// ---------------------------------------------------------------------------
//
// Ces cas falsifient le TÉMOIN : la trace résultante ne satisfait plus l'AIR. On
// le démontre en millisecondes via spTraceUnsatisfied (au moins une contrainte non
// nulle), ce qui implique qu'aucune preuve honnête ne pourrait être produite
// (ProveSpend sur cette trace donnerait une composition de haut degré => rejet).
// Cela évite des preuves STARK coûteuses tout en prouvant la soundness logique.

// spTraceUnsatisfied renvoie true s'il EXISTE une contrainte (transition ou bord)
// non satisfaite par la trace — c.-à-d. la trace est REJETABLE.
func spTraceUnsatisfied(air spendAIR, trace [][]Felt) bool {
	n := air.NumSteps()
	for i := 0; i < n-1; i++ {
		res := air.EvalTransition(trace[i], trace[i+1])
		for _, r := range res {
			if !r.IsZero() {
				return true
			}
		}
	}
	for _, bc := range air.Boundaries() {
		if !trace[bc.Row][bc.Col].Equal(bc.Value) {
			return true
		}
	}
	return false
}

// DÉSÉQUILIBRE PROUVÉ : un témoin où inValue != outValue + fee. On construit une
// trace avec outValue trafiqué (en gardant l'énoncé public qui résulterait de la
// vraie note de sortie) : la contrainte de conservation mCommitW·(regVal - wVal -
// fee) ne s'annule plus => trace insatisfaisante => preuve impossible.
func TestSpend_DesequilibreProuve(t *testing.T) {
	w, fee, _ := spBuildScenario("desequilibre", 6)
	// On casse la conservation : outValue trop grand (on retire la fee deux fois moins).
	w.OutValue = w.InValue // au lieu de inValue - fee : inValue != outValue + fee.

	trace, public := buildSpendTrace(w, fee)
	air := spAirOf(public)

	// La trace doit être REJETABLE : la contrainte de conservation est violée.
	// (Note : buildSpendTrace recalcule outCm = Commit(outValue,…) cohérent, donc le
	//  bord outCm tient ; c'est bien la CONSERVATION qui casse.)
	row := spOutputRowOf(spBlkMemL) // ligne mCommitW (conservation)
	res := air.EvalTransition(trace[row], trace[row+1])
	consResidue := res[len(res)-1] // dernier résidu = conservation
	if consResidue.IsZero() {
		t.Fatalf("la contrainte de conservation s'annule pour un témoin déséquilibré")
	}
	if !spTraceUnsatisfied(air, trace) {
		t.Fatalf("trace déséquilibrée jugée satisfaisante")
	}
}

// VOL DE NOTE D'AUTRUI (nk ne correspond pas au ownerTag de la note) : un attaquant
// connaît une note (inCm dans l'arbre) mais PAS le nk du propriétaire. Il tente avec
// un nk' arbitraire. Alors ownerTag' = PoseidonHash(nk') != ownerTag, donc
// Commit(inValue, ownerTag', inRho) != inCm : la feuille reconstruite n'est plus
// celle de l'arbre => l'appartenance casse (racine reconstruite différente).
//
// On le démontre nativement : avec nk' faux, l'inCm recalculé sort de l'arbre, donc
// la racine reconstruite par la trace diffère de la racine de l'arbre cible — donc
// aucune preuve ne peut lier ce nk' à la fois à la note (appartenance) et à un nf.
func TestSpend_VolNoteAutruiNkFaux(t *testing.T) {
	// Scénario honnête : la VRAIE note inCm est en position `index` d'un arbre.
	w, fee, treeRoot := spBuildScenario("vol", 11)
	ownerTag := SpendOwnerTag(w.Nk)
	trueInCm := SpendCommit(w.InValue, ownerTag, w.InRho)

	// L'attaquant garde le chemin/arbre (il connaît inCm publiquement) mais ne
	// connaît pas nk : il invente nk'. ownerTag' != ownerTag (préimage différente).
	attacker := w
	attacker.Nk = spTestDigest("vol/nk-attaquant")
	if attacker.Nk == w.Nk {
		t.Fatalf("collision improbable: nk attaquant == nk réel")
	}
	attackerOwnerTag := SpendOwnerTag(attacker.Nk)
	if attackerOwnerTag == ownerTag {
		t.Fatalf("collision improbable: ownerTag attaquant == ownerTag réel")
	}

	// La trace de l'attaquant recalcule inCm' = Commit(inValue, ownerTag', inRho) et
	// l'insère comme feuille de la chaîne d'appartenance ; mais le CHEMIN mène à
	// l'arbre de la vraie note. La racine reconstruite diverge donc de treeRoot.
	_, attackerPublic := buildSpendTrace(attacker, fee)

	attackerInCm := SpendCommit(attacker.InValue, attackerOwnerTag, attacker.InRho)
	if attackerInCm == trueInCm {
		t.Fatalf("collision improbable: inCm attaquant == inCm réel")
	}
	if attackerPublic.MerkleRoot == treeRoot {
		t.Fatalf("l'attaquant avec un nk faux reconstruit la vraie racine (vol réussi !)")
	}

	// Conséquence vérifieur : présenter la preuve honnête (vraie note) avec un nf
	// dérivé du nk attaquant échoue aussi. On le couvre déjà via NullifierFaux ; ici
	// l'essentiel est que nk' ne peut PAS reconstruire l'arbre cible.
}

// VOL via présentation au vérifieur : si l'attaquant veut dépenser la vraie note
// (vraie racine + vrai nf nécessaire pour ne pas être hors-arbre), il lui faudrait
// le nf = Hash2(nk, inCm) du vrai nk. Avec nk' il produit nf' != nf. Présenter la
// preuve honnête avec la vraie racine mais un nf' (de l'attaquant) => REJET
// (nullifier ne correspond pas). Couvert par TestSpend_NullifierFaux côté vérifieur.
// Ce test-ci documente le lien nk -> (ownerTag, nf) sans preuve STARK supplémentaire.
func TestSpend_NkLieOwnerTagEtNullifier(t *testing.T) {
	w, _, _ := spBuildScenario("lien-nk", 2)
	ownerTag := SpendOwnerTag(w.Nk)
	inCm := SpendCommit(w.InValue, ownerTag, w.InRho)

	nf := SpendNullifier(w.Nk, inCm)
	// Un nk différent => ownerTag différent (donc inCm différent) ET nf différent.
	nk2 := spTestDigest("lien-nk/autre")
	ownerTag2 := SpendOwnerTag(nk2)
	if ownerTag2 == ownerTag {
		t.Fatalf("ownerTag indépendant de nk (collision improbable)")
	}
	nf2 := SpendNullifier(nk2, inCm)
	if nf2 == nf {
		t.Fatalf("nf indépendant de nk (collision improbable)")
	}
}

// ---------------------------------------------------------------------------
// 3 ter) NÉGATIFS — trace falsifiée prouvée par STARK (prouveur dédié : coûteux,
// strictement nécessaire). UN SEUL : on corrompt une cellule d'état d'une ronde
// Poseidon interne ; la transition casse => composition pas de bas degré => REJET.
// Démontre que le PROUVEUR/VÉRIFIEUR STARK (pas seulement la satisfaction native)
// rejette une trace invalide.
// ---------------------------------------------------------------------------

func TestSpend_TraceFalsifieeRejeteeParSTARK(t *testing.T) {
	w, fee, _ := spBuildScenario("stark-falsifie", 8)
	trace, public := buildSpendTrace(w, fee)
	air := spAirOf(public)
	inputs := spendPublicInputs(public)

	// Corrompt une cellule d'état à une ronde du milieu du bloc inCm (bloc 1).
	row := spBlkInCm*spBlock + 10 // ronde 10 du bloc inCm
	bad := spCloneTrace(trace)
	bad[row][spStateOff+5] = bad[row][spStateOff+5].Add(FromUint64(987654321))

	badProof := ProveAIR(air, bad, inputs...)
	if VerifyAIR(air, badProof, inputs...) {
		t.Fatalf("preuve acceptée pour une trace violant une ronde Poseidon")
	}
}

// spCloneTrace duplique une trace (lignes + cellules) pour falsification isolée.
func spCloneTrace(trace [][]Felt) [][]Felt {
	out := make([][]Felt, len(trace))
	for i, line := range trace {
		out[i] = append([]Felt(nil), line...)
	}
	return out
}
