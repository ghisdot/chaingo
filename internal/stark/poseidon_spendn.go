// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// ÉTAGE 4.3 — Dépense blindée M-ENTRÉES / N-SORTIES (join-split généralisé).
//
// Généralise le circuit 1-in/1-out (poseidon_spend_air.go) à M entrées dépensées
// vers N sorties. Chaque ENTRÉE i reproduit la séquence éprouvée du circuit unitaire
// (ownerTag_i -> inCm_i -> nf_i -> appartenance_i) ; chaque SORTIE j est un bloc de
// commitment. Tous les blocs sont enchaînés linéairement par des modes de glue :
//
//   - mCommitS : forme inCm_i (tag = ownerTag courant) ET ajoute inValue_i à
//     l'accumulateur de conservation ;
//   - mHash2   : forme nf_i = Hash2(regNk_i, inCm_i) ;
//   - mReasmReg / mReasm : chaîne d'appartenance de l'entrée i ;
//   - mPackNk  : à la fin de l'entrée i (< M-1), CHARGE le témoin nk_{i+1} et forme
//     l'état pack(nk_{i+1}) = entrée du bloc ownerTag de l'entrée suivante ; c'est
//     le mode « charge-témoin » qui permet d'enchaîner des entrées dont la clé est
//     un secret frais (et non dérivée du bloc précédent) ;
//   - mCommitW : forme outCm_j ET retranche outValue_j de l'accumulateur.
//
// CONSERVATION (accumulateur SIGNÉ) : un registre acc cumule Σ inValue_i (aux glue
// mCommitS) MOINS Σ outValue_j (aux glue mCommitW) ; une contrainte finale impose
// acc == fee, soit Σ inValue_i == Σ outValue_j + fee. Un seul accumulateur traite
// donc symétriquement entrées et sorties.
//
// La hauteur (nombre de blocs · 32) est complétée en puissance de 2 par des blocs
// IDENTITÉ inertes après la dernière sortie.
//
// PORTÉE / GAPS : mêmes réserves que le circuit unitaire (profondeur d'arbre FIXE
// spendDepth, PAS de range proof — un wrap-around Goldilocks de la conservation
// reste un GAP documenté, trace non masquée, paramètres Poseidon non audités).
// L'unicité des nullifiers (anti double-dépense, y compris ENTRE les M entrées) est
// vérifiée HORS-circuit par le pool blindé.
//
// DÉTERMINISME ABSOLU : aucun time/rand ; tout l'aléa vient de ProveAIR/VerifyAIR.
package stark

// ---------------------------------------------------------------------------
// Disposition (réutilise le layout de colonnes du circuit unitaire + 3 colonnes)
// ---------------------------------------------------------------------------

const (
	// snInBlocks = blocs par ENTRÉE : ownerTag(1) + inCm(1) + nf(1) +
	// appartenance(spendDepth) = spendDepth + 3 (= 7 pour d=4).
	snInBlocks = spendDepth + 3

	// Colonnes ajoutées au layout du circuit unitaire (qui occupe [0, spNumCols)).
	snRegAccCol = spNumCols     // accumulateur SIGNÉ Σ inValue_i - Σ outValue_j (porté)
	snConsCol   = spNumCols + 1 // sélecteur PUBLIC : ligne de conservation finale
	snMPackNk   = spNumCols + 2 // mode de glue « charge-témoin » : pack(nk_{i+1})

	// RANGE-PROOFS : chaque valeur de note (in/out) est bornée à < 2^snRangeBits via
	// décomposition en bits. Cela ferme le wrap-around de la conservation dans le
	// corps Goldilocks (p ≈ 2^64) : avec toutes les valeurs < 2^48 et jusqu'à 128
	// notes par tx, Σ < 128·2^48 = 2^55 ≪ p — aucune somme ne peut « boucler » pour
	// créer de la valeur. 2^48 (~281 000 CGO à 9 décimales) couvre l'usage courant ;
	// les très grosses notes se fractionnent via le multi-sortie. Les bits vivent sur
	// la LIGNE DE GLUE de chaque valeur (où wVal porte la valeur) ; ils sont nuls
	// ailleurs.
	snRangeBits = 48
	snBitOff    = spNumCols + 3                // 1ère colonne de bits de range
	snNumCols   = snBitOff + snRangeBits        // largeur totale

	// Résidus : 12 (état) + 4 (regNk) + 4 (regCm) + 1 (binarité bit Merkle) +
	// 1 (accumulateur conservation) + 1 (conservation finale) + snRangeBits (binarité
	// de chaque bit de range) + 1 (reconstruction valeur = Σ bit·2^i).
	snNumResidues = pfStateCols + poseidonDigestLen + poseidonDigestLen + 1 + 1 + 1 + snRangeBits + 1
)

