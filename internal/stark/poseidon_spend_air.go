// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// ÉTAGE 4.2 — AIR du circuit de DÉPENSE blindée (1 entrée / 1 sortie + frais).
// Voir poseidon_spend.go pour l'énoncé, le bandeau de portée et les définitions
// natives de la note Poseidon. Ce fichier porte l'arithmétisation (interface AIR
// du moteur multi-colonnes) et le prouveur/vérifieur de haut niveau.
//
// L'AIR empile (4 + spendDepth) blocs Poseidon de 32 lignes. Chaque bloc est UNE
// permutation arithmétisée EXACTEMENT comme poseidon_air_full.go (réutilisation
// de pfApplyRound / pfIsFullRound / params). Le glue inter-bloc (ligne de sortie
// d'un bloc) construit l'état d'entrée du bloc suivant par une SOMME pondérée de
// modes mutuellement exclusifs (voir spendRowStructure). Deux registres-témoins
// (regNk = nk, regCm = inCm) et un registre de valeur (regVal = inValue) sont
// portés CONSTANTS entre leur chargement et leur relecture, résolvant les seuls
// couplages non-locaux (nf et l'appartenance ont besoin de inCm ; nf a besoin de
// nk ; la conservation relie inValue et outValue sur deux lignes distinctes).
//
// DÉTERMINISME ABSOLU : aucun time, aucun math/rand.
package stark

// ---------------------------------------------------------------------------
// Constantes de layout
// ---------------------------------------------------------------------------

const (
	// spendDepth est la profondeur FIXE de l'arbre d'appartenance prouvé par le
	// circuit de dépense. d=4 => 2^4 = 16 feuilles. Réduit (vs memDepth=8) pour
	// garder le prouveur tractable (voir bandeau de portée). C'est un format unique.
	spendDepth = 4

	// spBlock = lignes par bloc Poseidon (= pfSteps : 30 rondes + sortie + service).
	spBlock = pfSteps // 32

	// Nombre de blocs : ownerTag(1) + inCm(1) + nf(1) + appartenance(spendDepth) +
	// outCm(1) = spendDepth + 4. Pour d=4 : 8 blocs.
	spNumBlocks = spendDepth + 4 // 8

	// spSteps = hauteur de trace = spNumBlocks·32 = 256 (puissance de 2).
	spSteps = spNumBlocks * spBlock // 256

	// --- Colonnes ---
	// Bloc Poseidon (réutilise le layout de poseidon_air_full.go).
	spStateOff = pfStateOff             // 0   : s0..s11   -> [0,12)
	spRcOff    = pfRcOff                // 12  : rc0..rc11  -> [12,24)
	spFselCol  = pfFselCol              // 24  : fsel
	spActCol   = pfActCol               // 25  : active
	// Témoins de ré-assemblage / commitment.
	spSibOff   = pfNumCols              // 26  : sib0..sib3 -> [26,30)
	spBitCol   = spSibOff + poseidonDigestLen // 30 : bit (direction Merkle)
	// Registres portés constants (témoins).
	spRegNkOff = spBitCol + 1                       // 31 : regNk0..3 -> [31,35)
	spRegCmOff = spRegNkOff + poseidonDigestLen     // 35 : regCm0..3 -> [35,39)
	spRegValCol = spRegCmOff + poseidonDigestLen    // 39 : regVal (inValue porté)
	// Témoins de commitment (tag/rho/value).
	spWTagOff = spRegValCol + 1                     // 40 : wTag0..3 -> [40,44)
	spWRhoOff = spWTagOff + poseidonDigestLen       // 44 : wRho0..3 -> [44,48)
	spWValCol = spWRhoOff + poseidonDigestLen       // 48 : wVal (valeur du commitment de la ligne)
	// Colonne PUBLIQUE de frais (ancrée = fee à la ligne outCm, 0 ailleurs).
	spFeeCol = spWValCol + 1                        // 49 : fee (public)
	// Sélecteurs de mode de glue (PUBLICS).
	spMCommitS  = spFeeCol + 1   // 50 : commit, tag = état courant (inCm)
	spMHash2    = spMCommitS + 1 // 51 : Hash2(regNk, état courant) (nf)
	spMReasmReg = spMHash2 + 1   // 52 : ré-assemblage child = regCm (appartenance L0)
	spMReasm    = spMReasmReg + 1 // 53 : ré-assemblage child = état courant (appartenance Li>0)
	spMCommitW  = spMReasm + 1   // 54 : commit, tag = wTag témoin (outCm)
	// Sélecteurs de registres (PUBLICS).
	spLoadNk  = spMCommitW + 1 // 55 : charge regNk := état courant (ligne 0)
	spHoldNk  = spLoadNk + 1   // 56 : tient regNk constant
	spLoadCm  = spHoldNk + 1   // 57 : charge regCm := état courant (sortie inCm)
	spHoldCm  = spLoadCm + 1   // 58 : tient regCm constant
	spLoadVal = spHoldCm + 1   // 59 : charge regVal := wVal (ligne commit inCm)
	spHoldVal = spLoadVal + 1  // 60 : tient regVal constant
	spNumCols = spHoldVal + 1  // W = 61

	// Degré max : la ronde Poseidon active·fsel·a^7 domine (9), comme
	// poseidon_air_full.go / membership_air.go.
	spMaxDegree = pfMaxDegree // 9

	// Nombre de résidus de transition : 12 (état) + 4 (regNk) + 4 (regCm) +
	// 1 (regVal) + 1 (binarité bit) + 1 (conservation) = 23.
	spNumResidues = pfStateCols + poseidonDigestLen + poseidonDigestLen + 1 + 1 + 1 // 23
)

