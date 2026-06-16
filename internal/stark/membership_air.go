// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// ÉTAGE 4.1 — Circuit d'APPARTENANCE Merkle Poseidon.
//
// On prouve, en zero-knowledge sur la feuille et le chemin, l'énoncé :
//
//	« Je connais une feuille L ([4]Felt) et un chemin de Merkle (d siblings
//	  + d bits de direction, profondeur d=8) tels que l'application répétée de
//	  la compression Poseidon Hash2 (EXACTEMENT celle de poseidon.go) mène à la
//	  racine PUBLIQUE root ([4]Felt). »
//
// La feuille, les siblings et les bits sont des COLONNES PRIVÉES (témoins) : ils
// ne sont JAMAIS publiés (ni en clair, ni comme valeur publique du transcript,
// ni comme contrainte de bord). Seule `root` est publique (une contrainte de
// bord sur les 4 premières cellules d'état de la DERNIÈRE ligne de sortie).
//
// Le circuit est construit AU-DESSUS du moteur multi-colonnes générique
// (stark_mc.go, ProveAIR/VerifyAIR) et RÉUTILISE TELLE QUELLE l'arithmétisation
// d'UNE permutation Poseidon de poseidon_air_full.go (mêmes couches ARC + S-box
// pleine/partielle + MDS, mêmes constantes/sélecteurs ancrés par bord). On
// chaîne d niveaux : la sortie de la permutation du niveau i (ses 4 premières
// cellules = digest parent) est RÉ-ASSEMBLÉE avec le sibling du niveau i+1 (et
// ordonnée par le bit) pour former l'état d'entrée de la permutation i+1.
//
// ┌──────────────────────────────────────────────────────────────────────────┐
// │ PORTÉE & HONNÊTETÉ — PROTOTYPE RÉDUIT ASSUMÉ.                               │
// │                                                                            │
// │ CE QUI EST PROUVÉ : la connaissance d'une feuille L et d'un chemin         │
// │ (siblings + bits, profondeur FIXE d=8) tels que la chaîne de compressions  │
// │ Poseidon Hash2 — la MÊME fonction que poseidon.go / merkle_poseidon.go —   │
// │ reconstruit la racine publique. Chaque niveau combine                      │
// │   parent = bit==0 ? Hash2(cur, sibling) : Hash2(sibling, cur).             │
// │ Les bits sont contraints binaires (b·(b-1)=0) par une contrainte de        │
// │ transition active sur les lignes de ré-assemblage.                         │
// │                                                                            │
// │ CE QUI EST PRIVÉ (zero-knowledge AU SENS « non publié ») : la feuille, les │
// │ d siblings et les d bits sont des colonnes-témoins. Elles ne figurent dans │
// │ AUCUNE valeur publique. ATTENTION : comme pour les autres étages, la trace │
// │ n'est PAS masquée par des aléas (randomized LDE) ; le « ZK » se limite     │
// │ ici à NE PAS PUBLIER le témoin. Les ouvertures FRI exposent des            │
// │ évaluations LDE de colonnes en quelques points hors du domaine de trace :  │
// │ un masquage zero-knowledge COMPLET reste À FAIRE et À AUDITER.             │
// │                                                                            │
// │ CE QUI EST RÉDUIT / OMIS : profondeur FIXE d=8 (un seul format d'arbre,    │
// │ 256 feuilles) ; UNE feuille / UN chemin par preuve (pas de multi-feuilles, │
// │ pas de transaction blindée entrée/sortie/frais — c'est l'objet d'étages    │
// │ ultérieurs). On ne prouve QUE l'appartenance, pas l'absence de double-     │
// │ dépense ni la conservation de valeur.                                      │
// │                                                                            │
// │ PARAMÈTRES NON AUDITÉS : matrice MDS + constantes de ronde dérivées par    │
// │ NOUS (voir poseidon.go). La résistance aux secondes préimages / collisions │
// │ n'est PAS établie. Ne pas utiliser en consensus / production.              │
// └──────────────────────────────────────────────────────────────────────────┘
//
// ---------------------------------------------------------------------------
// Layout de la trace (multi-colonnes)
// ---------------------------------------------------------------------------
//
// On empile VERTICALEMENT les d permutations Poseidon. Chaque niveau occupe un
// bloc de pfSteps (32) lignes — la même découpe que poseidon_air_full.go (30
// rondes + 2 lignes de service). Hauteur totale n = d · 32 = 256 (puissance de
// 2, requis par le moteur).
//
// Colonnes (W = 32) :
//
//	[0  .. 11]  s0..s11   : état Poseidon AVANT la ronde / opération de la ligne.
//	[12 .. 23]  rc0..rc11 : vecteur de constantes de ronde de la ligne (public).
//	[24]        fsel      : sélecteur « ronde pleine » (1 pleine, 0 partielle).
//	[25]        active    : sélecteur « ronde Poseidon réelle » (transition = un
//	                        tour ARC+S-box+MDS depuis cette ligne).
//	[26 .. 29]  sib0..sib3: digest frère (sibling) du niveau ré-assemblé à la
//	                        transition de CETTE ligne. PRIVÉ (témoin).
//	[30]        rmode     : sélecteur « ré-assemblage » (1 : la transition de
//	                        cette ligne construit l'état d'entrée du niveau
//	                        suivant à partir de l'état courant + sib + bit).
//	[31]        bit       : bit de direction du niveau ré-assemblé. PRIVÉ.
//
// Les colonnes rc/fsel/active/rmode sont PUBLIQUES (elles décrivent la STRUCTURE
// du circuit, identique pour tout le monde) et ancrées par une contrainte de
// bord à CHAQUE ligne (comme dans poseidon_air_full.go). Les colonnes
// sib0..sib3 et bit NE sont PAS ancrées : ce sont les TÉMOINS privés.
//
// Bloc du niveau ℓ (ℓ in [0,d), base = ℓ·32) :
//
//	base+0  .. base+29 : état avant la ronde 0..29 ; active=1, rmode=0.
//	                     rc/fsel = constantes/sélecteur de la ronde correspondante.
//	base+30            : SORTIE de la permutation du niveau ℓ (Hash2 du niveau) ;
//	                     ses 4 premières cellules = digest parent du niveau ℓ.
//	                     - niveaux ℓ<d-1 : active=0, rmode=1 ; la transition
//	                       base+30 -> base+31 RÉ-ASSEMBLE (cur.s[0..3]=digest
//	                       parent, sib=sibling du niveau ℓ+1, bit) en l'état
//	                       d'entrée du niveau ℓ+1.
//	                     - niveau ℓ=d-1 (racine) : active=0, rmode=0 (identité).
//	base+31            : état d'entrée du niveau ℓ+1 (issu du ré-assemblage), ou
//	                     état racine recopié (dernier niveau) ; active=0,rmode=0.
//	                     La transition base+31 -> (base+32 = bloc suivant ligne 0)
//	                     est l'IDENTITÉ, de sorte que la ligne 0 du bloc suivant
//	                     reprend exactement l'état d'entrée ré-assemblé.
//
// ---------------------------------------------------------------------------
// Contrainte de transition (12 résidus d'état + 1 résidu de binarité = 13)
// ---------------------------------------------------------------------------
//
// Pour chaque colonne d'état k (notation : cur = ligne courante, next = suivante) :
//
//	round_k = couche Poseidon (ARC + S-box sélectionnée par fsel + MDS) appliquée
//	          à cur.s     — IDENTIQUE à poseidon_air_full.go.
//	reasm_k = cellule k de l'état d'entrée Hash2 ré-assemblé à partir du digest
//	          courant child = cur.s[0..3], du sibling sib et du bit :
//	            k in [0,4) : (1-bit)·child_k     + bit·sib_k         (gauche)
//	            k in [4,8) : (1-bit)·sib_{k-4}   + bit·child_{k-4}   (droite)
//	            k == 8     : poseidonDomainSep   (capacité, constante)
//	            k == 9     : poseidonRate (=8)   (capacité, constante)
//	            k in {10,11}: 0
//	out_k   = active·round_k + rmode·reasm_k + (1 - active - rmode)·cur.s_k
//	C_k     = next.s_k - out_k                              (12 résidus, k in [0,12))
//
//	active et rmode sont MUTUELLEMENT EXCLUSIFS (jamais tous deux à 1) : la somme
//	active+rmode vaut 0 (identité/pad) ou 1 (un seul mode actif), donc le facteur
//	identité (1-active-rmode) est bien 0 ou 1. Ce sont des colonnes PUBLIQUES
//	ancrées par bord, donc non manipulables.
//
//	13e résidu — BINARITÉ DU BIT : rmode·bit·(bit-1). Sur une ligne de ré-
//	  assemblage (rmode=1) il impose bit·(bit-1)=0, donc bit in {0,1}. Hors ré-
//	  assemblage il est neutralisé (rmode=0).
//
// DEGRÉ : active·fsel·a^7 => 1+1+7 = 9 (domine). rmode·bit·child => 3.
// rmode·bit·(bit-1) => 3. Donc MaxDegree = 9, comme poseidon_air_full.go.
//
// DÉTERMINISME ABSOLU : aucun time, aucun math/rand. Constantes/sélecteurs issus
// de params (poseidon.go) ; tout l'aléa du protocole vient du transcript Fiat-
// Shamir via ProveAIR/VerifyAIR.
package stark

