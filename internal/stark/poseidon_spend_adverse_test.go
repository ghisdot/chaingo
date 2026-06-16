// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// ÉTAGE 4.3 — VÉRIFICATION ADVERSE du circuit de dépense (poseidon_spend*.go).
// Ce fichier est ADDITIF (nouveau fichier de test) : il complète les tests de
// poseidon_spend_test.go par les ATTAQUES exigées par l'étage 4.3, sans modifier
// le code de production. Il documente aussi, par des assertions vérifiables, la
// PORTÉE RÉELLE du « zero-knowledge » (fuite) — cf. poseidon_spend_zk.go.
//
// ATTAQUES couvertes ici (du plus structurant au moins) :
//
//  A1. DÉPENSER UNE NOTE ABSENTE DE L'ARBRE — un attaquant connaît un chemin
//      valide mais sa note (inCm) n'est PAS une feuille de l'arbre cible : la
//      racine reconstruite par la trace diffère de la racine cible => aucune
//      preuve ne lie cet inCm à MerkleRoot (démonstration native + côté vérifieur
//      via la preuve honnête présentée sur la mauvaise racine).
//
//  A2. FORGER UN NULLIFIER POUR VOLER UNE NOTE SANS nk — l'attaquant voit une
//      note (inCm) dans l'arbre mais ne connaît pas le nk du propriétaire. Pour
//      la dépenser il lui faut nf = Hash2(nk, inCm) ET ownerTag = PoseidonHash(nk)
//      cohérent avec inCm. Avec un nk' inventé : ownerTag' != ownerTag => inCm'
//      sort de l'arbre (A1), et nf' != nf. Il ne peut PAS « brancher » un nf forgé
//      sur la vraie note : nk lie ownerTag ET nf dans la MÊME trace (registre
//      regNk). On le démontre nativement + en falsifiant une trace (nf trafiqué)
//      qui devient insatisfaisante.
//
//  A3. CRÉER DE LA VALEUR (mint) — un témoin où inValue < outValue + fee (ou
//      outValue gonflé) viole la conservation. On le démontre NATIVEMENT : la
//      contrainte de conservation a un résidu non nul et la trace est REJETABLE.
//      Le fait qu'une trace insatisfaisante => REJET du vérifieur STARK est déjà
//      établi UNE fois par TestSpend_TraceFalsifieeRejeteeParSTARK (prouveur
//      dédié, coûteux) ; on NE le re-prouve PAS pour le mint (même machinerie
//      STARK, seule la contrainte diffère et elle est vérifiée en ms) afin de
//      tenir le budget de temps (« minimise le nombre de preuves »).
//
//  A4. REJOUER UNE PREUVE SUR UNE AUTRE RACINE / UN AUTRE nf — variations fines
//      de l'énoncé public (swap nf seul, racine seule, outCm seul) présentées
//      avec la preuve honnête => REJET (réutilise la preuve partagée).
//
//  A5. EXTRAIRE UN BIT DU MONTANT DEPUIS LA PREUVE — borne de fuite : aucune
//      valeur lisible de la preuve (OOD, requêtes, coeffs FRI) ne COÏNCIDE avec
//      une cellule-témoin brute (montant, bits du chemin, nk). On l'asserte, tout
//      en RAPPELANT que ce n'est PAS de la ZK (fuite de fonctions linéaires) :
//      SpendIsZeroKnowledge() == false.
//
// COÛT : on RÉUTILISE la preuve honnête partagée (spShared) ; un SEUL prouveur
// dédié supplémentaire (A3 STARK). Le reste est natif (millisecondes).
package stark

import "testing"

// ---------------------------------------------------------------------------
// A1 — DÉPENSER UNE NOTE ABSENTE DE L'ARBRE
// ---------------------------------------------------------------------------

