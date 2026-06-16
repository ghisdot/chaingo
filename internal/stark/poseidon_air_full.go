// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// AIR COMPLET de la permutation Poseidon (t=12, R_F=8, R_P=22, 30 rondes),
// arithmétisée sur le moteur STARK MULTI-COLONNES générique (stark_mc.go,
// ProveAIR/VerifyAIR). C'est la version COMPLÈTE promise : à la différence de
// poseidon_air.go (S-box x^7 SEULE, mono-colonne), on prouve ici l'EXÉCUTION
// ENTIÈRE d'une permutation Poseidon, couche par couche :
//
//   - addRoundConstants (ARC) : ajout du vecteur de constantes de la ronde ;
//   - S-box x^7 : sur les 12 cellules (ronde PLEINE) ou la seule cellule 0
//     (ronde PARTIELLE), sélectionnée par un sélecteur précalculé par ligne ;
//   - couche MDS : combinaison linéaire des 12 colonnes par la matrice de
//     poseidon.go.
//
// La permutation prouvée est EXACTEMENT Permute() de poseidon.go : le test de
// cohérence (poseidon_air_full_test.go) recoupe le digest prouvé avec celui de
// la permutation native. Si les deux divergent, l'AIR ne calcule pas Poseidon
// et le test échoue — c'est l'IMPÉRATIF de correction.
//
// ┌──────────────────────────────────────────────────────────────────────────┐
// │ PORTÉE & HONNÊTETÉ.                                                         │
// │                                                                            │
// │ CE QUI EST PROUVÉ : l'application correcte des 30 rondes de Permute        │
// │ (ARC + S-box pleine/partielle + MDS), de l'état d'entrée (ligne 0, public  │
// │ dans cette variante) au digest de sortie (4 premières cellules, public).   │
// │ Les constantes de ronde, la matrice MDS et le motif plein/partiel sont     │
// │ ancrés DANS LA TRACE par des contraintes de bord sur des colonnes de       │
// │ sélecteurs/constantes précalculées : un prouveur ne peut PAS substituer    │
// │ d'autres constantes (chaque colonne de constante est entièrement fixée par │
// │ ses n contraintes de bord, donc déterminée de façon unique).               │
// │                                                                            │
// │ CE QUI N'EST PAS « ZERO-KNOWLEDGE » ICI : l'état d'entrée (le préimage de  │
// │ la permutation) est PUBLIC dans cette variante (contrainte de bord sur la  │
// │ ligne 0). On prouve la CORRECTION du calcul, pas la connaissance secrète   │
// │ d'un préimage. Le masquage zero-knowledge (randomized LDE) reste à faire   │
// │ et À AUDITER.                                                              │
// │                                                                            │
// │ PARAMÈTRES NON AUDITÉS : la matrice MDS et les constantes de ronde sont    │
// │ dérivées par NOUS (voir poseidon.go), PAS les constantes officielles d'un  │
// │ Poseidon publié. Ne pas utiliser en consensus / production.                │
// └──────────────────────────────────────────────────────────────────────────┘
//
// ---------------------------------------------------------------------------
// Layout de la trace (multi-colonnes)
// ---------------------------------------------------------------------------
//
// Le moteur multi-colonnes applique EvalTransition(cur, next) à TOUTE paire de
// lignes consécutives, sans connaître l'indice de ligne. Or chaque ronde de
// Poseidon a des constantes DIFFÉRENTES et un motif S-box (plein/partiel)
// DIFFÉRENT. On ne peut donc pas « lire params selon la ronde » dans
// EvalTransition. La solution standard : porter les constantes et les
// sélecteurs DANS la trace, sous forme de colonnes précalculées, ancrées par
// des contraintes de bord à chaque ligne (elles sont PUBLIQUES).
//
// Colonnes (W = 26) :
//
//	[0  .. 11]  s0..s11   : état Poseidon AVANT la ronde de la ligne courante.
//	[12 .. 23]  rc0..rc11 : vecteur de constantes de ronde de la ligne courante.
//	[24]        fsel      : sélecteur « ronde pleine » (1 pleine, 0 partielle).
//	[25]        active    : sélecteur « ronde réelle » (1 rondes 0..29, 0 pad).
//
// Lignes (n = 32, puissance de 2 ; 30 rondes + 1 ligne d'état final + 1 pad) :
//
//	ligne i (i in [0,30))  : état avant la ronde i ; rc/fsel/active de la ronde i.
//	ligne 30               : état APRÈS la ronde 29 = sortie de Permute ; ses
//	                         colonnes rc/fsel/active valent celles d'une ligne de
//	                         padding (rc=0, fsel=0, active=0) car aucune ronde ne
//	                         part de la ligne 30 vers la 31.
//	ligne 31               : copie identité de la ligne 30 (padding) ; rc/fsel/
//	                         active = padding.
//
// Motif des rondes (cf. Permute de poseidon.go) :
//
//	rondes 0..3   : PLEINES   (fsel=1, active=1)
//	rondes 4..25  : PARTIELLES(fsel=0, active=1)   (22 rondes)
//	rondes 26..29 : PLEINES   (fsel=1, active=1)
//	lignes 30,31  : PADDING   (fsel=0, active=0)
//
// ---------------------------------------------------------------------------
// Contrainte de transition (par colonne d'état k, ligne courante = ronde r)
// ---------------------------------------------------------------------------
//
//	a_k   = s_k + rc_k                                   (addRoundConstants)
//	sb_0  = a_0^7                                        (cellule 0 toujours S-boxée)
//	sb_k  = fsel·a_k^7 + (1 - fsel)·a_k     (k>=1)       (pleine: ^7 ; partielle: a_k)
//	round_k = Σ_j MDS[k][j]·sb_j                         (couche MDS)
//	out_k = active·round_k + (1 - active)·s_k            (padding = identité)
//	C_k   = next.s_k - out_k                             (doit s'annuler)
//
// DEGRÉ : a_k^7 est de degré 7 ; fsel·a_k^7 monte à 8 ; round_k (MDS linéaire)
// reste à 8 ; active·round_k monte à 9. Donc MaxDegree = 9. Le moteur
// dimensionne bigN = mcBlowup · nextPow2(9·n).
//
// Les colonnes rc/fsel/active n'ont PAS de contrainte de transition propre
// (leur valeur est libre d'une ligne à l'autre) : elles sont entièrement fixées
// par les contraintes de BORD (une par ligne), ce qui les rend non
// manipulables. EvalTransition renvoie donc exactement 12 résidus (un par
// colonne d'état).
//
// DÉTERMINISME ABSOLU : aucun time, aucun math/rand. Constantes et sélecteurs
// proviennent de params (poseidon.go, dérivé par SHAKE256) ; tout l'aléa du
// protocole vient du transcript Fiat-Shamir via ProveAIR/VerifyAIR.
package stark