// snInputBase renvoie l'indice du premier bloc de l'entrée i.
func snInputBase(i int) int { return i * snInBlocks }

// snBlkOwner_/snBlkInCm_/snBlkNf_/snBlkMemL_ : indices de blocs de l'entrée i.
func snBlkOwnerI(i int) int { return snInputBase(i) }
func snBlkInCmI(i int) int  { return snInputBase(i) + 1 }
func snBlkNfI(i int) int    { return snInputBase(i) + 2 }
func snBlkMem0I(i int) int  { return snInputBase(i) + 3 }
func snBlkMemLI(i int) int  { return snInputBase(i) + 3 + spendDepth - 1 }

// snOutputBase renvoie l'indice du premier bloc de SORTIE (après les M entrées).
func snOutputBase(numIn int) int { return numIn * snInBlocks }

// snBlkOutputJ renvoie l'indice de bloc de la sortie j.
func snBlkOutputJ(numIn, j int) int { return snOutputBase(numIn) + j }

// snRealBlocks / snTotalBlocks / snSteps : tailles (avant et après complétion).
func snRealBlocks(numIn, numOut int) int { return numIn*snInBlocks + numOut }
func snTotalBlocks(numIn, numOut int) int {
	return nextPow2(snRealBlocks(numIn, numOut))
}
func snSteps(numIn, numOut int) int { return snTotalBlocks(numIn, numOut) * spBlock }

// snConsRow : ligne de conservation = ligne de sortie de la DERNIÈRE sortie.
func snConsRow(numIn, numOut int) int {
	return spOutputRowOf(snBlkOutputJ(numIn, numOut-1))
}

// ---------------------------------------------------------------------------
// Témoin / énoncé
// ---------------------------------------------------------------------------

// SpendNIn décrit UNE note d'entrée dépensée (témoin privé).
type SpendNIn struct {
	Value Felt
	Rho   [poseidonDigestLen]Felt
	Nk    [poseidonDigestLen]Felt
	Path  SpendPath
}

// SpendNOut décrit UNE note de sortie (témoin privé).
type SpendNOut struct {
	Value    Felt
	OwnerTag [poseidonDigestLen]Felt
	Rho      [poseidonDigestLen]Felt
}

// SpendNWitness est le témoin d'une dépense M-entrées / N-sorties (M,N >= 1).
type SpendNWitness struct {
	Ins  []SpendNIn
	Outs []SpendNOut
}

// SpendNPublic est l'énoncé public : racine commune, un nullifier par entrée, un
// engagement par sortie, et les frais.
type SpendNPublic struct {
	MerkleRoot [poseidonDigestLen]Felt
	Nfs        [][poseidonDigestLen]Felt
	OutCms     [][poseidonDigestLen]Felt
	Fee        Felt
}

// ---------------------------------------------------------------------------
// AIR
// ---------------------------------------------------------------------------

type spendNAIR struct {
	merkleRoot [poseidonDigestLen]Felt
	nfs        [][poseidonDigestLen]Felt
	outCms     [][poseidonDigestLen]Felt
	fee        Felt
	numIn      int
	numOut     int
}

func (a spendNAIR) NumColumns() int { return snNumCols }
func (a spendNAIR) NumSteps() int   { return snSteps(a.numIn, a.numOut) }
func (a spendNAIR) MaxDegree() int  { return spMaxDegree }