// ---------------------------------------------------------------------------
// Constantes de layout
// ---------------------------------------------------------------------------

const (
	// memDepth est la profondeur FIXE de l'arbre prouvé (nombre de niveaux de
	// compression Hash2). d=8 => arbre de 2^8 = 256 feuilles. Prototype : un seul
	// format d'arbre (voir bandeau de portée).
	memDepth = 8

	// memBlock est le nombre de lignes par niveau (= pfSteps de poseidon_air_full :
	// 30 rondes + ligne de sortie + ligne de service). 32.
	memBlock = pfSteps // 32

	// memSteps est la hauteur de trace : d niveaux × 32 lignes = 256 (puissance
	// de 2, exigée par le moteur multi-colonnes).
	memSteps = memDepth * memBlock // 256

	// Indices de colonnes (la trace réutilise le layout d'état/rc/fsel/active de
	// poseidon_air_full.go, étendu de colonnes de ré-assemblage).
	memStateOff = pfStateOff             // 0   : s0..s11   -> [0,12)
	memRcOff    = pfRcOff                // 12  : rc0..rc11  -> [12,24)
	memFselCol  = pfFselCol              // 24  : fsel
	memActCol   = pfActCol               // 25  : active
	memSibOff   = pfNumCols              // 26  : sib0..sib3 -> [26,30)
	memRmodeCol = memSibOff + poseidonDigestLen // 30 : rmode
	memBitCol   = memRmodeCol + 1        // 31  : bit
	memNumCols  = memBitCol + 1          // W = 32

	// memMaxDegree : degré total max des contraintes (active·fsel·a^7 => 9), comme
	// poseidon_air_full.go.
	memMaxDegree = pfMaxDegree // 9
)

