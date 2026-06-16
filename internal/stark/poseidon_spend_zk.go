// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// ÉTAGE 4.3 — MASQUAGE ZERO-KNOWLEDGE du circuit de dépense : ANALYSE, VERDICT
// de faisabilité, et RECETTE du correctif. Fichier ADDITIF : il n'écrit aucun
// fichier existant, il les APPELLE et les DOCUMENTE.
//
// ┌──────────────────────────────────────────────────────────────────────────┐
// │ VERDICT BRUTAL : LE CIRCUIT DE DÉPENSE N'EST PAS ZERO-KNOWLEDGE.           │
// │                                                                            │
// │ « ZK » ici se limite STRICTEMENT à « le témoin n'est pas PUBLIÉ » (ni en   │
// │ clair, ni comme valeur publique du transcript : voir SpendPublic, qui ne   │
// │ porte que MerkleRoot | Nf | OutCm | Fee). Ce N'EST PAS la zero-knowledge   │
// │ au sens cryptographique (existence d'un simulateur produisant une preuve   │
// │ indistinguable SANS le témoin). La preuve produite par ProveSpend FUIT de  │
// │ l'information sur le témoin, comme analysé ci-dessous.                      │
// │                                                                            │
// │ CE QUI FUIT, PRÉCISÉMENT (côté preuve AirProof, cf. stark_mc.go) :         │
// │                                                                            │
// │  1. OUVERTURES HORS-DOMAINE (OOD). Pour CHAQUE colonne c (W=61 colonnes,   │
// │     dont les colonnes d'état qui portent les SECRETS), la preuve révèle    │
// │     Tc(z) et Tc(g·z) — deux évaluations du polynôme d'interpolation de la  │
// │     colonne en des points z, g·z choisis par le transcript. Tc est de      │
// │     degré < n=256 et interpole EXACTEMENT les 256 valeurs de cellule de la │
// │     colonne (dont les valeurs SECRÈTES : montant en cellule d'état 8 du    │
// │     bloc inCm, nk, chemin Merkle, etc.). Tc(z), Tc(g·z) sont donc des      │
// │     FONCTIONS LINÉAIRES DÉTERMINISTES du témoin. Elles ne sont PAS         │
// │     simulables sans le témoin => fuite.                                    │
// │                                                                            │
// │  2. OUVERTURES AUX REQUÊTES. Pour mcNumQueries=32 positions du domaine LDE │
// │     (taille bigN), la preuve révèle Tc(ω^pos) pour chaque colonne, plus    │
// │     H(ω^pos) et P(ω^pos). Ce sont encore des évaluations (sur le domaine   │
// │     étendu, pas le domaine de trace) — donc des fonctions linéaires du     │
// │     témoin. 32 positions × 61 colonnes = 1952 évaluations de colonnes      │
// │     révélées par preuve.                                                   │
// │                                                                            │
// │  => Au total ~ (2 + 32) = 34 évaluations par colonne d'état sont révélées. │
// │     Aucune ne COÏNCIDE avec un point du domaine de trace g^i (z est        │
// │     hors-domaine ; les positions de requête sont des points LDE distincts  │
// │     des g^i), donc AUCUNE ouverture n'est ÉGALE à une cellule-témoin brute.│
// │     MAIS ce sont des combinaisons linéaires connues du vecteur de 256      │
// │     cellules de la colonne : un distingueur peut exploiter ces relations.  │
// │     La preuve N'EST PAS indistinguable d'une preuve simulée sans témoin.   │
// │                                                                            │
// │  CONSÉQUENCE SUR LE MONTANT : un bit isolé du montant n'est PAS lisible    │
// │  directement (aucune ouverture = une cellule brute, et le montant vit dans │
// │  Goldilocks, pas en base 2 dans la trace : il n'y a pas de « colonne de    │
// │  bit » du montant dans ce circuit — voir aussi le GAP range-proof). Mais   │
// │  des FONCTIONS LINÉAIRES du montant fuient via OOD/requêtes. On NE         │
// │  prétend donc PAS cacher le montant au sens ZK ; on prétend seulement qu'il│
// │  n'est pas PUBLIÉ et qu'aucune ouverture ne le donne en clair.             │
// │                                                                            │
// │  AUTRES FUITES STRUCTURELLES (métadonnées) : la TAILLE de la preuve, le    │
// │  nombre de colonnes W=61, la profondeur d'arbre spendDepth=4, le motif de  │
// │  glue — tout cela est PUBLIC (énoncé du schéma). Deux dépenses ont des     │
// │  preuves de structure identique ; seules les valeurs ouvertes diffèrent.   │
// └──────────────────────────────────────────────────────────────────────────┘
//
// ┌──────────────────────────────────────────────────────────────────────────┐
// │ POURQUOI LE MASQUAGE N'EST PAS AJOUTÉ ICI (faisabilité honnête).          │
// │                                                                            │
// │ Le masquage zero-knowledge STANDARD d'un STARK est le « randomized LDE »   │
// │ (a.k.a. ZK-STARK par sur-échantillonnage) : on étend le degré de chaque    │
// │ polynôme de colonne de q coefficients ALÉATOIRES au-delà du degré imposé   │
// │ par la trace, de sorte que jusqu'à q ouvertures (OOD + requêtes) soient    │
// │ MASQUÉES de façon information-théorique (le surplus de degré « absorbe »   │
// │ les évaluations révélées). Concrètement il faut :                          │
// │                                                                            │
// │   (a) ENGAGER des polynômes de colonne de degré < n + q (q ≳ #ouvertures   │
// │       par colonne, ici ≳ 34), PAS < n. Le prouveur ajoute q coefficients   │
// │       aléatoires de haut degré (ou q lignes de blinding) par colonne.      │
// │   (b) ÉLARGIR la borne de bas degré prouvée par FRI de q (bigN/Blowup doit │
// │       couvrir n + q et le degré de H qui en découle), faute de quoi FRI    │
// │       rejette la preuve honnête.                                           │
// │   (c) RANDOMISER aussi le polynôme de composition H (masque de degré) pour │
// │       que H(z) ne fuite pas non plus.                                      │
// │                                                                            │
// │ OR ProveAIR/VerifyAIR (stark_mc.go) INTERPOLENT chaque colonne sur         │
// │ EXACTEMENT n=NumSteps() points (degré < n, déterminé à 100 % par la trace) │
// │ et fixent bigN = mcBlowup·nextPow2(MaxDegree·n) SANS marge q. Ajouter le   │
// │ masquage exige donc de MODIFIER le moteur (engagement degré < n+q + borne  │
// │ FRI élargie + masque de H). Cela SORT du périmètre ADDITIF de cet étage    │
// │ (interdiction de réécrire les fichiers existants).                         │
// │                                                                            │
// │ Tout « blinding » bricolé au seul niveau du TÉMOIN (randomiser des cellules│
// │ non contraintes) NE masquerait PAS les cellules SECRÈTES : leurs polynômes │
// │ de colonne sont ÉPINGLÉS par les contraintes de transition à leur vraie    │
// │ valeur en chaque point de trace ; randomiser d'AUTRES colonnes ajoute du   │
// │ bruit sur ces autres colonnes, pas sur le montant/nk/chemin. Ce serait un  │
// │ FAUX masquage, donc DANGEREUX (illusion de ZK). On s'y REFUSE.             │
// │                                                                            │
// │ DÉCISION : NE RIEN CASSER. On documente la fuite (ci-dessus), on capture   │
// │ la RECETTE du correctif (ci-dessus + SpendBlindingSeed), et on laisse le   │
// │ masquage réel comme TRAVAIL D'UN ÉTAGE FUTUR sur le MOTEUR (randomized LDE)│
// │ — à AUDITER. La graine de blinding ci-dessous est DÉTERMINISTE et dérivée  │
// │ du témoin (comme exigé) afin que, le jour où le moteur supportera le        │
// │ randomized LDE, le prouveur reste reproductible bit-à-bit.                 │
// └──────────────────────────────────────────────────────────────────────────┘
//
// DÉTERMINISME ABSOLU : aucun time, aucun math/rand. La graine de blinding est
// dérivée du témoin par SHAKE256 (poseidon.go), donc reproductible.
package stark