// EvalTransition : branches ronde/glue/registres du circuit unitaire, étendues du
// mode mPackNk (charge-témoin) et de l'accumulateur signé de conservation.
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
	mPackNk := cur[snMPackNk]

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
	ownerSep := FromUint64(spOwnerSep)

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

	// mPackNk : pack(nk) = [ wTag(nk)(4) | 0(4) | spOwnerSep | 0 | 0 | 0 ].
	var packNk [pfStateCols]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		packNk[k] = wTag[k]
	}
	packNk[poseidonRate] = ownerSep

	sumModes := mCommitS.Add(mHash2).Add(mReasmReg).Add(mReasm).Add(mCommitW).Add(mPackNk)
	idfac := one.Sub(active).Sub(sumModes)

	residues := make([]Felt, snNumResidues)

	// 12 résidus d'état.
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
			Add(mPackNk.Mul(packNk[k])).
			Add(idfac.Mul(cur[spStateOff+k]))
		residues[k] = next[spStateOff+k].Sub(outk)
	}

	// Registres regNk / regCm (load/hold par entrée).
	loadNk := cur[spLoadNk]
	holdNk := cur[spHoldNk]
	loadCm := cur[spLoadCm]
	holdCm := cur[spHoldCm]

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

	// Binarité du bit (lignes de ré-assemblage).
	reasmAny := mReasmReg.Add(mReasm)
	residues[idx] = reasmAny.Mul(bit).Mul(bit.Sub(one))
	idx++

	// ACCUMULATEUR SIGNÉ : next.acc = cur.acc + mCommitS·wVal - mCommitW·wVal.
	// Chaque commit d'ENTRÉE (mCommitS, wVal=inValue_i) ajoute, chaque commit de
	// SORTIE (mCommitW, wVal=outValue_j) retranche. Tenu partout ailleurs.
	delta := mCommitS.Mul(wVal).Sub(mCommitW.Mul(wVal))
	residues[idx] = next[snRegAccCol].Sub(cur[snRegAccCol]).Sub(delta)
	idx++

	// CONSERVATION : sur la ligne finale (snCons=1), acc doit valoir fee, soit
	// Σ inValue_i - Σ outValue_j == fee.
	cons := cur[snConsCol]
	residues[idx] = cons.Mul(cur[snRegAccCol].Sub(cur[spFeeCol]))
	idx++

	// RANGE-PROOF de la valeur de note, gated par les lignes de glue qui PORTENT une
	// valeur (mCommitS pour une entrée, mCommitW pour une sortie). Sur ces lignes,
	// wVal doit être < 2^snRangeBits, décomposé en bits snBitOff..+snRangeBits-1 :
	//   - chaque bit est BINAIRE : valSel·b·(b-1) = 0 ;
	//   - RECONSTRUCTION : valSel·(wVal - Σ b_i·2^i) = 0.
	// Hors lignes de glue de valeur (valSel=0), les bits sont libres (mis à 0 par la
	// trace) et non contraints. Borner ainsi toutes les valeurs ferme le wrap-around
	// de la conservation dans Goldilocks.
	valSel := mCommitS.Add(mCommitW)
	recon := Zero()
	pow := one
	for i := 0; i < snRangeBits; i++ {
		b := cur[snBitOff+i]
		residues[idx] = valSel.Mul(b).Mul(b.Sub(one)) // binarité du bit
		idx++
		recon = recon.Add(b.Mul(pow))
		pow = pow.Add(pow) // 2^(i+1)
	}
	residues[idx] = valSel.Mul(wVal.Sub(recon)) // wVal == Σ b_i·2^i
	idx++

	return residues
}