// memOutputRowOf renvoie la ligne portant l'état de SORTIE de la permutation du
// niveau ℓ (digest parent dans ses 4 premières cellules). C'est la 31e ligne du
// bloc (indice 30 dans le bloc), comme pfOutputRow.
func memOutputRowOf(level int) int {
	return level*memBlock + pfOutputRow // ℓ·32 + 30
}

// memRootRow est la ligne portant la RACINE : sortie du dernier niveau.
func memRootRow() int {
	return memOutputRowOf(memDepth - 1) // 7·32 + 30 = 254
}

// ---------------------------------------------------------------------------
// AIR d'appartenance
// ---------------------------------------------------------------------------

// membershipAIR implémente l'interface AIR (stark_mc.go) pour une preuve
// d'appartenance Merkle Poseidon de profondeur memDepth. La SEULE donnée
// publique portée par l'instance est la racine (ancrée en bord). La feuille, les
// siblings et les bits ne figurent PAS dans l'instance : ils n'existent que dans
// la trace-témoin.
type membershipAIR struct {
	root [poseidonDigestLen]Felt // racine PUBLIQUE (sortie du dernier niveau)
}

func (a membershipAIR) NumColumns() int { return memNumCols }
func (a membershipAIR) NumSteps() int   { return memSteps }
func (a membershipAIR) MaxDegree() int  { return memMaxDegree }