// ---------------------------------------------------------------------------
// Constantes de layout
// ---------------------------------------------------------------------------

const (
	// pfSteps est la hauteur de trace (puissance de 2). 30 rondes + 1 ligne d'état
	// final + 1 ligne de padding => 32.
	pfSteps = 32

	// pfStateCols est le nombre de colonnes d'état (= largeur Poseidon t = 12).
	pfStateCols = poseidonWidth // 12

	// Indices de blocs de colonnes dans la trace.
	pfStateOff = 0                        // s0..s11   : [0,12)
	pfRcOff    = pfStateOff + pfStateCols // rc0..rc11 : [12,24)
	pfFselCol  = pfRcOff + pfStateCols    // fsel      : 24
	pfActCol   = pfFselCol + 1            // active    : 25
	pfNumCols  = pfActCol + 1             // W = 26

	// pfMaxDegree est le degré total maximal des contraintes de transition
	// (active·fsel·a^7 => 1+1+7 = 9). Voir l'analyse de degré en tête.
	pfMaxDegree = 9
)

// ---------------------------------------------------------------------------
// AIR de la permutation Poseidon complète
// ---------------------------------------------------------------------------

// poseidonFullAIR implémente l'interface AIR (stark_mc.go) pour UNE exécution de
// la permutation Poseidon. L'instance porte l'état d'entrée (public dans cette
// variante) et le digest de sortie (public), tous deux ancrés par des
// contraintes de bord. Les colonnes de constantes/sélecteurs sont ancrées par
// des bords précalculés sur CHAQUE ligne.
type poseidonFullAIR struct {
	input  [pfStateCols]Felt       // état d'entrée (ligne 0)
	digest [poseidonDigestLen]Felt // 4 premières cellules de la sortie (ligne 30)
}