// Boundaries : structure par ligne, capacités des blocs (entrées + sorties),
// init acc=0, et sorties publiques (nf_i, racine_i == merkleRoot, outCm_j).
func (a spendNAIR) Boundaries() []Boundary {
	steps := a.NumSteps()
	bs := make([]Boundary, 0, steps*18)

	for row := 0; row < steps; row++ {
		st := spendNRowStructure(row, a.fee, a.numIn, a.numOut)
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
		bs = append(bs, Boundary{Col: snMPackNk, Row: row, Value: st.mPackNk})
		bs = append(bs, Boundary{Col: spLoadNk, Row: row, Value: st.loadNk})
		bs = append(bs, Boundary{Col: spHoldNk, Row: row, Value: st.holdNk})
		bs = append(bs, Boundary{Col: spLoadCm, Row: row, Value: st.loadCm})
		bs = append(bs, Boundary{Col: spHoldCm, Row: row, Value: st.holdCm})
		bs = append(bs, Boundary{Col: spFeeCol, Row: row, Value: st.fee})
		bs = append(bs, Boundary{Col: snConsCol, Row: row, Value: st.cons})
	}

	// Capacités par ENTRÉE.
	for i := 0; i < a.numIn; i++ {
		addCap(&bs, snBlkOwnerI(i)*spBlock, FromUint64(spOwnerSep), Zero(), Zero(), Zero())
		addCapPartial(&bs, snBlkInCmI(i)*spBlock)
		addCap(&bs, snBlkNfI(i)*spBlock, FromUint64(poseidonDomainSep), FromUint64(uint64(poseidonRate)), Zero(), Zero())
		for lvl := 0; lvl < spendDepth; lvl++ {
			addCap(&bs, (snBlkMem0I(i)+lvl)*spBlock, FromUint64(poseidonDomainSep), FromUint64(uint64(poseidonRate)), Zero(), Zero())
		}
	}
	// Capacités de chaque bloc de SORTIE.
	for j := 0; j < a.numOut; j++ {
		addCapPartial(&bs, snBlkOutputJ(a.numIn, j)*spBlock)
	}

	// Init de l'accumulateur : acc == 0 à la ligne 0.
	bs = append(bs, Boundary{Col: snRegAccCol, Row: 0, Value: Zero()})

	// Sorties publiques.
	for k := 0; k < poseidonDigestLen; k++ {
		for i := 0; i < a.numIn; i++ {
			// Nullifier de l'entrée i (sortie du bloc nf_i).
			bs = append(bs, Boundary{Col: spStateOff + k, Row: spOutputRowOf(snBlkNfI(i)), Value: a.nfs[i][k]})
			// Racine d'appartenance de l'entrée i (== racine commune).
			bs = append(bs, Boundary{Col: spStateOff + k, Row: spOutputRowOf(snBlkMemLI(i)), Value: a.merkleRoot[k]})
		}
		for j := 0; j < a.numOut; j++ {
			bs = append(bs, Boundary{Col: spStateOff + k, Row: spOutputRowOf(snBlkOutputJ(a.numIn, j)), Value: a.outCms[j][k]})
		}
	}

	return bs
}

// ---------------------------------------------------------------------------
// Structure par ligne
// ---------------------------------------------------------------------------

type spendNStructure struct {
	rc                                                     [pfStateCols]Felt
	fsel, active                                           Felt
	mCommitS, mHash2, mReasmReg, mReasm, mCommitW, mPackNk Felt
	loadNk, holdNk, loadCm, holdCm                         Felt
	fee, cons                                              Felt
}

