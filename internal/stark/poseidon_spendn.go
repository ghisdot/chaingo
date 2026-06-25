// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// ÉTAGE 4.3 — Dépense blindée 1-ENTRÉE / N-SORTIES (note splitting).
//
// Généralise le circuit 1-in/1-out (poseidon_spend_air.go) au cas d'UNE entrée
// dépensée vers PLUSIEURS sorties (fractionnement d'une note). Le côté ENTRÉE est
// STRICTEMENT identique au circuit 1-out (ownerTag -> inCm -> nf -> appartenance),
// donc réutilise telle quelle la logique de glue/registres déjà éprouvée. Seul le
// côté SORTIE change :
//
//   - au lieu d'un unique bloc outCm, on enchaîne numOut blocs de commitment, le
//     glue mCommitW de chaque bloc construisant l'entrée du suivant (exactement
//     comme la chaîne d'appartenance enchaîne ses niveaux) ;
//   - la CONSERVATION devient une somme : un registre ACCUMULATEUR regAcc additionne
//     chaque outValue_j (déclenché par le même sélecteur mCommitW qui forme la
//     sortie), et une contrainte finale impose inValue == Σ outValue_j + fee.
//
// La hauteur de trace (nombre de blocs · 32) doit être une puissance de 2 ; on
// COMPLÈTE donc par des blocs IDENTITÉ inertes (active=0, aucun glue) après la
// dernière sortie. Ces blocs ne portent aucune contrainte de hachage ni de bord.
//
// PORTÉE / GAPS : mêmes réserves que le circuit 1-out (profondeur d'arbre FIXE
// spendDepth, pas de range proof — un wrap-around Goldilocks de la conservation
// reste un GAP documenté, trace non masquée). 1 ENTRÉE seulement ; le multi-ENTRÉE
// suit le même schéma d'accumulateur côté entrée et reste à faire.
//
// DÉTERMINISME ABSOLU : aucun time/rand ; tout l'aléa vient de ProveAIR/VerifyAIR.
package stark

// ---------------------------------------------------------------------------
// Disposition (réutilise le layout de colonnes du circuit 1-out + 2 colonnes)
// ---------------------------------------------------------------------------

const (
	// snInBlocks = blocs du côté ENTRÉE : ownerTag(1) + inCm(1) + nf(1) +
	// appartenance(spendDepth) = spendDepth + 3 (= 7 pour d=4).
	snInBlocks = spendDepth + 3

	// Colonnes ajoutées au layout du circuit 1-out (qui occupe [0, spNumCols)).
	snRegAccCol = spNumCols     // accumulateur Σ outValue_j (porté)
	snConsCol   = spNumCols + 1 // sélecteur PUBLIC : ligne de conservation finale
	snNumCols   = spNumCols + 2 // largeur totale du circuit N-sorties

	// Résidus : les 23 du circuit 1-out, MOINS sa conservation mono-sortie, PLUS
	// (accumulateur) + (conservation par accumulateur) = 23 - 1 + 2 = 24.
	snNumResidues = spNumResidues + 1
)

// snBlkOutput0 est l'indice du PREMIER bloc de sortie (juste après l'entrée).
const snBlkOutput0 = snInBlocks

// snRealBlocks renvoie le nombre de blocs RÉELS (entrée + numOut sorties), avant
// complétion en puissance de 2.
func snRealBlocks(numOut int) int { return snInBlocks + numOut }

// snTotalBlocks renvoie le nombre de blocs APRÈS complétion identité (puissance de
// 2 de blocs, donc hauteur = ·32 puissance de 2 pour la NTT).
func snTotalBlocks(numOut int) int { return nextPow2(snRealBlocks(numOut)) }

// snSteps renvoie la hauteur de trace (puissance de 2).
func snSteps(numOut int) int { return snTotalBlocks(numOut) * spBlock }

// snBlkOutput renvoie l'indice de bloc de la j-ème sortie.
func snBlkOutput(j int) int { return snBlkOutput0 + j }