func (a poseidonFullAIR) NumColumns() int { return pfNumCols }
func (a poseidonFullAIR) NumSteps() int   { return pfSteps }
func (a poseidonFullAIR) MaxDegree() int  { return pfMaxDegree }

// EvalTransition applique la contrainte d'UNE ronde Poseidon à la paire de
// lignes (cur, next). Renvoie 12 résidus (un par colonne d'état) qui s'annulent
// ssi next.s = sortie correcte de la ronde décrite par cur (ARC + S-box
// sélectionnée + MDS), ou next.s = cur.s sur les lignes de padding (active=0).
//
// PURE et déterministe : ne dépend que de cur/next. Les constantes/sélecteurs
// sont lus DANS cur (colonnes rc/fsel/active), jamais dans params : c'est ce qui
// permet à la même fonction d'exprimer 30 rondes hétérogènes.
func (a poseidonFullAIR) EvalTransition(cur, next []Felt) []Felt {
	fsel := cur[pfFselCol]
	active := cur[pfActCol]
	one := One()
	oneMinusFsel := one.Sub(fsel)
	oneMinusActive := one.Sub(active)

	// 1) addRoundConstants : arc_k = s_k + rc_k.
	var arc [pfStateCols]Felt
	for k := 0; k < pfStateCols; k++ {
		arc[k] = cur[pfStateOff+k].Add(cur[pfRcOff+k])
	}

	// 2) S-box sélectionnée : sb_0 = arc_0^7 (toujours) ; pour k>=1,
	//    sb_k = fsel·arc_k^7 + (1-fsel)·arc_k.
	var sb [pfStateCols]Felt
	sb[0] = arc[0].Exp(poseidonAlpha)
	for k := 1; k < pfStateCols; k++ {
		ak7 := arc[k].Exp(poseidonAlpha)
		sb[k] = fsel.Mul(ak7).Add(oneMinusFsel.Mul(arc[k]))
	}

	// 3) couche MDS : round_k = Σ_j MDS[k][j]·sb_j.
	residues := make([]Felt, pfStateCols)
	for k := 0; k < pfStateCols; k++ {
		acc := Zero()
		row := &params.mds[k]
		for j := 0; j < pfStateCols; j++ {
			acc = acc.Add(row[j].Mul(sb[j]))
		}
		// 4) Sélecteur d'activité : out_k = active·round_k + (1-active)·s_k.
		outk := active.Mul(acc).Add(oneMinusActive.Mul(cur[pfStateOff+k]))
		// 5) Résidu de transition : next.s_k - out_k.
		residues[k] = next[pfStateOff+k].Sub(outk)
	}
	return residues
}

// Boundaries renvoie toutes les contraintes de bord :
//
//   - état d'entrée (ligne 0, 12 cellules) = a.input (public) ;
//   - colonnes de constantes/sélecteurs (rc0..rc11, fsel, active) ancrées sur
//     CHAQUE ligne aux valeurs précalculées (publiques) — 13 par ligne ;
//   - digest (ligne 30, 4 premières cellules d'état) = a.digest (public).
//
// L'ancrage des constantes/sélecteurs sur toutes les lignes rend ces colonnes
// non manipulables : un polynôme de degré < n=32 est uniquement déterminé par
// ses 32 valeurs, donc le prouveur ne peut substituer d'autres constantes sans
// violer un bord (et faire échouer la vérification).
func (a poseidonFullAIR) Boundaries() []Boundary {
	bs := make([]Boundary, 0, pfStateCols+pfSteps*(pfStateCols+2)+poseidonDigestLen)

	// État d'entrée : ligne 0.
	for k := 0; k < pfStateCols; k++ {
		bs = append(bs, Boundary{Col: pfStateOff + k, Row: 0, Value: a.input[k]})
	}

	// Constantes de ronde + sélecteurs, ligne par ligne.
	for row := 0; row < pfSteps; row++ {
		rc, fsel, active := pfRowConstants(row)
		for k := 0; k < pfStateCols; k++ {
			bs = append(bs, Boundary{Col: pfRcOff + k, Row: row, Value: rc[k]})
		}
		bs = append(bs, Boundary{Col: pfFselCol, Row: row, Value: fsel})
		bs = append(bs, Boundary{Col: pfActCol, Row: row, Value: active})
	}

	// Digest : 4 premières cellules d'état de la ligne 30 (sortie de Permute).
	for k := 0; k < poseidonDigestLen; k++ {
		bs = append(bs, Boundary{Col: pfStateOff + k, Row: pfOutputRow, Value: a.digest[k]})
	}

	return bs
}