func spendNRowStructure(row int, fee Felt, numIn, numOut int) spendNStructure {
	var s spendNStructure
	block := row / spBlock
	r := row % spBlock
	realBlocks := snRealBlocks(numIn, numOut)
	outputBase := snOutputBase(numIn)

	// Blocs de complétion (identité).
	if block >= realBlocks {
		return s
	}

	// Rondes Poseidon réelles (0..29).
	if r < poseidonTotalRounds {
		s.rc = params.roundConstants[r]
		s.active = One()
		if pfIsFullRound(r) {
			s.fsel = One()
		}
	}

	// Classification du bloc.
	isInput := block < outputBase
	var inI, within, outJ int
	if isInput {
		inI = block / snInBlocks
		within = block % snInBlocks // 0=owner,1=inCm,2=nf,3..=mem
	} else {
		outJ = block - outputBase
	}

	// Glue sur la ligne de sortie.
	if r == pfOutputRow {
		switch {
		case isInput && within == 0:
			s.mCommitS = One() // ownerTag_i -> inCm_i (+inValue_i)
		case isInput && within == 1:
			s.mHash2 = One() // inCm_i -> nf_i
		case isInput && within == 2:
			s.mReasmReg = One() // nf_i -> appartenance L0 (child = regCm_i)
		case isInput && within >= 3 && within < snInBlocks-1:
			s.mReasm = One() // niveau d'appartenance l -> l+1
		case isInput && within == snInBlocks-1:
			// dernier niveau d'appartenance de l'entrée i.
			if inI < numIn-1 {
				s.mPackNk = One() // -> ownerTag de l'entrée i+1 (charge nk_{i+1})
			} else {
				s.mCommitW = One() // dernière entrée -> sortie 0 (-outValue_0)
			}
		case !isInput && outJ < numOut-1:
			s.mCommitW = One() // sortie j -> sortie j+1 (-outValue_{j+1})
		case !isInput && outJ == numOut-1:
			// dernière sortie : pas de glue (conservation ici).
		}
	}

	// Registres regNk / regCm, par ENTRÉE.
	if isInput {
		ownerRow0 := snBlkOwnerI(inI) * spBlock
		inCmOutRow := spOutputRowOf(snBlkInCmI(inI))
		nfOutRow := spOutputRowOf(snBlkNfI(inI))

		// regNk_i : chargé à la ligne 0 du bloc ownerTag_i, tenu jusqu'à la ligne
		// mHash2 (sortie inCm_i) où il est relu.
		if row == ownerRow0 {
			s.loadNk = One()
		}
		if row >= ownerRow0 && row < inCmOutRow {
			s.holdNk = One()
		}
		// regCm_i : chargé en sortie inCm_i, tenu jusqu'à la ligne mReasmReg
		// (sortie nf_i) où il est relu.
		if row == inCmOutRow {
			s.loadCm = One()
		}
		if row >= inCmOutRow && row < nfOutRow {
			s.holdCm = One()
		}
	}

	// fee + conservation sur la ligne finale.
	consRow := snConsRow(numIn, numOut)
	if row == consRow {
		s.fee = fee
		s.cons = One()
	}

	return s
}

// ---------------------------------------------------------------------------
// Construction de la trace
// ---------------------------------------------------------------------------

func spFillBlockN(trace [][]Felt, b int, in [pfStateCols]Felt, fee Felt, numIn, numOut int) {
	base := b * spBlock
	s := in
	for r := 0; r < spBlock; r++ {
		row := base + r
		st := spendNRowStructure(row, fee, numIn, numOut)
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
		line[snMPackNk] = st.mPackNk
		line[spLoadNk] = st.loadNk
		line[spHoldNk] = st.holdNk
		line[spLoadCm] = st.loadCm
		line[spHoldCm] = st.holdCm
		line[spFeeCol] = st.fee
		line[snConsCol] = st.cons
		trace[row] = line

		if r < poseidonTotalRounds && !st.active.IsZero() {
			s = pfApplyRound(s, st.rc, !st.fsel.IsZero())
		}
	}
}

// snOverwriteService écrase l'état de la ligne de service (31) du bloc b par nextIn.
func snOverwriteService(trace [][]Felt, b int, nextIn [pfStateCols]Felt) {
	row := b*spBlock + (spBlock - 1)
	for k := 0; k < pfStateCols; k++ {
		trace[row][spStateOff+k] = nextIn[k]
	}
}