// Indices de blocs (numéro de bloc dans la pile).
const (
	spBlkOwner = 0              // ownerTag = PoseidonHash(nk)
	spBlkInCm  = 1              // inCm = Commit(inValue, ownerTag, inRho)
	spBlkNf    = 2              // nf = Hash2(nk, inCm)
	spBlkMem0  = 3              // premier niveau d'appartenance
	spBlkMemL  = spBlkMem0 + spendDepth - 1 // dernier niveau (sortie = merkleRoot)
	spBlkOutCm = spBlkMemL + 1  // outCm = Commit(outValue, outOwnerTag, outRho)
)

// spOutputRowOf renvoie la ligne de SORTIE (digest dans les 4 premières cellules)
// du bloc `b` : c'est la 31e ligne du bloc (indice pfOutputRow=30 dans le bloc).
func spOutputRowOf(b int) int { return b*spBlock + pfOutputRow }

// Lignes publiques porteuses des sorties (digests).
func spNfRow() int        { return spOutputRowOf(spBlkNf) }
func spRootRow() int      { return spOutputRowOf(spBlkMemL) }
func spOutCmRow() int     { return spOutputRowOf(spBlkOutCm) }

// ---------------------------------------------------------------------------
// AIR de dépense
// ---------------------------------------------------------------------------

// spendAIR implémente l'interface AIR (stark_mc.go) pour le circuit de dépense.
// Les SEULES données publiques portées par l'instance sont les sorties ancrées
// par bord : nf, merkleRoot, outCm, fee. Le témoin (nk, ownerTag, inValue, inRho,
// chemin Merkle, outValue, outOwnerTag, outRho, inCm) ne figure PAS dans
// l'instance : il n'existe que dans la trace-témoin.
type spendAIR struct {
	merkleRoot [poseidonDigestLen]Felt
	nf         [poseidonDigestLen]Felt
	outCm      [poseidonDigestLen]Felt
	fee        Felt
}

func (a spendAIR) NumColumns() int { return spNumCols }
func (a spendAIR) NumSteps() int   { return spSteps }
func (a spendAIR) MaxDegree() int  { return spMaxDegree }