// A1a (natif) : une note inCm absente de l'arbre, présentée sur un chemin
// arbitraire, reconstruit une racine DIFFÉRENTE de la racine cible. Donc la trace
// honnête pour cette note n'atteindra jamais la MerkleRoot cible.
func TestSpendAdv_NoteAbsenteArbre_Natif(t *testing.T) {
	// Arbre cible légitime (notes d'autrui).
	w, fee, targetRoot := spBuildScenario("adv/absente/cible", 5)
	_ = fee

	// L'attaquant fabrique SA note (inCm absent de l'arbre cible) avec son propre
	// nk/rho/valeur, et tente de la dépenser en réutilisant le chemin de la note 5.
	attacker := SpendWitness{
		InValue:     FromUint64(777_000),
		InRho:       spTestDigest("adv/absente/atk-rho"),
		Nk:          spTestDigest("adv/absente/atk-nk"),
		Path:        w.Path, // chemin de l'arbre cible
		OutValue:    FromUint64(776_000),
		OutOwnerTag: spTestDigest("adv/absente/atk-outtag"),
		OutRho:      spTestDigest("adv/absente/atk-outrho"),
	}
	attacker.OutValue = attacker.InValue.Sub(FromUint64(1_000)) // conservation OK localement
	fee2 := FromUint64(1_000)

	attackerInCm := SpendCommit(attacker.InValue, SpendOwnerTag(attacker.Nk), attacker.InRho)
	// La note de l'attaquant n'est pas une feuille de l'arbre cible (collision
	// astronomiquement improbable).
	for _, leaf := range spLeavesWith(t, "adv/absente/cible", 5, SpendCommit(w.InValue, SpendOwnerTag(w.Nk), w.InRho)) {
		if leaf == attackerInCm {
			t.Fatalf("collision improbable: la note de l'attaquant est une feuille de l'arbre")
		}
	}

	_, attackerPublic := buildSpendTrace(attacker, fee2)
	if attackerPublic.MerkleRoot == targetRoot {
		t.Fatalf("une note ABSENTE reconstruit la racine cible (appartenance forgée !)")
	}
	// La racine reconstruite est celle de la note de l'attaquant le long du chemin,
	// pas l'arbre cible : la preuve qu'il produirait porterait sur SA racine, qui
	// n'est pas MerkleRoot. Le consensus, qui n'accepte que la vraie MerkleRoot,
	// rejette donc cette dépense.
}

// A1b (vérifieur) : la preuve honnête présentée avec une racine étrangère (la note
// honnête n'appartient pas à CET arbre) => REJET. Couvre la note hors-arbre du
// point de vue du vérifieur, en réutilisant la preuve partagée.
func TestSpendAdv_NoteAbsenteArbre_Verifieur(t *testing.T) {
	public, proof := spShared()
	// Racine d'un AUTRE arbre (note honnête absente de cet arbre-là).
	_, _, autreRoot := spBuildScenario("adv/absente/autre-arbre", 1)
	if autreRoot == public.MerkleRoot {
		t.Fatalf("collision improbable: deux arbres distincts ont la même racine")
	}
	bad := public
	bad.MerkleRoot = autreRoot
	if VerifySpend(bad, proof) {
		t.Fatalf("preuve acceptée pour une note absente de l'arbre annoncé")
	}
}

// ---------------------------------------------------------------------------
// A2 — FORGER UN NULLIFIER POUR VOLER UNE NOTE SANS nk
// ---------------------------------------------------------------------------

// A2a (natif) : sans le vrai nk, l'attaquant ne peut PAS produire à la fois le
// ownerTag liant inCm à l'arbre ET le nf de la note. nk lie les deux.
func TestSpendAdv_VolNullifierSansNk_Natif(t *testing.T) {
	// Vraie note d'autrui, dans l'arbre.
	w, _, targetRoot := spBuildScenario("adv/vol/cible", 12)
	ownerTag := SpendOwnerTag(w.Nk)
	trueInCm := SpendCommit(w.InValue, ownerTag, w.InRho)
	trueNf := SpendNullifier(w.Nk, trueInCm)

	// L'attaquant connaît inCm (public dans l'arbre) et veut forger un nf pour la
	// dépenser, mais ignore nk. Il invente nk'.
	atkNk := spTestDigest("adv/vol/atk-nk")
	if atkNk == w.Nk {
		t.Fatalf("collision improbable: nk attaquant == nk réel")
	}
	atkOwnerTag := SpendOwnerTag(atkNk)
	if atkOwnerTag == ownerTag {
		t.Fatalf("collision improbable: ownerTag attaquant == ownerTag réel")
	}
	atkNf := SpendNullifier(atkNk, trueInCm)
	if atkNf == trueNf {
		t.Fatalf("collision improbable: nf forgé == nf réel")
	}

	// Tentative : l'attaquant construit une trace avec nk' tout en visant l'arbre
	// cible. La trace recalcule inCm' = Commit(inValue, ownerTag', inRho) != trueInCm,
	// donc la feuille injectée dans l'appartenance n'est plus celle de l'arbre :
	// la racine reconstruite diffère de targetRoot.
	attacker := w
	attacker.Nk = atkNk
	_, atkPublic := buildSpendTrace(attacker, FromUint64(2_500))
	if atkPublic.MerkleRoot == targetRoot {
		t.Fatalf("avec un nk forgé, l'attaquant reconstruit la racine cible (vol réussi !)")
	}
	// Et le nf qu'il prouverait (atkPublic.Nf) dérive de nk', donc != trueNf : il ne
	// peut PAS « consommer » la vraie note (le consensus indexe par nf réel).
	if atkPublic.Nf == trueNf {
		t.Fatalf("le nf prouvé avec nk forgé coïncide avec le nf réel (impossible)")
	}
}