// EvalTransition applique, sur la paire de lignes (cur, next), soit une ronde
// Poseidon (active=1), soit un ré-assemblage de niveau (rmode=1), soit l'identité
// (les deux à 0). Renvoie 13 résidus : 12 pour l'état + 1 pour la binarité du
// bit. PURE et déterministe : ne lit les sélecteurs/constantes que dans cur.
func (a membershipAIR) EvalTransition(cur, next []Felt) []Felt {
	fsel := cur[memFselCol]
	active := cur[memActCol]
	rmode := cur[memRmodeCol]
	bit := cur[memBitCol]
	one := One()
	oneMinusFsel := one.Sub(fsel)
	// Facteur identité : 1 - active - rmode (0 ou 1 puisque active/rmode exclusifs).
	idfac := one.Sub(active).Sub(rmode)

	// --- Branche RONDE POSEIDON (identique à poseidon_air_full.go) ---
	// 1) addRoundConstants : arc_k = s_k + rc_k.
	var arc [pfStateCols]Felt
	for k := 0; k < pfStateCols; k++ {
		arc[k] = cur[memStateOff+k].Add(cur[memRcOff+k])
	}
	// 2) S-box sélectionnée : sb_0 = arc_0^7 ; sb_k = fsel·arc_k^7 + (1-fsel)·arc_k.
	var sb [pfStateCols]Felt
	sb[0] = arc[0].Exp(poseidonAlpha)
	for k := 1; k < pfStateCols; k++ {
		ak7 := arc[k].Exp(poseidonAlpha)
		sb[k] = fsel.Mul(ak7).Add(oneMinusFsel.Mul(arc[k]))
	}

	// --- Branche RÉ-ASSEMBLAGE : état d'entrée Hash2 du niveau suivant ---
	// child = digest parent courant = cur.s[0..3].
	var child [poseidonDigestLen]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		child[k] = cur[memStateOff+k]
	}
	oneMinusBit := one.Sub(bit)
	var reasm [pfStateCols]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		sibk := cur[memSibOff+k]
		// gauche (cellules 0..3) : bit==0 -> child, bit==1 -> sibling.
		reasm[k] = oneMinusBit.Mul(child[k]).Add(bit.Mul(sibk))
		// droite (cellules 4..7) : bit==0 -> sibling, bit==1 -> child.
		reasm[poseidonDigestLen+k] = oneMinusBit.Mul(sibk).Add(bit.Mul(child[k]))
	}
	// Capacité : EXACTEMENT l'ensemencement de Hash2 (poseidon.go).
	reasm[poseidonRate] = FromUint64(poseidonDomainSep)   // cellule 8
	reasm[poseidonRate+1] = FromUint64(uint64(poseidonRate)) // cellule 9 = 8
	// cellules 10, 11 déjà à zéro (valeur par défaut du tableau).

	// --- Combinaison des branches + résidus d'état ---
	residues := make([]Felt, pfStateCols+1)
	for k := 0; k < pfStateCols; k++ {
		// MDS de la branche ronde : round_k = Σ_j MDS[k][j]·sb_j.
		acc := Zero()
		row := &params.mds[k]
		for j := 0; j < pfStateCols; j++ {
			acc = acc.Add(row[j].Mul(sb[j]))
		}
		roundK := acc
		// out_k = active·round_k + rmode·reasm_k + (1-active-rmode)·s_k.
		outk := active.Mul(roundK).
			Add(rmode.Mul(reasm[k])).
			Add(idfac.Mul(cur[memStateOff+k]))
		residues[k] = next[memStateOff+k].Sub(outk)
	}

	// 13e résidu : binarité du bit sur les lignes de ré-assemblage.
	// rmode·bit·(bit-1) s'annule ssi (rmode=0) ou (bit in {0,1}).
	residues[pfStateCols] = rmode.Mul(bit).Mul(bit.Sub(one))

	return residues
}