// EvalTransition applique, sur la paire (cur, next), la ronde Poseidon (active=1),
// un glue inter-bloc (un des modes), ou l'identité ; gère les registres et la
// conservation. Renvoie spNumResidues (23) résidus. PURE et déterministe : ne lit
// sélecteurs/constantes que dans cur.
func (a spendAIR) EvalTransition(cur, next []Felt) []Felt {
	one := One()
	fsel := cur[spFselCol]
	active := cur[spActCol]
	oneMinusFsel := one.Sub(fsel)

	// Modes de glue.
	mCommitS := cur[spMCommitS]
	mHash2 := cur[spMHash2]
	mReasmReg := cur[spMReasmReg]
	mReasm := cur[spMReasm]
	mCommitW := cur[spMCommitW]

	// --- Branche RONDE POSEIDON (identique à poseidon_air_full.go) ---
	var arc [pfStateCols]Felt
	for k := 0; k < pfStateCols; k++ {
		arc[k] = cur[spStateOff+k].Add(cur[spRcOff+k])
	}
	var sb [pfStateCols]Felt
	sb[0] = arc[0].Exp(poseidonAlpha)
	for k := 1; k < pfStateCols; k++ {
		ak7 := arc[k].Exp(poseidonAlpha)
		sb[k] = fsel.Mul(ak7).Add(oneMinusFsel.Mul(arc[k]))
	}

	// --- Sources des modes de glue (état d'entrée du bloc suivant) ---
	// child courant = cur.s[0..3] (digest sortant du bloc).
	var child [poseidonDigestLen]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		child[k] = cur[spStateOff+k]
	}
	// Registres.
	var regNk, regCm [poseidonDigestLen]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		regNk[k] = cur[spRegNkOff+k]
		regCm[k] = cur[spRegCmOff+k]
	}
	// Témoins de commitment.
	var wTag, wRho [poseidonDigestLen]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		wTag[k] = cur[spWTagOff+k]
		wRho[k] = cur[spWRhoOff+k]
	}
	wVal := cur[spWValCol]
	bit := cur[spBitCol]
	oneMinusBit := one.Sub(bit)

	domainSep := FromUint64(poseidonDomainSep)
	rateF := FromUint64(uint64(poseidonRate))
	commitSep := FromUint64(spCommitSep)

	// commitS : tag = child (cur.s[0..3]), rho = wRho, value = wVal.
	var commitS [pfStateCols]Felt
	// commitW : tag = wTag (témoin), rho = wRho, value = wVal.
	var commitW [pfStateCols]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		commitS[k] = child[k]
		commitW[k] = wTag[k]
		commitS[poseidonDigestLen+k] = wRho[k]
		commitW[poseidonDigestLen+k] = wRho[k]
	}
	commitS[poseidonRate] = wVal
	commitW[poseidonRate] = wVal
	commitS[poseidonRate+1] = commitSep
	commitW[poseidonRate+1] = commitSep

	// hash2 : left = regNk, right = child (=inCm). Ensemencement Hash2.
	var hash2 [pfStateCols]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		hash2[k] = regNk[k]
		hash2[poseidonDigestLen+k] = child[k]
	}
	hash2[poseidonRate] = domainSep
	hash2[poseidonRate+1] = rateF

	// reasm : ré-assemblage Merkle child = cur.s[0..3] ; reasmReg : child = regCm.
	var reasm, reasmReg [pfStateCols]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		sibk := cur[spSibOff+k]
		// gauche (0..3) : bit==0 -> child, bit==1 -> sib.
		reasm[k] = oneMinusBit.Mul(child[k]).Add(bit.Mul(sibk))
		reasmReg[k] = oneMinusBit.Mul(regCm[k]).Add(bit.Mul(sibk))
		// droite (4..7) : bit==0 -> sib, bit==1 -> child.
		reasm[poseidonDigestLen+k] = oneMinusBit.Mul(sibk).Add(bit.Mul(child[k]))
		reasmReg[poseidonDigestLen+k] = oneMinusBit.Mul(sibk).Add(bit.Mul(regCm[k]))
	}
	reasm[poseidonRate] = domainSep
	reasmReg[poseidonRate] = domainSep
	reasm[poseidonRate+1] = rateF
	reasmReg[poseidonRate+1] = rateF

	// Facteur identité : 1 - active - (somme des modes de glue). Vaut 0 ou 1 car
	// au plus un mode (ou active) est à 1 sur une ligne donnée (mutuelle exclusion
	// garantie par les bords publics : voir spendRowStructure).
	sumModes := mCommitS.Add(mHash2).Add(mReasmReg).Add(mReasm).Add(mCommitW)
	idfac := one.Sub(active).Sub(sumModes)

	residues := make([]Felt, spNumResidues)

	// --- 12 résidus d'état ---
	for k := 0; k < pfStateCols; k++ {
		// Ronde Poseidon (MDS de sb).
		acc := Zero()
		row := &params.mds[k]
		for j := 0; j < pfStateCols; j++ {
			acc = acc.Add(row[j].Mul(sb[j]))
		}
		roundK := acc
		// out_k = active·round + Σ modes·source + idfac·cur.s.
		outk := active.Mul(roundK).
			Add(mCommitS.Mul(commitS[k])).
			Add(mHash2.Mul(hash2[k])).
			Add(mReasmReg.Mul(reasmReg[k])).
			Add(mReasm.Mul(reasm[k])).
			Add(mCommitW.Mul(commitW[k])).
			Add(idfac.Mul(cur[spStateOff+k]))
		residues[k] = next[spStateOff+k].Sub(outk)
	}

	// --- Registres ---
	loadNk := cur[spLoadNk]
	holdNk := cur[spHoldNk]
	loadCm := cur[spLoadCm]
	holdCm := cur[spHoldCm]
	loadVal := cur[spLoadVal]
	holdVal := cur[spHoldVal]

	idx := pfStateCols
	// regNk : load·(cur.regNk - cur.s) + hold·(next.regNk - cur.regNk).
	for k := 0; k < poseidonDigestLen; k++ {
		loadRes := loadNk.Mul(cur[spRegNkOff+k].Sub(cur[spStateOff+k]))
		holdRes := holdNk.Mul(next[spRegNkOff+k].Sub(cur[spRegNkOff+k]))
		residues[idx] = loadRes.Add(holdRes)
		idx++
	}
	// regCm : load·(cur.regCm - cur.s) + hold·(next.regCm - cur.regCm).
	for k := 0; k < poseidonDigestLen; k++ {
		loadRes := loadCm.Mul(cur[spRegCmOff+k].Sub(cur[spStateOff+k]))
		holdRes := holdCm.Mul(next[spRegCmOff+k].Sub(cur[spRegCmOff+k]))
		residues[idx] = loadRes.Add(holdRes)
		idx++
	}
	// regVal : load·(cur.regVal - wVal) + hold·(next.regVal - cur.regVal).
	{
		loadRes := loadVal.Mul(cur[spRegValCol].Sub(wVal))
		holdRes := holdVal.Mul(next[spRegValCol].Sub(cur[spRegValCol]))
		residues[idx] = loadRes.Add(holdRes)
		idx++
	}

	// --- Binarité du bit (sur les lignes de ré-assemblage) ---
	reasmAny := mReasmReg.Add(mReasm)
	residues[idx] = reasmAny.Mul(bit).Mul(bit.Sub(one))
	idx++

	// --- Conservation : sur la ligne du commitment de sortie (mCommitW),
	//     regVal (= inValue porté) doit valoir wVal (= outValue) + fee. ---
	residues[idx] = mCommitW.Mul(cur[spRegValCol].Sub(wVal).Sub(cur[spFeeCol]))
	idx++

	return residues
}