// pfOutputRow est la ligne portant l'état de sortie de la permutation (après la
// 30e ronde, indice 29). C'est la 31e ligne (indice 30).
const pfOutputRow = poseidonTotalRounds // 30

// pfRowConstants renvoie, pour une ligne donnée, le vecteur de constantes de
// ronde, le sélecteur plein/partiel (fsel) et le sélecteur d'activité (active).
//
//	lignes 0..29 (rondes réelles) : rc = params.roundConstants[row] ;
//	                                fsel = 1 si la ronde est pleine, 0 sinon ;
//	                                active = 1.
//	lignes >= 30 (état final + pad) : rc = 0, fsel = 0, active = 0 (identité).
//
// Le motif plein/partiel suit EXACTEMENT Permute : 4 pleines, 22 partielles,
// 4 pleines.
func pfRowConstants(row int) (rc [pfStateCols]Felt, fsel Felt, active Felt) {
	if row >= poseidonTotalRounds {
		// Lignes de padding (état final + identité) : tout à zéro.
		return rc, Zero(), Zero()
	}
	rc = params.roundConstants[row]
	active = One()
	if pfIsFullRound(row) {
		fsel = One()
	} else {
		fsel = Zero()
	}
	return rc, fsel, active
}

// pfIsFullRound indique si la ronde d'indice r est PLEINE (S-box sur les 12
// cellules) plutôt que PARTIELLE (S-box sur la cellule 0 seule). Motif de
// Permute : les R_F/2 = 4 premières et les 4 dernières rondes sont pleines, les
// R_P = 22 du milieu sont partielles.
func pfIsFullRound(r int) bool {
	const halfFull = poseidonFullRounds / 2 // 4
	return r < halfFull || r >= halfFull+poseidonPartialRounds
}

// ---------------------------------------------------------------------------
// Construction de la trace
// ---------------------------------------------------------------------------

// buildPoseidonFullTrace construit la trace COMPLÈTE de la permutation Poseidon
// appliquée à `input`. Elle calcule l'état réel ronde par ronde (via les mêmes
// opérations que Permute) et remplit, pour chaque ligne, les colonnes d'état,
// de constantes et de sélecteurs. La trace résultante satisfait par construction
// la contrainte de transition de EvalTransition.
//
// Renvoie aussi la sortie complète (12 Felt) pour que l'appelant en extraie le
// digest et puisse la recouper avec Permute.
func buildPoseidonFullTrace(input [pfStateCols]Felt) (trace [][]Felt, output [pfStateCols]Felt) {
	trace = make([][]Felt, pfSteps)

	// État courant, démarré à l'entrée.
	s := input

	for row := 0; row < pfSteps; row++ {
		rc, fsel, active := pfRowConstants(row)

		// Remplissage de la ligne `row` : état AVANT la ronde + constantes/sélecteurs.
		line := make([]Felt, pfNumCols)
		for k := 0; k < pfStateCols; k++ {
			line[pfStateOff+k] = s[k]
			line[pfRcOff+k] = rc[k]
		}
		line[pfFselCol] = fsel
		line[pfActCol] = active
		trace[row] = line

		// Calcul de l'état de la ligne suivante = sortie de la ronde `row`.
		// Sur les lignes de padding (active=0), c'est l'identité (s inchangé) —
		// cohérent avec out_k = active·round_k + (1-active)·s_k.
		if row+1 < pfSteps {
			if active.IsZero() {
				// Identité : la ligne suivante reprend le même état.
				// (s inchangé.)
			} else {
				s = pfApplyRound(s, rc, !fsel.IsZero())
			}
		}
	}

	output = s
	return trace, output
}