// snConsRow renvoie la ligne de conservation : la ligne de SORTIE de la DERNIÈRE
// sortie (regAcc y vaut la somme totale, regVal y vaut encore inValue).
func snConsRow(numOut int) int { return spOutputRowOf(snBlkOutput(numOut - 1)) }

// ---------------------------------------------------------------------------
// Témoin / énoncé
// ---------------------------------------------------------------------------

// SpendNOut décrit UNE note de sortie (témoin privé : value/owner/rho).
type SpendNOut struct {
	Value    Felt
	OwnerTag [poseidonDigestLen]Felt
	Rho      [poseidonDigestLen]Felt
}

// SpendNWitness est le témoin d'une dépense 1-entrée / N-sorties. Le côté entrée
// est identique à SpendWitness ; le côté sortie est un slice (len == numOut >= 1).
type SpendNWitness struct {
	// Entrée (identique à SpendWitness).
	InValue Felt
	InRho   [poseidonDigestLen]Felt
	Nk      [poseidonDigestLen]Felt
	Path    SpendPath
	// Sorties (au moins une).
	Outs []SpendNOut
}

// SpendNPublic est l'énoncé public : racine, nullifier (unique : une entrée),
// les numOut engagements de sortie, et les frais.
type SpendNPublic struct {
	MerkleRoot [poseidonDigestLen]Felt
	Nf         [poseidonDigestLen]Felt
	OutCms     [][poseidonDigestLen]Felt
	Fee        Felt
}

// ---------------------------------------------------------------------------
// AIR
// ---------------------------------------------------------------------------

type spendNAIR struct {
	merkleRoot [poseidonDigestLen]Felt
	nf         [poseidonDigestLen]Felt
	outCms     [][poseidonDigestLen]Felt
	fee        Felt
	numOut     int
}

func (a spendNAIR) NumColumns() int { return snNumCols }
func (a spendNAIR) NumSteps() int   { return snSteps(a.numOut) }
func (a spendNAIR) MaxDegree() int  { return spMaxDegree }