// Boundaries renvoie les contraintes de bord :
//
//   - colonnes de structure (rc, fsel, active, tous les modes/load/hold, feeCol)
//     ancrées sur CHAQUE ligne aux valeurs précalculées PUBLIQUES ;
//   - capacités d'entrée fixées des blocs ownerTag/inCm/nf/appartenance (les
//     cellules de capacité 8..11 de l'état d'entrée de chaque bloc sont des
//     CONSTANTES publiques du schéma, donc ancrées) ;
//   - sorties PUBLIQUES : nf (ligne spNfRow), merkleRoot (spRootRow), outCm
//     (spOutCmRow), chacune sur les 4 premières cellules d'état.
//
// Les colonnes-témoins (état d'entrée hors capacité, sib, bit, regNk, regCm,
// regVal, wTag, wRho, wVal) ne sont PAS ancrées : ce sont les secrets.
func (a spendAIR) Boundaries() []Boundary {
	bs := make([]Boundary, 0, spSteps*16)

	// Colonnes de structure, ligne par ligne.
	for row := 0; row < spSteps; row++ {
		st := spendRowStructure(row, a.fee)
		for k := 0; k < pfStateCols; k++ {
			bs = append(bs, Boundary{Col: spRcOff + k, Row: row, Value: st.rc[k]})
		}
		bs = append(bs, Boundary{Col: spFselCol, Row: row, Value: st.fsel})
		bs = append(bs, Boundary{Col: spActCol, Row: row, Value: st.active})
		bs = append(bs, Boundary{Col: spMCommitS, Row: row, Value: st.mCommitS})
		bs = append(bs, Boundary{Col: spMHash2, Row: row, Value: st.mHash2})
		bs = append(bs, Boundary{Col: spMReasmReg, Row: row, Value: st.mReasmReg})
		bs = append(bs, Boundary{Col: spMReasm, Row: row, Value: st.mReasm})
		bs = append(bs, Boundary{Col: spMCommitW, Row: row, Value: st.mCommitW})
		bs = append(bs, Boundary{Col: spLoadNk, Row: row, Value: st.loadNk})
		bs = append(bs, Boundary{Col: spHoldNk, Row: row, Value: st.holdNk})
		bs = append(bs, Boundary{Col: spLoadCm, Row: row, Value: st.loadCm})
		bs = append(bs, Boundary{Col: spHoldCm, Row: row, Value: st.holdCm})
		bs = append(bs, Boundary{Col: spLoadVal, Row: row, Value: st.loadVal})
		bs = append(bs, Boundary{Col: spHoldVal, Row: row, Value: st.holdVal})
		bs = append(bs, Boundary{Col: spFeeCol, Row: row, Value: st.fee})
	}

	// Capacités d'entrée des blocs, ancrées en ligne 0 du bloc concerné. Ces
	// cellules de capacité sont des CONSTANTES du schéma (séparateurs/0), donc
	// publiques ; les cellules de rate (0..7) restent TÉMOINS (non ancrées).
	// - bloc ownerTag : [8]=spOwnerSep, [9,10,11]=0.
	addCap(&bs, spBlkOwner*spBlock, FromUint64(spOwnerSep), Zero(), Zero(), Zero())
	// - bloc inCm : [9]=spCommitSep ; [8]=value (TÉMOIN), [10,11]=0.
	addCapPartial(&bs, spBlkInCm*spBlock)
	// - bloc nf : [8]=domainSep, [9]=8, [10,11]=0 (ensemencement Hash2).
	addCap(&bs, spBlkNf*spBlock, FromUint64(poseidonDomainSep), FromUint64(uint64(poseidonRate)), Zero(), Zero())
	// - blocs d'appartenance : [8]=domainSep, [9]=8, [10,11]=0 (ensemencement Hash2).
	for lvl := 0; lvl < spendDepth; lvl++ {
		addCap(&bs, (spBlkMem0+lvl)*spBlock, FromUint64(poseidonDomainSep), FromUint64(uint64(poseidonRate)), Zero(), Zero())
	}
	// - bloc outCm : [9]=spCommitSep ; [8]=value (TÉMOIN), [10,11]=0.
	addCapPartial(&bs, spBlkOutCm*spBlock)

	// Sorties publiques.
	for k := 0; k < poseidonDigestLen; k++ {
		bs = append(bs, Boundary{Col: spStateOff + k, Row: spNfRow(), Value: a.nf[k]})
		bs = append(bs, Boundary{Col: spStateOff + k, Row: spRootRow(), Value: a.merkleRoot[k]})
		bs = append(bs, Boundary{Col: spStateOff + k, Row: spOutCmRow(), Value: a.outCm[k]})
	}

	return bs
}