// buildSpendNTrace construit la trace satisfaisante M-entrées/N-sorties.
func buildSpendNTrace(w SpendNWitness, fee Felt) (trace [][]Felt, public SpendNPublic) {
	numIn := len(w.Ins)
	numOut := len(w.Outs)
	if numIn < 1 || numOut < 1 {
		panic("stark: buildSpendNTrace: au moins une entrée et une sortie")
	}
	for i := 0; i < numIn; i++ {
		for l := 0; l < spendDepth; l++ {
			if !(w.Ins[i].Path.Bits[l].IsZero() || w.Ins[i].Path.Bits[l].Equal(One())) {
				panic("stark: buildSpendNTrace: bit de direction non binaire")
			}
		}
	}

	steps := snSteps(numIn, numOut)
	trace = make([][]Felt, steps)

	// Valeurs natives par entrée.
	ownerTag := make([][poseidonDigestLen]Felt, numIn)
	inCm := make([][poseidonDigestLen]Felt, numIn)
	nfs := make([][poseidonDigestLen]Felt, numIn)
	var merkleRoot [poseidonDigestLen]Felt
	inOwnerState := make([][pfStateCols]Felt, numIn)
	inInCmState := make([][pfStateCols]Felt, numIn)
	inNfState := make([][pfStateCols]Felt, numIn)
	memIn := make([][spendDepth][pfStateCols]Felt, numIn)
	for i := 0; i < numIn; i++ {
		in := w.Ins[i]
		ownerTag[i] = SpendOwnerTag(in.Nk)
		inCm[i] = SpendCommit(in.Value, ownerTag[i], in.Rho)
		nfs[i] = SpendNullifier(in.Nk, inCm[i])
		root := spChainRoot(inCm[i], in.Path)
		if i == 0 {
			merkleRoot = root
		}
		inOwnerState[i] = spOwnerTagState(in.Nk)
		inInCmState[i] = spCommitState(in.Value, ownerTag[i], in.Rho)
		inNfState[i] = spHash2State(in.Nk, inCm[i])
		child := inCm[i]
		for lvl := 0; lvl < spendDepth; lvl++ {
			memIn[i][lvl] = spReasmState(child, in.Path.Siblings[lvl], in.Path.Bits[lvl])
			child = spChainStep(child, in.Path.Siblings[lvl], in.Path.Bits[lvl])
		}
	}

	// Valeurs natives par sortie.
	outCms := make([][poseidonDigestLen]Felt, numOut)
	outState := make([][pfStateCols]Felt, numOut)
	for j := 0; j < numOut; j++ {
		o := w.Outs[j]
		outCms[j] = SpendCommit(o.Value, o.OwnerTag, o.Rho)
		outState[j] = spCommitState(o.Value, o.OwnerTag, o.Rho)
	}

	// Remplissage des blocs réels.
	for i := 0; i < numIn; i++ {
		spFillBlockN(trace, snBlkOwnerI(i), inOwnerState[i], fee, numIn, numOut)
		spFillBlockN(trace, snBlkInCmI(i), inInCmState[i], fee, numIn, numOut)
		spFillBlockN(trace, snBlkNfI(i), inNfState[i], fee, numIn, numOut)
		for lvl := 0; lvl < spendDepth; lvl++ {
			spFillBlockN(trace, snBlkMem0I(i)+lvl, memIn[i][lvl], fee, numIn, numOut)
		}
	}
	for j := 0; j < numOut; j++ {
		spFillBlockN(trace, snBlkOutputJ(numIn, j), outState[j], fee, numIn, numOut)
	}
	// Complétion identité.
	realBlocks := snRealBlocks(numIn, numOut)
	totalBlocks := snTotalBlocks(numIn, numOut)
	if realBlocks < totalBlocks {
		lastReal := snBlkOutputJ(numIn, numOut-1)
		var carry [pfStateCols]Felt
		serviceRow := lastReal*spBlock + (spBlock - 1)
		for k := 0; k < pfStateCols; k++ {
			carry[k] = trace[serviceRow][spStateOff+k]
		}
		for b := realBlocks; b < totalBlocks; b++ {
			spFillBlockN(trace, b, carry, fee, numIn, numOut)
		}
	}

	// Chaînage des états de service (entrée du bloc suivant).
	for i := 0; i < numIn; i++ {
		snOverwriteService(trace, snBlkOwnerI(i), inInCmState[i]) // owner -> inCm
		snOverwriteService(trace, snBlkInCmI(i), inNfState[i])    // inCm -> nf
		snOverwriteService(trace, snBlkNfI(i), memIn[i][0])       // nf -> mem0
		for lvl := 0; lvl < spendDepth-1; lvl++ {
			snOverwriteService(trace, snBlkMem0I(i)+lvl, memIn[i][lvl+1])
		}
		// memL_i -> bloc suivant : ownerTag de l'entrée i+1, ou sortie 0.
		if i < numIn-1 {
			snOverwriteService(trace, snBlkMemLI(i), inOwnerState[i+1])
		} else {
			snOverwriteService(trace, snBlkMemLI(i), outState[0])
		}
	}
	for j := 0; j < numOut-1; j++ {
		snOverwriteService(trace, snBlkOutputJ(numIn, j), outState[j+1])
	}

	// Témoins de glue.
	for i := 0; i < numIn; i++ {
		// Glue mCommitS (sortie ownerTag_i) : forme inCm_i, wVal = inValue_i.
		spSetWNote(trace[spOutputRowOf(snBlkOwnerI(i))], w.Ins[i].Rho, w.Ins[i].Value)
		spSetRangeBits(trace[spOutputRowOf(snBlkOwnerI(i))], w.Ins[i].Value) // range-proof inValue_i
		// Glue mReasmReg (sortie nf_i) : sibling/bit du niveau 0.
		spSetSibBit(trace[spOutputRowOf(snBlkNfI(i))], w.Ins[i].Path.Siblings[0], w.Ins[i].Path.Bits[0])
		// Glue mReasm (sorties mem niveaux 0..depth-2) : sibling/bit du niveau l+1.
		for lvl := 0; lvl < spendDepth-1; lvl++ {
			spSetSibBit(trace[spOutputRowOf(snBlkMem0I(i)+lvl)], w.Ins[i].Path.Siblings[lvl+1], w.Ins[i].Path.Bits[lvl+1])
		}
		// Glue mPackNk (sortie memL_i, i<numIn-1) : charge nk_{i+1} dans wTag.
		if i < numIn-1 {
			spSetWTag(trace[spOutputRowOf(snBlkMemLI(i))], w.Ins[i+1].Nk)
		}
	}
	// Glue mCommitW : memL_{last} -> sortie 0, puis sortie j -> sortie j+1.
	spSetWTag(trace[spOutputRowOf(snBlkMemLI(numIn-1))], w.Outs[0].OwnerTag)
	spSetWNote(trace[spOutputRowOf(snBlkMemLI(numIn-1))], w.Outs[0].Rho, w.Outs[0].Value)
	spSetRangeBits(trace[spOutputRowOf(snBlkMemLI(numIn-1))], w.Outs[0].Value) // range-proof outValue_0
	for j := 0; j < numOut-1; j++ {
		row := spOutputRowOf(snBlkOutputJ(numIn, j))
		spSetWTag(trace[row], w.Outs[j+1].OwnerTag)
		spSetWNote(trace[row], w.Outs[j+1].Rho, w.Outs[j+1].Value)
		spSetRangeBits(trace[row], w.Outs[j+1].Value) // range-proof outValue_{j+1}
	}

	// Registres + accumulateur.
	spFillRegistersN(trace, w, inCm, numIn, numOut)

	public = SpendNPublic{MerkleRoot: merkleRoot, Nfs: nfs, OutCms: outCms, Fee: fee}
	return trace, public
}