// EvalTransition : réplique EXACTE des branches ronde/glue/registres du circuit
// 1-out (poseidon_spend_air.go) pour l'état et regNk/regCm/regVal/bit, puis
// remplace la conservation mono-sortie par (accumulateur) + (conservation finale).
func (a spendNAIR) EvalTransition(cur, next []Felt) []Felt {
	one := One()
	fsel := cur[spFselCol]
	active := cur[spActCol]
	oneMinusFsel := one.Sub(fsel)

	mCommitS := cur[spMCommitS]
	mHash2 := cur[spMHash2]
	mReasmReg := cur[spMReasmReg]
	mReasm := cur[spMReasm]
	mCommitW := cur[spMCommitW]

	// Ronde Poseidon.
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

	var child [poseidonDigestLen]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		child[k] = cur[spStateOff+k]
	}
	var regNk, regCm [poseidonDigestLen]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		regNk[k] = cur[spRegNkOff+k]
		regCm[k] = cur[spRegCmOff+k]
	}
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

	var commitS, commitW [pfStateCols]Felt
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

	var hash2 [pfStateCols]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		hash2[k] = regNk[k]
		hash2[poseidonDigestLen+k] = child[k]
	}
	hash2[poseidonRate] = domainSep
	hash2[poseidonRate+1] = rateF

	var reasm, reasmReg [pfStateCols]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		sibk := cur[spSibOff+k]
		reasm[k] = oneMinusBit.Mul(child[k]).Add(bit.Mul(sibk))
		reasmReg[k] = oneMinusBit.Mul(regCm[k]).Add(bit.Mul(sibk))
		reasm[poseidonDigestLen+k] = oneMinusBit.Mul(sibk).Add(bit.Mul(child[k]))
		reasmReg[poseidonDigestLen+k] = oneMinusBit.Mul(sibk).Add(bit.Mul(regCm[k]))
	}
	reasm[poseidonRate] = domainSep
	reasmReg[poseidonRate] = domainSep
	reasm[poseidonRate+1] = rateF
	reasmReg[poseidonRate+1] = rateF

	sumModes := mCommitS.Add(mHash2).Add(mReasmReg).Add(mReasm).Add(mCommitW)
	idfac := one.Sub(active).Sub(sumModes)

	residues := make([]Felt, snNumResidues)

	// 12 résidus d'état (identique au circuit 1-out).
	for k := 0; k < pfStateCols; k++ {
		acc := Zero()
		row := &params.mds[k]
		for j := 0; j < pfStateCols; j++ {
			acc = acc.Add(row[j].Mul(sb[j]))
		}
		outk := active.Mul(acc).
			Add(mCommitS.Mul(commitS[k])).
			Add(mHash2.Mul(hash2[k])).
			Add(mReasmReg.Mul(reasmReg[k])).
			Add(mReasm.Mul(reasm[k])).
			Add(mCommitW.Mul(commitW[k])).
			Add(idfac.Mul(cur[spStateOff+k]))
		residues[k] = next[spStateOff+k].Sub(outk)
	}

	// Registres (identique au circuit 1-out).
	loadNk := cur[spLoadNk]
	holdNk := cur[spHoldNk]
	loadCm := cur[spLoadCm]
	holdCm := cur[spHoldCm]
	loadVal := cur[spLoadVal]
	holdVal := cur[spHoldVal]

	idx := pfStateCols
	for k := 0; k < poseidonDigestLen; k++ {
		loadRes := loadNk.Mul(cur[spRegNkOff+k].Sub(cur[spStateOff+k]))
		holdRes := holdNk.Mul(next[spRegNkOff+k].Sub(cur[spRegNkOff+k]))
		residues[idx] = loadRes.Add(holdRes)
		idx++
	}
	for k := 0; k < poseidonDigestLen; k++ {
		loadRes := loadCm.Mul(cur[spRegCmOff+k].Sub(cur[spStateOff+k]))
		holdRes := holdCm.Mul(next[spRegCmOff+k].Sub(cur[spRegCmOff+k]))
		residues[idx] = loadRes.Add(holdRes)
		idx++
	}
	{
		loadRes := loadVal.Mul(cur[spRegValCol].Sub(wVal))
		holdRes := holdVal.Mul(next[spRegValCol].Sub(cur[spRegValCol]))
		residues[idx] = loadRes.Add(holdRes)
		idx++
	}

	// Binarité du bit (lignes de ré-assemblage).
	reasmAny := mReasmReg.Add(mReasm)
	residues[idx] = reasmAny.Mul(bit).Mul(bit.Sub(one))
	idx++

	// --- ACCUMULATEUR de conservation ---
	// next.regAcc = cur.regAcc + mCommitW·wVal : chaque glue de sortie (mCommitW)
	// ajoute sa valeur outValue_j ; partout ailleurs (mCommitW=0) regAcc est tenu.
	residues[idx] = next[snRegAccCol].Sub(cur[snRegAccCol]).Sub(mCommitW.Mul(wVal))
	idx++

	// --- CONSERVATION finale ---
	// sur la ligne de conservation (snCons=1) : regVal (= inValue) doit valoir
	// regAcc (= Σ outValue_j) + fee.
	cons := cur[snConsCol]
	residues[idx] = cons.Mul(cur[spRegValCol].Sub(cur[snRegAccCol]).Sub(cur[spFeeCol]))
	idx++

	return residues
}