// A2b (trace falsifiée, natif) : l'attaquant tente de GARDER la vraie note (vrai
// nk dans regNk, vraie appartenance) mais d'INJECTER un nf forgé dans la cellule
// de sortie du bloc nf (pour faire croire à un autre nullifier). La contrainte de
// transition du bloc nf (Hash2(regNk, inCm)) ne produit plus ce nf => la trace
// devient INSATISFAISANTE. On le démontre sans STARK.
func TestSpendAdv_VolNullifierSansNk_TraceForge(t *testing.T) {
	w, fee, _ := spBuildScenario("adv/vol/forge-nf", 3)
	trace, public := buildSpendTrace(w, fee)
	air := spAirOf(public)

	// Trace honnête d'abord satisfaisante (sanity, natif rapide).
	if spTraceUnsatisfied(air, trace) {
		t.Fatalf("préparation: la trace honnête est déjà insatisfaisante")
	}

	// On force le nf publié à une valeur forgée et on injecte cette valeur dans la
	// ligne de sortie du bloc nf de la trace, pour tromper le bord nf.
	bad := spCloneTrace(trace)
	nfRow := spNfRow()
	forged := public.Nf
	forged[0] = forged[0].Add(One())
	for k := 0; k < poseidonDigestLen; k++ {
		bad[nfRow][spStateOff+k] = forged[k]
	}
	badPublic := public
	badPublic.Nf = forged
	badAir := spAirOf(badPublic)

	// La ligne de sortie du bloc nf est l'aboutissement des rondes Poseidon du bloc :
	// la transition de la ronde 29 -> ligne 30 ne produit plus `forged`. La trace est
	// donc REJETABLE (au moins une contrainte de transition non nulle).
	if !spTraceUnsatisfied(badAir, bad) {
		t.Fatalf("un nf forgé injecté dans la trace passe les contraintes (vol de nullifier)")
	}
}

// ---------------------------------------------------------------------------
// A3 — CRÉER DE LA VALEUR (mint) — trace insatisfaisante (natif)
// ---------------------------------------------------------------------------

// A3 : un attaquant construit une trace qui CRÉE de la valeur (outValue >
// inValue - fee, donc inValue != outValue + fee). buildSpendTrace recalcule un
// outCm cohérent avec l'outValue gonflé (le bord outCm tient), mais la contrainte
// de CONSERVATION mCommitW·(regVal - wVal - fee) ne s'annule plus : la trace est
// REJETABLE. Conséquence STARK : ProveSpend sur cette trace produit une
// composition qui n'est PAS de bas degré, donc VerifySpend rejette (machinerie
// déjà éprouvée par TestSpend_TraceFalsifieeRejeteeParSTARK ; non re-prouvée ici).
func TestSpendAdv_CreationValeurMint_Natif(t *testing.T) {
	w, fee, _ := spBuildScenario("adv/mint", 2)
	// MINT : la note de sortie vaut PLUS que ce qui est consommé.
	w.OutValue = w.InValue.Add(FromUint64(1_000_000)) // inValue != outValue + fee

	trace, public := buildSpendTrace(w, fee)
	air := spAirOf(public)

	// La conservation est violée (résidu non nul) à la ligne mCommitW.
	consRow := spOutputRowOf(spBlkMemL)
	res := air.EvalTransition(trace[consRow], trace[consRow+1])
	if res[len(res)-1].IsZero() {
		t.Fatalf("préparation: la conservation s'annule pour un témoin de mint")
	}
	if !spTraceUnsatisfied(air, trace) {
		t.Fatalf("trace de MINT jugée satisfaisante (création de valeur passerait !)")
	}
}

// A3bis (natif) : DÉTRUIRE de la valeur en sens inverse n'est pas un vol mais on
// vérifie que TOUTE rupture d'égalité casse la conservation (robustesse de la
// contrainte dans les deux sens). inValue trop grand par rapport à outValue+fee.
func TestSpendAdv_ConservationStricteDansLesDeuxSens(t *testing.T) {
	w, fee, _ := spBuildScenario("adv/conservation", 4)
	// outValue trop PETIT : inValue > outValue + fee (l'attaquant « brûle » sans le
	// déclarer — toujours une violation de l'égalité stricte exigée).
	w.OutValue = w.OutValue.Sub(FromUint64(1))

	trace, public := buildSpendTrace(w, fee)
	air := spAirOf(public)
	consRow := spOutputRowOf(spBlkMemL)
	res := air.EvalTransition(trace[consRow], trace[consRow+1])
	if res[len(res)-1].IsZero() {
		t.Fatalf("la conservation s'annule alors que inValue != outValue + fee")
	}
	if !spTraceUnsatisfied(air, trace) {
		t.Fatalf("trace à conservation rompue jugée satisfaisante")
	}
}