// spSetRangeBits pose la décomposition en bits (LSB d'abord) de `value` sur les
// colonnes de range [snBitOff, snBitOff+snRangeBits) d'une ligne de glue de valeur.
// La valeur honnête est < 2^snRangeBits (sinon la reconstruction la rejetterait).
func spSetRangeBits(line []Felt, value Felt) {
	v := value.Uint64()
	for i := 0; i < snRangeBits; i++ {
		if (v>>uint(i))&1 == 1 {
			line[snBitOff+i] = One()
		} else {
			line[snBitOff+i] = Zero()
		}
	}
}

// spFillRegistersN remplit regNk/regCm par entrée et l'accumulateur signé.
func spFillRegistersN(trace [][]Felt, w SpendNWitness, inCm [][poseidonDigestLen]Felt, numIn, numOut int) {
	steps := snSteps(numIn, numOut)

	// Fenêtres regNk_i / regCm_i.
	for i := 0; i < numIn; i++ {
		ownerRow0 := snBlkOwnerI(i) * spBlock
		inCmOutRow := spOutputRowOf(snBlkInCmI(i))
		nfOutRow := spOutputRowOf(snBlkNfI(i))
		for row := ownerRow0; row <= inCmOutRow; row++ {
			for k := 0; k < poseidonDigestLen; k++ {
				trace[row][spRegNkOff+k] = w.Ins[i].Nk[k]
			}
		}
		for row := inCmOutRow; row <= nfOutRow; row++ {
			for k := 0; k < poseidonDigestLen; k++ {
				trace[row][spRegCmOff+k] = inCm[i][k]
			}
		}
	}

	// Accumulateur signé : points d'ajout (mCommitS, +inValue_i) et de retrait
	// (mCommitW, -outValue_j), dans l'ordre des lignes de glue.
	type pt struct {
		row int
		val Felt // signée (positive=ajout, négative=retrait)
	}
	pts := make([]pt, 0, numIn+numOut)
	for i := 0; i < numIn; i++ {
		pts = append(pts, pt{spOutputRowOf(snBlkOwnerI(i)), w.Ins[i].Value})
	}
	// Retraits : memL_last -> sortie0 (-outValue_0), sortie j -> j+1 (-outValue_{j+1}).
	pts = append(pts, pt{spOutputRowOf(snBlkMemLI(numIn - 1)), Zero().Sub(w.Outs[0].Value)})
	for j := 0; j < numOut-1; j++ {
		pts = append(pts, pt{spOutputRowOf(snBlkOutputJ(numIn, j)), Zero().Sub(w.Outs[j+1].Value)})
	}

	// acc[row] = somme des val dont la ligne d'ajout/retrait est STRICTEMENT < row.
	for row := 0; row < steps; row++ {
		sum := Zero()
		for _, p := range pts {
			if p.row < row {
				sum = sum.Add(p.val)
			}
		}
		trace[row][snRegAccCol] = sum
	}
}