// Boundaries : structure par ligne (avec snCons), capacités des blocs entrée +
// sorties, init regAcc=0 (ligne 0), et sorties publiques (nf, racine, chaque outCm).
func (a spendNAIR) Boundaries() []Boundary {
	steps := a.NumSteps()
	bs := make([]Boundary, 0, steps*16)

	for row := 0; row < steps; row++ {
		st := spendNRowStructure(row, a.fee, a.numOut)
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
		bs = append(bs, Boundary{Col: snConsCol, Row: row, Value: st.cons})
	}

	// Capacités d'entrée (identiques au circuit 1-out).
	addCap(&bs, spBlkOwner*spBlock, FromUint64(spOwnerSep), Zero(), Zero(), Zero())
	addCapPartial(&bs, spBlkInCm*spBlock)
	addCap(&bs, spBlkNf*spBlock, FromUint64(poseidonDomainSep), FromUint64(uint64(poseidonRate)), Zero(), Zero())
	for lvl := 0; lvl < spendDepth; lvl++ {
		addCap(&bs, (spBlkMem0+lvl)*spBlock, FromUint64(poseidonDomainSep), FromUint64(uint64(poseidonRate)), Zero(), Zero())
	}
	// Capacités de CHAQUE bloc de sortie (commitment).
	for j := 0; j < a.numOut; j++ {
		addCapPartial(&bs, snBlkOutput(j)*spBlock)
	}

	// Init de l'accumulateur : regAcc == 0 à la ligne 0.
	bs = append(bs, Boundary{Col: snRegAccCol, Row: 0, Value: Zero()})

	// Sorties publiques.
	for k := 0; k < poseidonDigestLen; k++ {
		bs = append(bs, Boundary{Col: spStateOff + k, Row: spNfRow(), Value: a.nf[k]})
		bs = append(bs, Boundary{Col: spStateOff + k, Row: spRootRow(), Value: a.merkleRoot[k]})
		for j := 0; j < a.numOut; j++ {
			bs = append(bs, Boundary{Col: spStateOff + k, Row: spOutputRowOf(snBlkOutput(j)), Value: a.outCms[j][k]})
		}
	}

	return bs
}

// ---------------------------------------------------------------------------
// Structure par ligne (généralise spendRowStructure au côté N-sorties)
// ---------------------------------------------------------------------------

type spendNStructure struct {
	rc                                              [pfStateCols]Felt
	fsel, active                                    Felt
	mCommitS, mHash2, mReasmReg, mReasm, mCommitW    Felt
	loadNk, holdNk, loadCm, holdCm, loadVal, holdVal Felt
	fee, cons                                       Felt
}

func spendNRowStructure(row int, fee Felt, numOut int) spendNStructure {
	var s spendNStructure
	block := row / spBlock
	r := row % spBlock
	realBlocks := snRealBlocks(numOut)
	memL := spBlkMem0 + spendDepth - 1
	lastOut := snBlkOutput(numOut - 1)

	// Blocs de COMPLÉTION (identité) : aucune ronde, aucun glue, aucun registre.
	if block >= realBlocks {
		return s
	}

	// Rondes Poseidon réelles (0..29) de chaque bloc réel.
	if r < poseidonTotalRounds {
		s.rc = params.roundConstants[r]
		s.active = One()
		if pfIsFullRound(r) {
			s.fsel = One()
		}
	}

	// Glue sur la ligne de sortie (r == pfOutputRow).
	if r == pfOutputRow {
		switch {
		case block == spBlkOwner:
			s.mCommitS = One() // ownerTag -> inCm
		case block == spBlkInCm:
			s.mHash2 = One() // inCm -> nf
		case block == spBlkNf:
			s.mReasmReg = One() // nf -> appartenance L0 (child = regCm)
		case block >= spBlkMem0 && block < memL:
			s.mReasm = One() // niveau d'appartenance i -> i+1
		case block == memL:
			s.mCommitW = One() // dernier niveau -> sortie 0
		case block >= snBlkOutput0 && block < lastOut:
			s.mCommitW = One() // sortie j -> sortie j+1
		case block == lastOut:
			// dernière sortie : pas de glue (identité), conservation ici.
		}
	}

	// regNk : chargé ligne 0, tenu jusqu'avant la ligne nf-glue.
	if row == 0 {
		s.loadNk = One()
	}
	if row < spNfRow() {
		s.holdNk = One()
	}

	// regCm : chargé en sortie inCm, tenu jusqu'avant la ligne nf-glue.
	loadCmRow := spOutputRowOf(spBlkInCm)
	if row == loadCmRow {
		s.loadCm = One()
	}
	if row >= loadCmRow && row < spNfRow() {
		s.holdCm = One()
	}

	// regVal : chargé à la glue inCm (mCommitS, wVal=inValue), tenu jusqu'à la
	// ligne de conservation (sortie de la DERNIÈRE sortie).
	loadValRow := spOutputRowOf(spBlkOwner)
	consRow := snConsRow(numOut)
	if row == loadValRow {
		s.loadVal = One()
	}
	if row >= loadValRow && row < consRow {
		s.holdVal = One()
	}

	// fee + sélecteur de conservation : uniquement sur la ligne de conservation.
	if row == consRow {
		s.fee = fee
		s.cons = One()
	}

	return s
}