import "golang.org/x/crypto/sha3"

// spZKBlinded indique si le circuit de dépense applique un masquage
// zero-knowledge (randomized LDE). Il vaut FALSE : aujourd'hui, AUCUN masquage
// n'est appliqué. Exposé comme constante de fait pour que les appelants (et les
// tests) puissent ASSERTER honnêtement l'absence de ZK et ne jamais croire à
// tort que la preuve cache le témoin de façon information-théorique.
const spZKBlinded = false

// SpendIsZeroKnowledge renvoie l'état réel du masquage ZK du circuit de dépense.
// FALSE aujourd'hui : la preuve fuit des fonctions linéaires du témoin via les
// ouvertures OOD et de requête (voir le bandeau de ce fichier). Le « ZK » du
// prototype se limite à « témoin non publié ». Fonction exposée pour que la
// couche supérieure (pool blindé) ne se repose JAMAIS sur une confidentialité
// inexistante.
func SpendIsZeroKnowledge() bool { return spZKBlinded }

// spBlindingDomain est l'étiquette de domaine SHAKE256 de la graine de blinding,
// séparée de tout autre usage (transcript, dérivation de paramètres Poseidon).
const spBlindingDomain = "stark/spend/blinding-seed/v1"

// SpendBlindingSeed dérive, de façon DÉTERMINISTE et à partir du TÉMOIN, la
// graine d'aléa de masquage que consommerait un randomized LDE. Elle absorbe
// TOUS les champs secrets du témoin (montants, nk, rho, chemin, note de sortie)
// dans un flux SHAKE256, puis produit un Felt.
//
// IMPORTANT — PORTÉE RÉELLE : cette graine n'est AUJOURD'HUI consommée par
// AUCUN masquage (le moteur n'implémente pas le randomized LDE ; voir le
// bandeau). Elle existe pour DEUX raisons :
//
//  1. Capturer, dès maintenant, l'exigence de déterminisme du futur correctif :
//     l'aléa de blinding DOIT venir du témoin (pas de math/rand), de sorte que le
//     prouveur reste reproductible bit-à-bit. La recette est ainsi figée et
//     auditable.
//  2. Servir de point d'ancrage unique le jour où le moteur sera étendu : le
//     prouveur de dépense dérivera ses q coefficients de blinding par colonne en
//     étendant ce flux (graine || compteur), sans réinventer la dérivation.
//
// Elle NE DOIT PAS être interprétée comme une preuve de confidentialité : tant
// que spZKBlinded == false, la graine est inerte vis-à-vis de la fuite.
func SpendBlindingSeed(w SpendWitness, fee Felt) Felt {
	xof := newXOF(spBlindingDomain)
	absorbFeltXOF(xof, w.InValue)
	absorbDigestXOF(xof, w.InRho)
	absorbDigestXOF(xof, w.Nk)
	for lvl := 0; lvl < spendDepth; lvl++ {
		absorbDigestXOF(xof, w.Path.Siblings[lvl])
		absorbFeltXOF(xof, w.Path.Bits[lvl])
	}
	absorbFeltXOF(xof, w.OutValue)
	absorbDigestXOF(xof, w.OutOwnerTag)
	absorbDigestXOF(xof, w.OutRho)
	absorbFeltXOF(xof, fee)
	return nextFelt(xof)
}