// Boundaries renvoie les contraintes de bord :
//
//   - colonnes de structure (rc0..rc11, fsel, active, rmode) ancrées sur CHAQUE
//     ligne aux valeurs précalculées (PUBLIQUES) ;
//   - capacité de l'état d'entrée du PREMIER niveau (ligne 0, cellules 8..11) =
//     (domainSep, 8, 0, 0) — l'ensemencement Hash2, public ;
//   - racine (ligne memRootRow, 4 premières cellules d'état) = a.root (public).
//
// Les colonnes sib0..sib3 et bit n'ont AUCUNE contrainte de bord : ce sont les
// témoins privés (feuille via l'état initial, siblings, directions). L'état
// d'entrée du premier niveau (cellules 0..7) n'est PAS contraint non plus : il
// porte (selon bit[0]) la feuille privée et le premier sibling.
func (a membershipAIR) Boundaries() []Boundary {
	bs := make([]Boundary, 0, memSteps*(pfStateCols+3)+poseidonDigestLen+poseidonCapacity)

	// Colonnes de structure (rc/fsel/active/rmode), ligne par ligne.
	for row := 0; row < memSteps; row++ {
		rc, fsel, active, rmode := memRowStructure(row)
		for k := 0; k < pfStateCols; k++ {
			bs = append(bs, Boundary{Col: memRcOff + k, Row: row, Value: rc[k]})
		}
		bs = append(bs, Boundary{Col: memFselCol, Row: row, Value: fsel})
		bs = append(bs, Boundary{Col: memActCol, Row: row, Value: active})
		bs = append(bs, Boundary{Col: memRmodeCol, Row: row, Value: rmode})
	}

	// Capacité d'entrée du premier niveau (ligne 0) : ensemencement Hash2 public.
	bs = append(bs, Boundary{Col: memStateOff + poseidonRate, Row: 0, Value: FromUint64(poseidonDomainSep)})
	bs = append(bs, Boundary{Col: memStateOff + poseidonRate + 1, Row: 0, Value: FromUint64(uint64(poseidonRate))})
	bs = append(bs, Boundary{Col: memStateOff + poseidonRate + 2, Row: 0, Value: Zero()})
	bs = append(bs, Boundary{Col: memStateOff + poseidonRate + 3, Row: 0, Value: Zero()})

	// Racine : 4 premières cellules d'état de la ligne de sortie du dernier niveau.
	rootRow := memRootRow()
	for k := 0; k < poseidonDigestLen; k++ {
		bs = append(bs, Boundary{Col: memStateOff + k, Row: rootRow, Value: a.root[k]})
	}

	return bs
}