// ---------------------------------------------------------------------------
// Construction de la trace
// ---------------------------------------------------------------------------

// spFillBlockN remplit un bloc à partir de l'état d'entrée `in`, en respectant la
// structure N-sorties. Pour un bloc de complétion (active=0), l'état est tenu
// constant (pas de ronde) ; pour un bloc réel, identique à spFillBlock.
func spFillBlockN(trace [][]Felt, b int, in [pfStateCols]Felt, fee Felt, numOut int) (output [pfStateCols]Felt) {
	base := b * spBlock
	s := in
	for r := 0; r < spBlock; r++ {
		row := base + r
		st := spendNRowStructure(row, fee, numOut)
		line := make([]Felt, snNumCols)
		for k := 0; k < pfStateCols; k++ {
			line[spStateOff+k] = s[k]
			line[spRcOff+k] = st.rc[k]
		}
		line[spFselCol] = st.fsel
		line[spActCol] = st.active
		line[spMCommitS] = st.mCommitS
		line[spMHash2] = st.mHash2
		line[spMReasmReg] = st.mReasmReg
		line[spMReasm] = st.mReasm
		line[spMCommitW] = st.mCommitW
		line[spLoadNk] = st.loadNk
		line[spHoldNk] = st.holdNk
		line[spLoadCm] = st.loadCm
		line[spHoldCm] = st.holdCm
		line[spLoadVal] = st.loadVal
		line[spHoldVal] = st.holdVal
		line[spFeeCol] = st.fee
		line[snConsCol] = st.cons
		trace[row] = line

		// Avance de l'état uniquement sur les rondes ACTIVES (blocs réels).
		if r < poseidonTotalRounds && !st.active.IsZero() {
			s = pfApplyRound(s, st.rc, !st.fsel.IsZero())
		}
	}
	return s
}