// addCap ancre les 4 cellules de capacité (8..11) de l'état d'entrée (ligne row0
// du bloc) aux valeurs fournies.
func addCap(bs *[]Boundary, row0 int, c8, c9, c10, c11 Felt) {
	*bs = append(*bs,
		Boundary{Col: spStateOff + poseidonRate, Row: row0, Value: c8},
		Boundary{Col: spStateOff + poseidonRate + 1, Row: row0, Value: c9},
		Boundary{Col: spStateOff + poseidonRate + 2, Row: row0, Value: c10},
		Boundary{Col: spStateOff + poseidonRate + 3, Row: row0, Value: c11},
	)
}

// addCapPartial ancre la capacité d'un bloc commitment : [9]=spCommitSep,
// [10,11]=0. La cellule [8] (value) reste TÉMOIN (cachée), donc NON ancrée.
func addCapPartial(bs *[]Boundary, row0 int) {
	*bs = append(*bs,
		Boundary{Col: spStateOff + poseidonRate + 1, Row: row0, Value: FromUint64(spCommitSep)},
		Boundary{Col: spStateOff + poseidonRate + 2, Row: row0, Value: Zero()},
		Boundary{Col: spStateOff + poseidonRate + 3, Row: row0, Value: Zero()},
	)
}

// ---------------------------------------------------------------------------
// Structure (valeurs publiques) par ligne
// ---------------------------------------------------------------------------

// spendStructure regroupe les valeurs publiques des colonnes de structure d'une
// ligne (constantes de ronde, sélecteurs Poseidon, modes de glue, registres,
// frais). Ces colonnes décrivent la STRUCTURE du circuit, identique pour tous.
type spendStructure struct {
	rc                                              [pfStateCols]Felt
	fsel, active                                    Felt
	mCommitS, mHash2, mReasmReg, mReasm, mCommitW    Felt
	loadNk, holdNk, loadCm, holdCm, loadVal, holdVal Felt
	fee                                             Felt
}