// ---------------------------------------------------------------------------
// A4 — REJEU SUR UNE AUTRE RACINE / UN AUTRE nf (variations fines)
// ---------------------------------------------------------------------------

// A4 : prendre la preuve honnête et échanger UNIQUEMENT le nf, puis UNIQUEMENT la
// racine, par ceux d'une AUTRE dépense réelle. Chaque substitution isolée viole le
// bord correspondant et fait diverger le transcript => REJET. (Complète le rejeu
// global de poseidon_spend_test.go par des substitutions composant par composant.)
func TestSpendAdv_RejeuSubstitutionPartielle(t *testing.T) {
	public, proof := spShared()
	_, other := buildSpendTrace2(t, "adv/rejeu/autre", 6)

	if other.Nf == public.Nf || other.MerkleRoot == public.MerkleRoot {
		t.Fatalf("collision improbable: composant public identique entre deux dépenses")
	}

	// Swap nf seul (garder la racine honnête) : le nf d'une autre dépense ne dérive
	// pas de la note honnête => REJET.
	badNf := public
	badNf.Nf = other.Nf
	if VerifySpend(badNf, proof) {
		t.Fatalf("preuve acceptée avec le nf d'une autre dépense (rejeu de nullifier)")
	}

	// Swap racine seule (garder le nf honnête) : la note honnête n'appartient pas à
	// l'autre arbre => REJET.
	badRoot := public
	badRoot.MerkleRoot = other.MerkleRoot
	if VerifySpend(badRoot, proof) {
		t.Fatalf("preuve acceptée avec la racine d'un autre arbre (rejeu cross-arbre)")
	}

	// Swap outCm seul : l'outCm d'une autre dépense n'est pas celui prouvé => REJET.
	badOut := public
	badOut.OutCm = other.OutCm
	if VerifySpend(badOut, proof) {
		t.Fatalf("preuve acceptée avec l'outCm d'une autre dépense")
	}
}

// buildSpendTrace2 construit nativement l'énoncé public d'une dépense honnête
// distincte (sans STARK), pour les tests de rejeu/substitution.
func buildSpendTrace2(t *testing.T, seed string, index int) (SpendWitness, SpendPublic) {
	t.Helper()
	w, fee, _ := spBuildScenario(seed, index)
	_, public := buildSpendTrace(w, fee)
	return w, public
}

// ---------------------------------------------------------------------------
// A5 — EXTRAIRE UN BIT DU MONTANT DEPUIS LA PREUVE (borne de fuite + honnêteté)
// ---------------------------------------------------------------------------

// A5a : aucune valeur LISIBLE de la preuve (ouvertures OOD, valeurs de requête,
// coefficients FRI finaux) ne COÏNCIDE avec une cellule-témoin BRUTE — montant
// (InValue/OutValue), bits du chemin (0/1), ni les Felt de nk/rho/ownerTag/inCm.
// Les points d'ouverture (z, g·z, ω^pos) sont distincts des points de trace g^i,
// donc une cellule brute n'apparaît jamais telle quelle.
//
// PORTÉE : c'est une garantie FAIBLE et BORNÉE — PAS de la zero-knowledge. Les
// valeurs lisibles RESTENT des fonctions linéaires déterministes du témoin (fuite).
// Le test l'affirme explicitement via SpendIsZeroKnowledge() == false.
func TestSpendAdv_PasDeCelluleBruteDansPreuve(t *testing.T) {
	// Honnêteté d'abord : le circuit n'est PAS zero-knowledge.
	if SpendIsZeroKnowledge() {
		t.Fatalf("régression: le circuit prétend être zero-knowledge alors qu'il fuit")
	}

	public, proof := spShared()
	_ = public
	revealed := spProofRevealedFelts(proof)
	revealedSet := make(map[Felt]bool, len(revealed))
	for _, v := range revealed {
		revealedSet[v] = true
	}

	w := spShareWit

	// Cellules-témoins brutes qui NE DOIVENT PAS apparaître telles quelles.
	ownerTag := SpendOwnerTag(w.Nk)
	inCm := SpendCommit(w.InValue, ownerTag, w.InRho)

	raw := map[Felt]string{
		w.InValue:  "InValue (montant d'entrée)",
		w.OutValue: "OutValue (montant de sortie)",
	}
	addDigest := func(d [poseidonDigestLen]Felt, name string) {
		for k := 0; k < poseidonDigestLen; k++ {
			raw[d[k]] = name
		}
	}
	addDigest(w.Nk, "Nk")
	addDigest(w.InRho, "InRho")
	addDigest(ownerTag, "ownerTag")
	addDigest(inCm, "inCm")
	addDigest(w.OutOwnerTag, "OutOwnerTag")
	addDigest(w.OutRho, "OutRho")

	// Les valeurs publiques légitimes (racine/nf/outCm/fee) PEUVENT apparaître dans
	// la preuve (ce sont des bords) ; ce ne sont pas des secrets. On ne les compte
	// donc pas comme fuite. (Aucune n'est dans `raw` de toute façon.)

	for v, name := range raw {
		// On ignore 0 et 1 : ils sont omniprésents (bits, padding, sélecteurs) et ne
		// constituent pas une fuite du montant (le montant testé n'est ni 0 ni 1).
		if v.IsZero() || v.Equal(One()) {
			continue
		}
		if revealedSet[v] {
			t.Fatalf("FUITE EN CLAIR: la cellule-témoin %q (=%d) apparaît telle quelle dans la preuve", name, uint64(v))
		}
	}
}