// memRowStructure renvoie, pour une ligne donnée, les valeurs PUBLIQUES des
// colonnes de structure : constantes de ronde rc, sélecteur plein/partiel fsel,
// sélecteur d'activité active, sélecteur de ré-assemblage rmode.
//
// Découpe par bloc (niveau ℓ, indice local r = row mod 32) :
//
//	r in [0,30)  : ronde réelle r ; rc = params.roundConstants[r] ;
//	               fsel = 1 si ronde pleine ; active = 1 ; rmode = 0.
//	r == 30      : ligne de sortie du niveau ℓ.
//	               - ℓ < d-1 : rmode = 1 (ré-assemblage vers le niveau ℓ+1) ;
//	                 active = 0, fsel = 0, rc = 0.
//	               - ℓ == d-1 (racine) : rmode = 0, active = 0 (identité).
//	r == 31      : ligne de service (identité vers le bloc suivant) ;
//	               active = 0, rmode = 0, fsel = 0, rc = 0.
//
// Le motif plein/partiel des rondes 0..29 suit EXACTEMENT Permute (pfIsFullRound).
func memRowStructure(row int) (rc [pfStateCols]Felt, fsel, active, rmode Felt) {
	level := row / memBlock
	r := row % memBlock

	switch {
	case r < poseidonTotalRounds: // rondes réelles 0..29
		rc = params.roundConstants[r]
		active = One()
		if pfIsFullRound(r) {
			fsel = One()
		}
		// rmode = 0.
	case r == pfOutputRow: // ligne de sortie (indice 30)
		if level < memDepth-1 {
			rmode = One() // ré-assemblage vers le niveau suivant
		}
		// dernier niveau : tout à zéro (identité) -> racine recopiée.
	default: // r == 31 : ligne de service (identité)
		// tout à zéro.
	}
	return rc, fsel, active, rmode
}

// ---------------------------------------------------------------------------
// Construction de la trace
// ---------------------------------------------------------------------------

// MembershipPath décrit un chemin d'authentification Merkle Poseidon pour une
// preuve d'appartenance : memDepth siblings (du bas vers le haut, comme
// PoseidonOpen) et memDepth bits de direction (Bits[i]==0 : nœud courant à
// gauche, sibling à droite ; Bits[i]==1 : l'inverse — MÊME convention que
// PoseidonVerifyPath où le bit est le LSB de l'indice au niveau i).
//
// Les Bits DOIVENT valoir 0 ou 1 ; toute autre valeur est rejetée à la
// construction de la trace (et, en preuve, par la contrainte de binarité).
type MembershipPath struct {
	Siblings [memDepth][poseidonDigestLen]Felt
	Bits     [memDepth]Felt
}

// MembershipDepth renvoie la profondeur (fixe) d'arbre prouvée par ce circuit.
// Exposé pour les appelants (taille attendue du chemin, du nombre de feuilles).
func MembershipDepth() int { return memDepth }

// memReassemble construit l'état d'entrée Hash2 (12 Felt) du niveau, à partir du
// digest courant `child`, du `sibling` et du `bit` de direction. C'est la
// version NATIVE (en clair) de la branche reasm de EvalTransition : elle DOIT en
// être l'exacte réplique, sans quoi trace et contrainte divergent.
//
//	bit==0 : left=child, right=sibling   (Hash2(child, sibling))
//	bit==1 : left=sibling, right=child   (Hash2(sibling, child))
//
// La capacité reçoit l'ensemencement de Hash2 (poseidon.go).
func memReassemble(child, sibling [poseidonDigestLen]Felt, bit Felt) [pfStateCols]Felt {
	var st [pfStateCols]Felt
	var left, right [poseidonDigestLen]Felt
	if bit.IsZero() {
		left, right = child, sibling
	} else {
		left, right = sibling, child
	}
	for k := 0; k < poseidonDigestLen; k++ {
		st[k] = left[k]
		st[poseidonDigestLen+k] = right[k]
	}
	st[poseidonRate] = FromUint64(poseidonDomainSep)
	st[poseidonRate+1] = FromUint64(uint64(poseidonRate))
	return st
}