// buildSpendNTrace construit la trace satisfaisante 1-entrée/N-sorties et renvoie
// l'énoncé public reconstruit.
func buildSpendNTrace(w SpendNWitness, fee Felt) (trace [][]Felt, public SpendNPublic) {
	numOut := len(w.Outs)
	if numOut < 1 {
		panic("stark: buildSpendNTrace: au moins une sortie requise")
	}
	for i := 0; i < spendDepth; i++ {
		if !(w.Path.Bits[i].IsZero() || w.Path.Bits[i].Equal(One())) {
			panic("stark: buildSpendNTrace: bit de direction non binaire")
		}
	}

	steps := snSteps(numOut)
	trace = make([][]Felt, steps)

	// Valeurs natives.
	ownerTag := SpendOwnerTag(w.Nk)
	inCm := SpendCommit(w.InValue, ownerTag, w.InRho)
	nf := SpendNullifier(w.Nk, inCm)
	merkleRoot := spChainRoot(inCm, w.Path)
	outCms := make([][poseidonDigestLen]Felt, numOut)
	for j := 0; j < numOut; j++ {
		outCms[j] = SpendCommit(w.Outs[j].Value, w.Outs[j].OwnerTag, w.Outs[j].Rho)
	}

	// États d'entrée natifs des blocs.
	inOwner := spOwnerTagState(w.Nk)
	inInCm := spCommitState(w.InValue, ownerTag, w.InRho)
	inNf := spHash2State(w.Nk, inCm)
	var memIn [spendDepth][pfStateCols]Felt
	child := inCm
	for lvl := 0; lvl < spendDepth; lvl++ {
		memIn[lvl] = spReasmState(child, w.Path.Siblings[lvl], w.Path.Bits[lvl])
		child = spChainStep(child, w.Path.Siblings[lvl], w.Path.Bits[lvl])
	}
	outIn := make([][pfStateCols]Felt, numOut)
	for j := 0; j < numOut; j++ {
		outIn[j] = spCommitState(w.Outs[j].Value, w.Outs[j].OwnerTag, w.Outs[j].Rho)
	}

	memL := spBlkMem0 + spendDepth - 1

	// Remplissage des blocs réels.
	spFillBlockN(trace, spBlkOwner, inOwner, fee, numOut)
	spFillBlockN(trace, spBlkInCm, inInCm, fee, numOut)
	spFillBlockN(trace, spBlkNf, inNf, fee, numOut)
	for lvl := 0; lvl < spendDepth; lvl++ {
		spFillBlockN(trace, spBlkMem0+lvl, memIn[lvl], fee, numOut)
	}
	for j := 0; j < numOut; j++ {
		spFillBlockN(trace, snBlkOutput(j), outIn[j], fee, numOut)
	}
	// Blocs de complétion : état tenu (= sortie de la dernière sortie).
	lastOutState := outIn[numOut-1] // état d'ENTRÉE de la dernière sortie...
	// ... mais pour la complétion on veut l'état de SORTIE (row 31) du dernier bloc.
	// spFillBlockN a déjà rempli ; on relit la ligne de service du dernier bloc réel.
	realBlocks := snRealBlocks(numOut)
	if realBlocks < snTotalBlocks(numOut) {
		lastReal := snBlkOutput(numOut - 1)
		var carry [pfStateCols]Felt
		serviceRow := lastReal*spBlock + (spBlock - 1)
		for k := 0; k < pfStateCols; k++ {
			carry[k] = trace[serviceRow][spStateOff+k]
		}
		for b := realBlocks; b < snTotalBlocks(numOut); b++ {
			spFillBlockN(trace, b, carry, fee, numOut)
		}
	}
	_ = lastOutState

	// Chaînage des états de service (ligne 31 = entrée du bloc suivant).
	spOverwriteServiceState(trace, spBlkOwner, inInCm)
	spOverwriteServiceState(trace, spBlkInCm, inNf)
	spOverwriteServiceState(trace, spBlkNf, memIn[0])
	for lvl := 0; lvl < spendDepth-1; lvl++ {
		spOverwriteServiceState(trace, spBlkMem0+lvl, memIn[lvl+1])
	}
	spOverwriteServiceState(trace, memL, outIn[0]) // dernier mem -> sortie 0
	for j := 0; j < numOut-1; j++ {
		spOverwriteServiceState(trace, snBlkOutput(j), outIn[j+1]) // sortie j -> j+1
	}
	// dernière sortie -> complétion : la ligne 31 reste la sortie (identité OK).

	// Témoins de glue.
	// Glue ownerTag (mCommitS) : forme inCm, wVal=inValue (charge regVal).
	spSetWNote(trace[spOutputRowOf(spBlkOwner)], w.InRho, w.InValue)
	// Glue memL (mCommitW) : forme la sortie 0.
	spSetWTag(trace[spOutputRowOf(memL)], w.Outs[0].OwnerTag)
	spSetWNote(trace[spOutputRowOf(memL)], w.Outs[0].Rho, w.Outs[0].Value)
	// Glue sortie j (mCommitW) : forme la sortie j+1.
	for j := 0; j < numOut-1; j++ {
		row := spOutputRowOf(snBlkOutput(j))
		spSetWTag(trace[row], w.Outs[j+1].OwnerTag)
		spSetWNote(trace[row], w.Outs[j+1].Rho, w.Outs[j+1].Value)
	}
	// Siblings/bits de ré-assemblage.
	spSetSibBit(trace[spOutputRowOf(spBlkNf)], w.Path.Siblings[0], w.Path.Bits[0])
	for lvl := 0; lvl < spendDepth-1; lvl++ {
		spSetSibBit(trace[spOutputRowOf(spBlkMem0+lvl)], w.Path.Siblings[lvl+1], w.Path.Bits[lvl+1])
	}

	// Registres portés + accumulateur.
	spFillRegistersN(trace, w, inCm, numOut)

	public = SpendNPublic{MerkleRoot: merkleRoot, Nf: nf, OutCms: outCms, Fee: fee}
	return trace, public
}