// absorbFeltXOF absorbe un Felt (8 octets big-endian, représentation canonique)
// dans le flux SHAKE256 de dérivation de graine. Déterministe.
func absorbFeltXOF(xof sha3.ShakeHash, x Felt) {
	_, _ = xof.Write(x.Bytes())
}

// absorbDigestXOF absorbe un digest [4]Felt dans le flux SHAKE256. Déterministe.
func absorbDigestXOF(xof sha3.ShakeHash, d [poseidonDigestLen]Felt) {
	for k := 0; k < poseidonDigestLen; k++ {
		absorbFeltXOF(xof, d[k])
	}
}

// ---------------------------------------------------------------------------
// Aides d'ANALYSE de fuite (utilisées par les tests adverses). Elles ne
// modifient RIEN : elles inspectent une AirProof déjà produite pour DÉMONTRER,
// de façon vérifiable, ce qui est révélé et ce qui ne l'est pas.
// ---------------------------------------------------------------------------

// spProofRevealedFelts collecte TOUTES les valeurs de corps qu'une AirProof
// révèle « en clair » dans sa structure (hors hachages Merkle, qui sont des
// engagements opaques) : les ouvertures hors-domaine de chaque colonne en z et
// g·z, H(z), les coefficients finaux FRI, et toutes les valeurs ouvertes aux
// positions de requête (colonnes, composition, DEEP, et — pour FRI — valeur et
// jumeau à chaque couche). C'est l'ensemble EXACT des Felt qu'un observateur de
// la preuve peut lire sans casser un engagement.
//
// Sert aux tests à VÉRIFIER une borne de fuite : aucune de ces valeurs « lisibles
// » ne doit coïncider avec une cellule-témoin BRUTE (montant, bit, nk…), car les
// points d'ouverture (z, g·z, ω^pos) sont distincts des points de trace g^i.
// C'est une garantie FAIBLE (pas de la ZK), explicitement bornée : voir le test.
func spProofRevealedFelts(proof AirProof) []Felt {
	out := make([]Felt, 0, 4096)

	// OOD de chaque colonne + composition.
	out = append(out, proof.OodColZ...)
	out = append(out, proof.OodColGZ...)
	out = append(out, proof.OodHz)

	// Coefficients finaux FRI (couche finale, en clair).
	out = append(out, proof.Fri.FinalCoeffs...)

	// Valeurs ouvertes aux positions de requête (STARK).
	for _, op := range proof.Openings {
		out = append(out, op.ColVals...)
		out = append(out, op.CompVal, op.DeepVal)
	}

	// Valeurs ouvertes par FRI à chaque couche, pour chaque requête.
	for _, q := range proof.Fri.Queries {
		for _, step := range q {
			out = append(out, step.Value, step.Sibling)
		}
	}

	return out
}