// ---------------------------------------------------------------------------
// Prouveur / Vérifieur
// ---------------------------------------------------------------------------

// spendNPublicInputs : MerkleRoot(4) | Nf_0..(4·numIn) | OutCm_0..(4·numOut) | Fee.
func spendNPublicInputs(public SpendNPublic) []Felt {
	out := make([]Felt, 0, poseidonDigestLen*(1+len(public.Nfs)+len(public.OutCms))+1)
	out = append(out, public.MerkleRoot[:]...)
	for i := range public.Nfs {
		out = append(out, public.Nfs[i][:]...)
	}
	for j := range public.OutCms {
		out = append(out, public.OutCms[j][:]...)
	}
	out = append(out, public.Fee)
	return out
}

func spendNAirOf(public SpendNPublic) spendNAIR {
	return spendNAIR{
		merkleRoot: public.MerkleRoot,
		nfs:        public.Nfs,
		outCms:     public.OutCms,
		fee:        public.Fee,
		numIn:      len(public.Nfs),
		numOut:     len(public.OutCms),
	}
}

// ProveSpendN construit une preuve STARK de dépense M-entrées / N-sorties. Atteste,
// sans révéler le témoin : pour chaque entrée i, inCm_i = Commit(inValue_i,
// Hash(Nk_i), inRho_i) ∈ arbre(root) et Nf_i = Hash2(Nk_i, inCm_i) ; pour chaque
// sortie j, OutCm_j = Commit(outValue_j, ownerTag_j, rho_j) ; et la conservation
// Σ inValue_i = Σ outValue_j + Fee.
func ProveSpendN(w SpendNWitness, fee Felt) (SpendNPublic, AirProof) {
	trace, public := buildSpendNTrace(w, fee)
	air := spendNAirOf(public)
	proof := ProveAIR(air, trace, spendNPublicInputs(public)...)
	return public, proof
}

// VerifySpendN vérifie une preuve produite par ProveSpendN pour l'énoncé `public`.
func VerifySpendN(public SpendNPublic, proof AirProof) bool {
	if len(public.Nfs) < 1 || len(public.OutCms) < 1 {
		return false
	}
	air := spendNAirOf(public)
	return VerifyAIR(air, proof, spendNPublicInputs(public)...)
}