// spFillRegistersN remplit regNk/regCm/regVal (comme le circuit 1-out, fenêtre
// regVal étendue jusqu'à la conservation) et l'accumulateur regAcc.
func spFillRegistersN(trace [][]Felt, w SpendNWitness, inCm [poseidonDigestLen]Felt, numOut int) {
	nfGlue := spOutputRowOf(spBlkNf)
	loadCmRow := spOutputRowOf(spBlkInCm)
	loadValRow := spOutputRowOf(spBlkOwner)
	consRow := snConsRow(numOut)
	steps := snSteps(numOut)
	memL := spBlkMem0 + spendDepth - 1

	// Lignes de glue mCommitW (ajout à l'accumulateur), dans l'ordre, avec la
	// valeur ajoutée : memL.out -> +outValue_0, sortie j.out -> +outValue_{j+1}.
	type addPt struct {
		row int
		val Felt
	}
	adds := make([]addPt, 0, numOut)
	adds = append(adds, addPt{spOutputRowOf(memL), w.Outs[0].Value})
	for j := 0; j < numOut-1; j++ {
		adds = append(adds, addPt{spOutputRowOf(snBlkOutput(j)), w.Outs[j+1].Value})
	}

	// regAcc[row] = somme des val dont la ligne d'ajout est STRICTEMENT < row
	// (l'ajout prend effet à la ligne suivante : next.regAcc = cur.regAcc + ...).
	accAt := func(row int) Felt {
		sum := Zero()
		for _, a := range adds {
			if a.row < row {
				sum = sum.Add(a.val)
			}
		}
		return sum
	}

	for row := 0; row < steps; row++ {
		if row <= nfGlue {
			for k := 0; k < poseidonDigestLen; k++ {
				trace[row][spRegNkOff+k] = w.Nk[k]
			}
		}
		if row >= loadCmRow && row <= nfGlue {
			for k := 0; k < poseidonDigestLen; k++ {
				trace[row][spRegCmOff+k] = inCm[k]
			}
		}
		if row >= loadValRow && row <= consRow {
			trace[row][spRegValCol] = w.InValue
		}
		trace[row][snRegAccCol] = accAt(row)
	}
}

// ---------------------------------------------------------------------------
// Prouveur / Vérifieur
// ---------------------------------------------------------------------------

// spendNPublicInputs : MerkleRoot(4) | Nf(4) | OutCm_0..(4·numOut) | Fee(1).
func spendNPublicInputs(public SpendNPublic) []Felt {
	out := make([]Felt, 0, 2*poseidonDigestLen+poseidonDigestLen*len(public.OutCms)+1)
	out = append(out, public.MerkleRoot[:]...)
	out = append(out, public.Nf[:]...)
	for j := range public.OutCms {
		out = append(out, public.OutCms[j][:]...)
	}
	out = append(out, public.Fee)
	return out
}

func spendNAirOf(public SpendNPublic) spendNAIR {
	return spendNAIR{
		merkleRoot: public.MerkleRoot,
		nf:         public.Nf,
		outCms:     public.OutCms,
		fee:        public.Fee,
		numOut:     len(public.OutCms),
	}
}

// ProveSpendN construit une preuve STARK de dépense 1-entrée / N-sorties. Atteste,
// sans révéler le témoin : inCm = Commit(inValue, Hash(Nk), inRho) ∈ arbre(root) ;
// Nf = Hash2(Nk, inCm) ; OutCm_j = Commit(outValue_j, ownerTag_j, rho_j) ;
// et la conservation inValue = Σ outValue_j + Fee.
func ProveSpendN(w SpendNWitness, fee Felt) (SpendNPublic, AirProof) {
	trace, public := buildSpendNTrace(w, fee)
	air := spendNAirOf(public)
	proof := ProveAIR(air, trace, spendNPublicInputs(public)...)
	return public, proof
}

// VerifySpendN vérifie une preuve produite par ProveSpendN pour l'énoncé `public`.
func VerifySpendN(public SpendNPublic, proof AirProof) bool {
	if len(public.OutCms) < 1 {
		return false
	}
	air := spendNAirOf(public)
	return VerifyAIR(air, proof, spendNPublicInputs(public)...)
}