// buildMembershipTrace construit la trace COMPLÈTE de la chaîne de compressions
// Hash2 pour la feuille `leaf` et le chemin `path`. Elle calcule l'état réel
// ronde par ronde de CHAQUE permutation (via pfApplyRound, comme
// poseidon_air_full.go), et le ré-assemblage entre niveaux (via memReassemble).
//
// Renvoie la trace et la racine reconstruite (sortie du dernier niveau, 4
// premières cellules). La racine renvoyée DOIT coïncider avec
// PoseidonVerifyPath/racine de merkle_poseidon.go pour le même (leaf, path) —
// c'est l'IMPÉRATIF de cohérence (testé).
//
// Panique si un bit n'est pas binaire (erreur d'appelant : un témoin non binaire
// n'a pas de sens et serait de toute façon rejeté par la contrainte).
func buildMembershipTrace(leaf [poseidonDigestLen]Felt, path MembershipPath) (trace [][]Felt, root [poseidonDigestLen]Felt) {
	trace = make([][]Felt, memSteps)

	// État d'entrée du premier niveau : ré-assemblage de la feuille et du premier
	// sibling selon bit[0]. (La feuille joue le rôle de `child` au niveau 0.)
	for i := 0; i < memDepth; i++ {
		if !(path.Bits[i].IsZero() || path.Bits[i].Equal(One())) {
			panic("stark: buildMembershipTrace: bit de direction non binaire")
		}
	}

	// `child` est le digest courant qui entre dans le niveau ℓ ; au niveau 0 c'est
	// la feuille.
	child := leaf

	for level := 0; level < memDepth; level++ {
		base := level * memBlock

		// État d'entrée Hash2 du niveau : ré-assemblage (child, sibling, bit).
		s := memReassemble(child, path.Siblings[level], path.Bits[level])

		// Remplissage des lignes du bloc.
		for r := 0; r < memBlock; r++ {
			row := base + r
			rc, fsel, active, rmode := memRowStructure(row)
			line := make([]Felt, memNumCols)
			for k := 0; k < pfStateCols; k++ {
				line[memStateOff+k] = s[k]
				line[memRcOff+k] = rc[k]
			}
			line[memFselCol] = fsel
			line[memActCol] = active
			line[memRmodeCol] = rmode

			// Colonnes-témoins sib/bit : portées par la ligne de SORTIE (r==30) du
			// niveau, car c'est la ligne dont la transition ré-assemble vers le
			// niveau suivant. On y inscrit le sibling et le bit du niveau SUIVANT.
			// Les autres lignes laissent sib/bit à zéro (non contraints).
			if r == pfOutputRow && level < memDepth-1 {
				nxt := level + 1
				for k := 0; k < poseidonDigestLen; k++ {
					line[memSibOff+k] = path.Siblings[nxt][k]
				}
				line[memBitCol] = path.Bits[nxt]
			}

			trace[row] = line

			// Avance de l'état d'une ligne à l'autre À L'INTÉRIEUR du bloc.
			if r < pfOutputRow {
				// Ronde réelle r : applique le tour Poseidon.
				s = pfApplyRound(s, rc, !fsel.IsZero())
			} else if r == pfOutputRow {
				// Ligne de sortie -> ligne 31 : ré-assemblage (non-dernier niveau) ou
				// identité (dernier niveau). On prépare l'état de la ligne 31, qui sera
				// l'état d'entrée du niveau suivant (ou la racine recopiée).
				if level < memDepth-1 {
					parent := memDigestOf(s)
					s = memReassemble(parent, path.Siblings[level+1], path.Bits[level+1])
				}
				// dernier niveau : s inchangé (identité) -> ligne 31 = état de sortie.
			}
			// r == 31 : transition identité vers la ligne 0 du bloc suivant ; s est
			// déjà l'état d'entrée du niveau suivant, qui sera réécrit par
			// memReassemble en tête de boucle (valeur identique). On NE touche pas s.
		}

		// `child` du niveau suivant = digest parent (sortie de la permutation ℓ).
		// Recalcul propre à partir de l'état de sortie réel : on relit la sortie de
		// la permutation via une recomposition. Mais on l'a déjà : c'est
		// digestOf(état de sortie du bloc). Pour rester strictement aligné sur la
		// trace, on recompute la sortie de la permutation du niveau.
		child = memLevelOutput(level, leaf, path)
	}

	root = child
	return trace, root
}