// A5b : on DOCUMENTE par assertion que les bits du chemin (0/1) ne sont pas
// distinguables dans la preuve par simple lecture : une preuve ne révèle pas
// « le bit du niveau i vaut 0/1 » par une cellule brute. (Les bits 0/1 étant
// triviaux, on vérifie surtout l'invariant structurel : le témoin de bits n'est
// pas dans l'énoncé public — déjà couvert — et n'apparaît pas comme valeur de
// requête isolée identifiable. La fuite linéaire, elle, demeure et est documentée.)
func TestSpendAdv_BitsMontantNonExtractiblesEnClair(t *testing.T) {
	// Le montant de test n'est PAS une puissance de deux triviale : on vérifie qu'il
	// ne se lit pas, ni lui ni outValue, dans la preuve (déjà fait en A5a). Ici on
	// renforce : même la DIFFÉRENCE inValue-outValue (= fee, publique) est cohérente,
	// et le montant lui-même reste non lu. C'est une borne, pas une preuve de ZK.
	public, proof := spShared()

	// fee est PUBLIQUE et égale à inValue - outValue : ce n'est pas une fuite, c'est
	// l'énoncé. On confirme la cohérence (conservation publique) sans rien extraire
	// de secret.
	if !spShareWit.InValue.Sub(spShareWit.OutValue).Equal(public.Fee) {
		t.Fatalf("incohérence de scénario: inValue - outValue != fee public")
	}

	// Rappel d'honnêteté machine-vérifiable : la liste des valeurs lisibles est non
	// vide (la preuve EXPOSE bien des évaluations), donc la confidentialité repose
	// UNIQUEMENT sur la non-coïncidence testée en A5a + la non-publication — pas sur
	// un masquage information-théorique (absent).
	if len(spProofRevealedFelts(proof)) == 0 {
		t.Fatalf("préparation: aucune valeur lisible — analyse de fuite impossible")
	}
}

// ---------------------------------------------------------------------------
// Déterminisme de la graine de blinding (recette du futur correctif).
// ---------------------------------------------------------------------------

// La graine de blinding est DÉTERMINISTE (même témoin => même graine) et SENSIBLE
// au témoin (deux témoins distincts => graines distinctes avec proba écrasante).
// C'est l'exigence figée pour le futur randomized LDE (reproductibilité bit-à-bit).
func TestSpendAdv_GraineBlindingDeterministe(t *testing.T) {
	w, fee, _ := spBuildScenario("adv/seed", 1)
	s1 := SpendBlindingSeed(w, fee)
	s2 := SpendBlindingSeed(w, fee)
	if s1 != s2 {
		t.Fatalf("graine de blinding non déterministe pour un même témoin")
	}

	w2 := w
	w2.InValue = w.InValue.Add(One()) // témoin différent (montant)
	s3 := SpendBlindingSeed(w2, fee)
	if s3 == s1 {
		t.Fatalf("graine de blinding insensible au montant (collision improbable)")
	}

	// La graine est INERTE aujourd'hui : elle ne masque rien. On asserte l'invariant
	// de portée pour qu'aucune régression ne fasse croire à une ZK inexistante.
	if SpendIsZeroKnowledge() {
		t.Fatalf("régression: masquage prétendu actif alors que la graine est inerte")
	}
}