// spendRowStructure renvoie les valeurs publiques de structure pour la ligne
// `row` (fee est la valeur publique de frais, ancrée seulement à la ligne du
// commitment de sortie). C'est le PLAN du circuit ; il fixe :
//
//   - rondes Poseidon 0..29 de chaque bloc (active=1, rc/fsel du tour) ;
//   - le mode de glue sur la ligne de SORTIE (indice 30) de chaque bloc, qui
//     construit l'entrée du bloc suivant ;
//   - les fenêtres load/hold des registres regNk, regCm, regVal.
//
// Mutuelle exclusion : sur une ligne donnée, au plus un de {active, mCommitS,
// mHash2, mReasmReg, mReasm, mCommitW} vaut 1.
func spendRowStructure(row int, fee Felt) spendStructure {
	var s spendStructure
	block := row / spBlock
	r := row % spBlock

	// --- Rondes Poseidon réelles (0..29) de chaque bloc ---
	if r < poseidonTotalRounds {
		s.rc = params.roundConstants[r]
		s.active = One()
		if pfIsFullRound(r) {
			s.fsel = One()
		}
	}

	// --- Glue : sur la ligne de SORTIE (indice pfOutputRow=30) du bloc, on choisit
	//     le mode qui construit l'entrée du bloc SUIVANT. ---
	if r == pfOutputRow {
		switch block {
		case spBlkOwner: // ownerTag -> inCm : commit, tag = état (ownerTag).
			s.mCommitS = One()
		case spBlkInCm: // inCm -> nf : Hash2(regNk, état=inCm).
			s.mHash2 = One()
		case spBlkNf: // nf -> appartenance L0 : ré-assemblage child = regCm (=inCm).
			s.mReasmReg = One()
		case spBlkOutCm:
			// dernier bloc : pas de glue (identité) -> outCm reste en sortie.
		default:
			if block >= spBlkMem0 && block < spBlkMemL {
				// niveau d'appartenance i>0 -> niveau i+1 : ré-assemblage child = état.
				s.mReasm = One()
			} else if block == spBlkMemL {
				// dernier niveau d'appartenance -> outCm : commit, tag = wTag témoin.
				s.mCommitW = One()
			}
			// (spBlkMem0 a son glue géré par spBlkNf ci-dessus : la chaîne entre dans
			//  L0 via mReasmReg, puis L0->L1 via mReasm sur la sortie de L0.)
		}
	}

	// Correction du cas L0 -> L1 : la sortie du bloc spBlkMem0 doit ré-assembler
	// vers spBlkMem0+1 (mode mReasm), comme tout niveau interne. Le switch ci-dessus
	// le couvre via la branche default (block in (spBlkMem0, spBlkMemL)) SAUF
	// block==spBlkMem0 lui-même. On l'ajoute explicitement :
	if r == pfOutputRow && block == spBlkMem0 && spBlkMem0 < spBlkMemL {
		s.mReasm = One()
	}

	// --- Registres ---
	// regNk : chargé à la ligne 0 (entrée ownerTag = pack(nk), donc cur.s[0..3]=nk),
	// tenu constant jusqu'à la ligne de glue du bloc nf (relecture en mHash2).
	if row == 0 {
		s.loadNk = One()
	}
	if row >= 0 && row < spNfRow() {
		// On tient regNk de la ligne 0 jusqu'à AVANT la ligne nf-glue, de sorte que
		// cur.regNk à la ligne nf-glue (= spOutputRowOf(spBlkInCm)) vaille nk.
		s.holdNk = One()
	}

	// regCm : chargé à la ligne de SORTIE du bloc inCm (cur.s[0..3]=inCm), tenu
	// constant jusqu'à la ligne de glue du bloc nf (mReasmReg lit regCm).
	loadCmRow := spOutputRowOf(spBlkInCm)
	if row == loadCmRow {
		s.loadCm = One()
	}
	if row >= loadCmRow && row < spNfRow() {
		s.holdCm = One()
	}

	// regVal : chargé à la ligne de glue du bloc inCm (mCommitS : wVal=inValue),
	// tenu constant jusqu'à la ligne de glue du bloc outCm (conservation).
	loadValRow := spOutputRowOf(spBlkOwner) // ligne mCommitS (entrée inCm)
	outCmGlueRow := spOutputRowOf(spBlkMemL) // ligne mCommitW (entrée outCm)
	if row == loadValRow {
		s.loadVal = One()
	}
	if row >= loadValRow && row < outCmGlueRow {
		s.holdVal = One()
	}

	// fee : valeur publique ancrée UNIQUEMENT sur la ligne de conservation (mCommitW,
	// = entrée outCm). 0 ailleurs.
	if row == outCmGlueRow {
		s.fee = fee
	}

	return s
}