// pfApplyRound applique UNE ronde Poseidon à l'état : ARC (avec le vecteur rc
// fourni) puis S-box (pleine si `full`, sinon partielle sur la cellule 0) puis
// MDS. C'est la réplique exacte d'un tour de Permute, paramétrée par rc/full,
// afin que la trace coïncide avec la sémantique de EvalTransition.
func pfApplyRound(s [pfStateCols]Felt, rc [pfStateCols]Felt, full bool) [pfStateCols]Felt {
	// addRoundConstants.
	var a [pfStateCols]Felt
	for k := 0; k < pfStateCols; k++ {
		a[k] = s[k].Add(rc[k])
	}
	// S-box.
	var sb [pfStateCols]Felt
	sb[0] = a[0].Exp(poseidonAlpha)
	for k := 1; k < pfStateCols; k++ {
		if full {
			sb[k] = a[k].Exp(poseidonAlpha)
		} else {
			sb[k] = a[k]
		}
	}
	// MDS.
	var out [pfStateCols]Felt
	for k := 0; k < pfStateCols; k++ {
		acc := Zero()
		row := &params.mds[k]
		for j := 0; j < pfStateCols; j++ {
			acc = acc.Add(row[j].Mul(sb[j]))
		}
		out[k] = acc
	}
	return out
}

// ---------------------------------------------------------------------------
// Prouveur / Vérifieur de haut niveau
// ---------------------------------------------------------------------------

// ProveHashFull construit une preuve STARK que le digest renvoyé est bien le
// résultat de la permutation Poseidon native appliquée à preimageState :
//
//	digest = Permute(preimageState)[:4]
//
// L'état d'entrée est PUBLIC dans cette variante (ancré en bord ligne 0) ; on
// prouve la CORRECTION du calcul complet (ARC + S-box pleine/partielle + MDS sur
// 30 rondes), pas la connaissance secrète d'un préimage.
//
// Renvoie (digest, proof). Les valeurs publiques absorbées par ProveAIR sont :
// l'état d'entrée (12 Felt) puis le digest (4 Felt). Le vérifieur DOIT rejouer
// exactement ces valeurs (voir VerifyHashFull).
func ProveHashFull(preimageState [pfStateCols]Felt) ([poseidonDigestLen]Felt, AirProof) {
	trace, output := buildPoseidonFullTrace(preimageState)

	var digest [poseidonDigestLen]Felt
	copy(digest[:], output[:poseidonDigestLen])

	air := poseidonFullAIR{input: preimageState, digest: digest}
	public := pfPublicInputs(preimageState, digest)
	proof := ProveAIR(air, trace, public...)
	return digest, proof
}

// VerifyHashFull vérifie une preuve produite par ProveHashFull pour le digest
// public attendu et l'état d'entrée public. Renvoie true ssi la preuve atteste
// l'exécution correcte de la permutation Poseidon menant à ce digest.
//
// preimageState est l'énoncé public (état d'entrée) ; il DOIT coïncider avec
// celui prouvé, sans quoi le transcript diverge / un bord est violé => rejet.
// Ne panique JAMAIS sur preuve falsifiée : rejet propre (false).
func VerifyHashFull(preimageState [pfStateCols]Felt, digest [poseidonDigestLen]Felt, proof AirProof) bool {
	air := poseidonFullAIR{input: preimageState, digest: digest}
	public := pfPublicInputs(preimageState, digest)
	return VerifyAIR(air, proof, public...)
}

// pfPublicInputs assemble le vecteur de valeurs publiques absorbées dans le
// transcript : l'état d'entrée (12 Felt) suivi du digest (4 Felt). L'ordre est
// FIGÉ et partagé par prouveur et vérifieur. (Les constantes/sélecteurs ne sont
// pas répétés ici : ils sont déjà absorbés via les contraintes de bord par
// ProveAIR/VerifyAIR.)
func pfPublicInputs(input [pfStateCols]Felt, digest [poseidonDigestLen]Felt) []Felt {
	public := make([]Felt, 0, pfStateCols+poseidonDigestLen)
	public = append(public, input[:]...)
	public = append(public, digest[:]...)
	return public
}