// memDigestOf extrait les 4 premières cellules d'un état [12]Felt (digest Hash2).
func memDigestOf(s [pfStateCols]Felt) [poseidonDigestLen]Felt {
	var d [poseidonDigestLen]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		d[k] = s[k]
	}
	return d
}

// memLevelOutput recalcule le digest parent (sortie Hash2) du niveau `level`
// pour (leaf, path), de façon NATIVE et indépendante de la trace. Sert d'ancre de
// cohérence interne (le `child` du niveau suivant). Le niveau 0 part de la
// feuille ; les niveaux suivants partent du digest parent du niveau précédent.
func memLevelOutput(level int, leaf [poseidonDigestLen]Felt, path MembershipPath) [poseidonDigestLen]Felt {
	cur := leaf
	for lvl := 0; lvl <= level; lvl++ {
		if path.Bits[lvl].IsZero() {
			cur = Hash2(cur, path.Siblings[lvl])
		} else {
			cur = Hash2(path.Siblings[lvl], cur)
		}
	}
	return cur
}

// ---------------------------------------------------------------------------
// Prouveur / Vérifieur de haut niveau
// ---------------------------------------------------------------------------

// ProveMembership construit une preuve STARK d'appartenance : elle atteste la
// connaissance de la feuille `leaf` et du chemin `path` (siblings + bits,
// profondeur memDepth) tels que la chaîne de compressions Poseidon Hash2 mène à
// une racine `root`, SANS révéler feuille/siblings/bits.
//
// Renvoie (root, proof). La `root` renvoyée est publique ; elle est garantie
// égale à PoseidonVerifyPath/racine de merkle_poseidon.go pour le même
// (leaf, path) (voir buildMembershipTrace et le test de cohérence). Le témoin
// (leaf, siblings, bits) ne figure dans AUCUNE valeur publique de la preuve.
//
// Panique (erreur d'appelant) si un bit n'est pas binaire.
func ProveMembership(leaf [poseidonDigestLen]Felt, path MembershipPath) ([poseidonDigestLen]Felt, AirProof) {
	trace, root := buildMembershipTrace(leaf, path)
	air := membershipAIR{root: root}
	public := memPublicInputs(root)
	proof := ProveAIR(air, trace, public...)
	return root, proof
}

// VerifyMembership vérifie une preuve produite par ProveMembership pour la racine
// publique `root`. Renvoie true ssi la preuve atteste l'existence d'une feuille
// et d'un chemin (de profondeur memDepth, bits binaires) dont la chaîne Hash2
// reconstruit `root`.
//
// `root` est l'énoncé public ; elle DOIT coïncider avec la racine prouvée, sans
// quoi le bord racine est violé / le transcript diverge => rejet. Ne panique
// JAMAIS sur preuve falsifiée : rejet propre (false).
func VerifyMembership(root [poseidonDigestLen]Felt, proof AirProof) bool {
	air := membershipAIR{root: root}
	public := memPublicInputs(root)
	return VerifyAIR(air, proof, public...)
}

// memPublicInputs assemble les valeurs publiques absorbées dans le transcript :
// la racine (4 Felt) UNIQUEMENT. Le témoin (feuille, siblings, bits) n'y figure
// pas (zero-knowledge au sens « non publié »). Les colonnes de structure sont
// déjà absorbées via les contraintes de bord par ProveAIR/VerifyAIR.
func memPublicInputs(root [poseidonDigestLen]Felt) []Felt {
	public := make([]Felt, 0, poseidonDigestLen)
	public = append(public, root[:]...)
	return public
}
