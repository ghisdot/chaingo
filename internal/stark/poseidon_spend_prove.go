// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// ÉTAGE 4.2 — Construction de la TRACE-témoin + prouveur/vérifieur de haut niveau
// du circuit de DÉPENSE blindée (1 entrée / 1 sortie + frais). Voir
// poseidon_spend.go (énoncé, bandeau de portée, définitions natives de la note)
// et poseidon_spend_air.go (arithmétisation : layout, EvalTransition, Boundaries,
// spendRowStructure). Ce fichier est ADDITIF : il n'écrit AUCUN fichier existant,
// il les APPELLE.
//
// ┌──────────────────────────────────────────────────────────────────────────┐
// │ COMMENT LES CONTRAINTES SONT LIÉES (témoin partagé engagé) — ESSENTIEL.    │
// │                                                                            │
// │ Tout est UNE seule preuve STARK sur UNE seule trace (pas de composition de │
// │ preuves séparées). Les 5 sous-énoncés partagent leur témoin via la MÊME    │
// │ trace et trois registres portés constants (regNk, regCm, regVal) :         │
// │                                                                            │
// │  - nk est chargé (regNk) à la ligne 0 (= rate du bloc ownerTag) ET sert    │
// │    d'entrée au bloc ownerTag ; le MÊME nk est relu au glue du bloc nf.     │
// │    => le ownerTag prouvé et le nf prouvé dérivent du MÊME nk : un attaquant │
// │       ne peut PAS produire un nf avec un nk' != nk tout en gardant le       │
// │       ownerTag de la note (1bis lie nk -> ownerTag, 3 lie nk -> nf).        │
// │  - inCm est la sortie du bloc inCm ; il est chargé (regCm) puis relu au     │
// │    glue d'entrée de l'appartenance (child du niveau 0) ET sert (comme child │
// │    courant) à former le nf. => le nf et l'appartenance portent sur le MÊME  │
// │    inCm que celui formé par Commit(inValue, ownerTag, inRho).               │
// │  - inValue est chargé (regVal) au glue du bloc inCm (wVal de la note        │
// │    d'entrée) puis relu au glue du bloc outCm où la contrainte de            │
// │    conservation impose regVal == outValue + fee.                            │
// │                                                                            │
// │ Comme les registres sont des COLONNES de la trace engagée et que leurs      │
// │ fenêtres load/hold sont des bords PUBLICS (spendRowStructure), le témoin    │
// │ est « engagé » : on ne peut pas réutiliser deux valeurs différentes de nk / │
// │ inCm / inValue selon le sous-énoncé.                                        │
// │                                                                            │
// │ PORTÉE / GAPS (rappel) : profondeur d'arbre FIXE spendDepth=4 ; 1-in/1-out ;│
// │ PAS de range proof (la conservation est dans Goldilocks, un wrap-around     │
// │ théorique reste un GAP documenté) ; trace NON masquée (ZK = « non publié », │
// │ pas de randomized LDE). Paramètres Poseidon NON audités. Hors-consensus.    │
// └──────────────────────────────────────────────────────────────────────────┘
//
// DÉTERMINISME ABSOLU : aucun time, aucun math/rand ; tout l'aléa du protocole
// vient du transcript Fiat-Shamir via ProveAIR/VerifyAIR.
package stark

// ---------------------------------------------------------------------------
// Témoin (privé) et énoncé (public)
// ---------------------------------------------------------------------------

// SpendWitness est le TÉMOIN PRIVÉ d'une dépense blindée 1-entrée/1-sortie. Aucun
// de ses champs n'est publié : ni en clair, ni comme valeur publique du
// transcript (voir SpendPublic pour ce qui est révélé).
//
// Note d'entrée :
//   - InValue : montant de la note dépensée.
//   - InRho   : aléa (« randomness ») du commitment d'entrée.
//   - Nk      : clé de nullifier (le propriétaire EST le détenteur de Nk).
//   - Path    : chemin d'authentification Merkle de inCm vers MerkleRoot
//     (siblings + bits, profondeur spendDepth). Le bit du niveau i suit la même
//     convention que PoseidonVerifyPath (LSB de l'indice au niveau i).
//
// Note de sortie :
//   - OutValue    : montant de la note créée.
//   - OutOwnerTag : tag de propriétaire du destinataire (= PoseidonHash(nk_dest),
//     opaque au prouveur ; c'est l'adresse blindée du bénéficiaire).
//   - OutRho      : aléa du commitment de sortie.
//
// L'engagement d'entrée inCm = Commit(InValue, PoseidonHash(Nk), InRho) et le
// ownerTag ne sont PAS fournis : ils sont RECALCULÉS par la trace (cohérence).
type SpendWitness struct {
	// --- Note d'entrée ---
	InValue Felt
	InRho   [poseidonDigestLen]Felt
	Nk      [poseidonDigestLen]Felt
	Path    SpendPath

	// --- Note de sortie ---
	OutValue    Felt
	OutOwnerTag [poseidonDigestLen]Felt
	OutRho      [poseidonDigestLen]Felt
}

// SpendPath est le chemin d'authentification Merkle (profondeur spendDepth) de
// l'engagement d'entrée inCm vers la racine. Même convention de bits que
// MembershipPath / PoseidonVerifyPath. Les bits DOIVENT valoir 0 ou 1.
type SpendPath struct {
	Siblings [spendDepth][poseidonDigestLen]Felt
	Bits     [spendDepth]Felt
}

// SpendPublic est l'ÉNONCÉ PUBLIC d'une dépense : les seules valeurs révélées.
//   - MerkleRoot : racine de l'arbre des notes (anti hors-arbre).
//   - Nf         : nullifier (anti double-dépense ; unicité vérifiée hors-circuit).
//   - OutCm      : engagement de la note de sortie (publié pour insertion en arbre).
//   - Fee        : frais (montant brûlé/payé, public pour le consensus).
type SpendPublic struct {
	MerkleRoot [poseidonDigestLen]Felt
	Nf         [poseidonDigestLen]Felt
	OutCm      [poseidonDigestLen]Felt
	Fee        Felt
}

// SpendDepth renvoie la profondeur (fixe) d'arbre d'appartenance du circuit de
// dépense. Exposé pour les appelants (taille attendue du chemin / nb de feuilles).
func SpendDepth() int { return spendDepth }

// RangeBits renvoie le nombre de bits sur lequel chaque valeur de note (et le
// montant public d'un shield/unshield) est bornée par les range-proofs du
// circuit M-in/N-out. Une valeur >= 2^RangeBits n'admet AUCUNE preuve valide :
// la couche état doit donc refuser de tels montants (sinon note indépensable).
func RangeBits() uint { return snRangeBits }

// MaxNoteValue renvoie la borne stricte (exclue) des valeurs de note prouvables :
// toute valeur honnête doit vérifier value < MaxNoteValue.
func MaxNoteValue() uint64 { return uint64(1) << snRangeBits }

// ---------------------------------------------------------------------------
// Construction de la trace-témoin
// ---------------------------------------------------------------------------

// spFillBlock remplit les 32 lignes d'un bloc Poseidon (base = b·32) à partir de
// l'état d'entrée `in`. Il calcule l'état réel ronde par ronde (via pfApplyRound,
// exactement comme poseidon_air_full.go), pose les colonnes de structure de
// chaque ligne (spendRowStructure) et renvoie l'état de SORTIE (12 Felt, ligne
// d'indice 30 du bloc). Les colonnes-témoins (sib/bit/regs/wTag/wRho/wVal) ne sont
// PAS posées ici : elles sont remplies après coup par buildSpendTrace selon le
// rôle de chaque ligne (glue, load/hold de registre).
//
// Les lignes 0..30 portent l'état avant/après rondes ; la ligne 31 (service)
// recopie l'état de sortie (identité), de sorte que la transition 31 -> bloc
// suivant ligne 0 reste cohérente lorsque le glue n'écrase pas l'état (dernier
// bloc) ; pour les blocs non-finaux, buildSpendTrace ÉCRASE l'état des lignes 31
// (et la ligne 0 du bloc suivant) par la sortie du glue.
func spFillBlock(trace [][]Felt, b int, in [pfStateCols]Felt, fee Felt) (output [pfStateCols]Felt) {
	base := b * spBlock
	s := in
	for r := 0; r < spBlock; r++ {
		row := base + r
		st := spendRowStructure(row, fee)
		line := make([]Felt, spNumCols)
		for k := 0; k < pfStateCols; k++ {
			line[spStateOff+k] = s[k]
			line[spRcOff+k] = st.rc[k]
		}
		// Colonnes de structure (publiques).
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
		trace[row] = line

		// Avance de l'état À L'INTÉRIEUR du bloc : rondes réelles 0..29.
		if r < poseidonTotalRounds {
			s = pfApplyRound(s, st.rc, !st.fsel.IsZero())
		}
		// r==30 (sortie) -> r==31 (service) : identité (on garde s = sortie).
		// r==31 -> bloc suivant : géré par buildSpendTrace (glue).
	}
	return s
}

// buildSpendTrace construit la trace COMPLÈTE et SATISFAISANTE du circuit de
// dépense pour le témoin `w` et les frais `fee`. Elle recalcule NATIVEMENT toutes
// les valeurs (ownerTag, inCm, nf, racine, outCm) — de sorte que l'énoncé public
// renvoyé soit, par construction, exactement celui que la trace satisfait — puis
// remplit les colonnes-témoins (états d'entrée libres, siblings/bits, registres,
// wTag/wRho/wVal) ligne par ligne.
//
// Renvoie la trace et l'énoncé public reconstruit (MerkleRoot, Nf, OutCm, Fee).
//
// Panique (erreur d'appelant) si un bit de direction n'est pas binaire : un
// témoin non binaire n'a pas de sens (et serait de toute façon rejeté par la
// contrainte de binarité du circuit).
func buildSpendTrace(w SpendWitness, fee Felt) (trace [][]Felt, public SpendPublic) {
	for i := 0; i < spendDepth; i++ {
		if !(w.Path.Bits[i].IsZero() || w.Path.Bits[i].Equal(One())) {
			panic("stark: buildSpendTrace: bit de direction non binaire")
		}
	}

	trace = make([][]Felt, spSteps)

	// --- Valeurs natives (en clair), exactement les fonctions de poseidon_spend.go
	//     et merkle_poseidon.go. La trace les reproduit colonne par colonne. ---
	ownerTag := SpendOwnerTag(w.Nk)                                // bloc 0
	inCm := SpendCommit(w.InValue, ownerTag, w.InRho)              // bloc 1
	nf := SpendNullifier(w.Nk, inCm)                               // bloc 2
	merkleRoot := spChainRoot(inCm, w.Path)                        // blocs 3..3+d-1
	outCm := SpendCommit(w.OutValue, w.OutOwnerTag, w.OutRho)      // bloc 3+d

	// --- États d'entrée natifs de chaque bloc (12 Felt), pour remplir l'état. ---
	// Bloc 0 : pack(nk).
	inOwner := spOwnerTagState(w.Nk)
	// Bloc 1 : commit(inValue, ownerTag, inRho).
	inInCm := spCommitState(w.InValue, ownerTag, w.InRho)
	// Bloc 2 : Hash2(nk, inCm) -> état d'éponge Hash2.
	inNf := spHash2State(w.Nk, inCm)
	// Blocs 3..3+d-1 : ré-assemblage Merkle, child initial = inCm.
	var memIn [spendDepth][pfStateCols]Felt
	child := inCm
	for lvl := 0; lvl < spendDepth; lvl++ {
		memIn[lvl] = spReasmState(child, w.Path.Siblings[lvl], w.Path.Bits[lvl])
		child = spChainStep(child, w.Path.Siblings[lvl], w.Path.Bits[lvl])
	}
	// Bloc 3+d : commit(outValue, outOwnerTag, outRho).
	inOutCm := spCommitState(w.OutValue, w.OutOwnerTag, w.OutRho)

	// --- Remplissage des 8 blocs (état + colonnes de structure). ---
	spFillBlock(trace, spBlkOwner, inOwner, fee)
	spFillBlock(trace, spBlkInCm, inInCm, fee)
	spFillBlock(trace, spBlkNf, inNf, fee)
	for lvl := 0; lvl < spendDepth; lvl++ {
		spFillBlock(trace, spBlkMem0+lvl, memIn[lvl], fee)
	}
	spFillBlock(trace, spBlkOutCm, inOutCm, fee)

	// --- Cohérence : l'état d'entrée d'un bloc (ligne 0) DOIT coïncider avec l'état
	//     de service (ligne 31) du bloc précédent, car la transition 31 -> 0 est une
	//     identité (idfac=1 sur ces lignes). spFillBlock a posé la ligne 31 = sortie
	//     du bloc précédent ; on ÉCRASE donc l'état des lignes de service (31) par
	//     l'état d'entrée du bloc suivant, pour satisfaire l'identité 31 -> 0. ---
	spOverwriteServiceState(trace, spBlkOwner, inInCm)               // 0 -> 1
	spOverwriteServiceState(trace, spBlkInCm, inNf)                  // 1 -> 2
	spOverwriteServiceState(trace, spBlkNf, memIn[0])                // 2 -> 3 (L0)
	for lvl := 0; lvl < spendDepth-1; lvl++ {
		spOverwriteServiceState(trace, spBlkMem0+lvl, memIn[lvl+1])  // Li -> Li+1
	}
	spOverwriteServiceState(trace, spBlkMemL, inOutCm)               // dernier mem -> outCm
	// Bloc outCm : pas de bloc suivant ; sa ligne 31 reste la sortie (identité OK).

	// --- Colonnes-témoins de glue (siblings/bits, wTag/wRho/wVal). ---
	// Glue du bloc ownerTag (mCommitS, ligne de sortie du bloc ownerTag) : forme la
	// note d'ENTRÉE inCm. La branche commitS prend tag = état courant (= ownerTag),
	// rho = wRho = inRho, value = wVal = inValue. C'est AUSSI la ligne de chargement
	// de regVal (regVal := wVal = inValue) — d'où la nécessité de poser wVal ici.
	rowOwnerGlue := spOutputRowOf(spBlkOwner)
	spSetWNote(trace[rowOwnerGlue], w.InRho, w.InValue)

	// Glue du bloc memL (mCommitW, ligne de sortie du dernier niveau d'appartenance) :
	// forme la note de SORTIE. tag = wTag témoin = outOwnerTag, rho = outRho,
	// value = outValue.
	rowOutCmGlue := spOutputRowOf(spBlkMemL)
	spSetWTag(trace[rowOutCmGlue], w.OutOwnerTag)
	spSetWNote(trace[rowOutCmGlue], w.OutRho, w.OutValue)

	// Siblings + bits des lignes de ré-assemblage Merkle :
	//   - glue du bloc nf (mReasmReg) : entrée du niveau 0 (child = regCm = inCm).
	//   - glue des blocs memL0..memL(d-2) (mReasm) : entrée du niveau lvl+1.
	rowNfGlue := spOutputRowOf(spBlkNf)
	spSetSibBit(trace[rowNfGlue], w.Path.Siblings[0], w.Path.Bits[0])
	for lvl := 0; lvl < spendDepth-1; lvl++ {
		row := spOutputRowOf(spBlkMem0 + lvl)
		spSetSibBit(trace[row], w.Path.Siblings[lvl+1], w.Path.Bits[lvl+1])
	}

	// --- Registres portés constants (regNk, regCm, regVal). ---
	spFillRegisters(trace, w, inCm)

	public = SpendPublic{
		MerkleRoot: merkleRoot,
		Nf:         nf,
		OutCm:      outCm,
		Fee:        fee,
	}
	return trace, public
}

// spOverwriteServiceState écrase l'état (12 cellules) de la ligne de SERVICE
// (indice 31) du bloc `b` par `nextIn`. C'est l'état d'entrée du bloc suivant : la
// transition 31 -> 0 étant l'identité (idfac=1), les deux lignes DOIVENT porter le
// même état. La ligne 0 du bloc suivant porte déjà `nextIn` (posée par spFillBlock
// avec l'état d'entrée correct), donc l'égalité est satisfaite.
func spOverwriteServiceState(trace [][]Felt, b int, nextIn [pfStateCols]Felt) {
	row := b*spBlock + (spBlock - 1) // ligne 31 du bloc
	for k := 0; k < pfStateCols; k++ {
		trace[row][spStateOff+k] = nextIn[k]
	}
}

// spSetWNote pose les colonnes-témoins de note (wRho, wVal) sur une ligne de glue
// commitment. wTag est posé séparément (spSetWTag) car commitS n'en a pas besoin
// (tag = état courant) tandis que commitW le lit.
func spSetWNote(line []Felt, rho [poseidonDigestLen]Felt, value Felt) {
	for k := 0; k < poseidonDigestLen; k++ {
		line[spWRhoOff+k] = rho[k]
	}
	line[spWValCol] = value
}

// spSetWTag pose le témoin de tag (wTag) sur une ligne de glue commitW (note de
// sortie : tag = outOwnerTag).
func spSetWTag(line []Felt, tag [poseidonDigestLen]Felt) {
	for k := 0; k < poseidonDigestLen; k++ {
		line[spWTagOff+k] = tag[k]
	}
}

// spSetSibBit pose le sibling et le bit de direction sur une ligne de ré-assemblage
// Merkle (glue mReasm / mReasmReg).
func spSetSibBit(line []Felt, sib [poseidonDigestLen]Felt, bit Felt) {
	for k := 0; k < poseidonDigestLen; k++ {
		line[spSibOff+k] = sib[k]
	}
	line[spBitCol] = bit
}

// spFillRegisters remplit les trois registres portés constants sur toute la trace,
// en cohérence EXACTE avec les fenêtres load/hold de spendRowStructure :
//
//   - regNk : doit valoir nk de la ligne 0 jusqu'AVANT la ligne nf-glue. La
//     contrainte load à la ligne 0 lie regNk = cur.s[0..3] = nk ; les contraintes
//     hold propagent la valeur. On pose donc regNk = nk sur toutes les lignes du
//     domaine de hold (+ la ligne nf-glue où il est relu par mHash2).
//   - regCm : doit valoir inCm de la ligne de sortie du bloc inCm jusqu'à la ligne
//     nf-glue (relu par mReasmReg). On pose regCm = inCm sur ce domaine.
//   - regVal : doit valoir inValue de la ligne inCm-glue jusqu'à la ligne outCm-glue
//     (où la conservation le compare à outValue + fee). On pose regVal = inValue.
//
// Hors de ces domaines, les registres restent à zéro (non contraints). Poser une
// valeur cohérente AVEC les load/hold garantit une trace satisfaisante.
func spFillRegisters(trace [][]Felt, w SpendWitness, inCm [poseidonDigestLen]Felt) {
	nfGlue := spOutputRowOf(spBlkNf)
	loadCmRow := spOutputRowOf(spBlkInCm)
	loadValRow := spOutputRowOf(spBlkOwner)
	outCmGlue := spOutputRowOf(spBlkMemL)

	for row := 0; row < spSteps; row++ {
		// regNk : posé de la ligne 0 jusqu'à la ligne nf-glue INCLUSE (relecture).
		if row <= nfGlue {
			for k := 0; k < poseidonDigestLen; k++ {
				trace[row][spRegNkOff+k] = w.Nk[k]
			}
		}
		// regCm : posé de la ligne de chargement (sortie inCm) jusqu'à nf-glue INCLUSE.
		if row >= loadCmRow && row <= nfGlue {
			for k := 0; k < poseidonDigestLen; k++ {
				trace[row][spRegCmOff+k] = inCm[k]
			}
		}
		// regVal : posé de la ligne inCm-glue jusqu'à la ligne outCm-glue INCLUSE.
		if row >= loadValRow && row <= outCmGlue {
			trace[row][spRegValCol] = w.InValue
		}
	}
}

// ---------------------------------------------------------------------------
// Répliques natives des états d'éponge utilisés par les glues (cohérence trace
// <-> AIR). Ces fonctions produisent EXACTEMENT les états dont EvalTransition
// calcule les branches hash2/reasm/reasmReg.
// ---------------------------------------------------------------------------

// spHash2State construit l'état d'entrée (12 Felt) de la compression Hash2 :
// [ left(4) | right(4) | domainSep | 8 | 0 | 0 ]. Réplique de l'ensemencement
// Hash2 de poseidon.go (et de la branche hash2 de EvalTransition).
func spHash2State(left, right [poseidonDigestLen]Felt) [pfStateCols]Felt {
	var st [pfStateCols]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		st[k] = left[k]
		st[poseidonDigestLen+k] = right[k]
	}
	st[poseidonRate] = FromUint64(poseidonDomainSep)
	st[poseidonRate+1] = FromUint64(uint64(poseidonRate))
	return st
}

// spReasmState construit l'état d'entrée Hash2 (12 Felt) d'un niveau d'appartenance
// à partir du digest courant `child`, du `sibling` et du `bit` :
//
//	bit==0 : left=child, right=sibling   (Hash2(child, sibling))
//	bit==1 : left=sibling, right=child   (Hash2(sibling, child))
//
// Réplique EXACTE des branches reasm/reasmReg de EvalTransition (poseidon_spend_air.go).
func spReasmState(child, sibling [poseidonDigestLen]Felt, bit Felt) [pfStateCols]Felt {
	var left, right [poseidonDigestLen]Felt
	if bit.IsZero() {
		left, right = child, sibling
	} else {
		left, right = sibling, child
	}
	return spHash2State(left, right)
}

// spChainStep applique UN niveau de la chaîne d'appartenance (compression Hash2
// orientée par le bit) : c'est la fonction de compression native du niveau.
//
//	bit==0 : Hash2(child, sibling) ; bit==1 : Hash2(sibling, child).
func spChainStep(child, sibling [poseidonDigestLen]Felt, bit Felt) [poseidonDigestLen]Felt {
	if bit.IsZero() {
		return Hash2(child, sibling)
	}
	return Hash2(sibling, child)
}

// spChainRoot recalcule la racine NATIVE atteinte par la chaîne d'appartenance de
// profondeur spendDepth partant de la feuille `leaf` (= inCm) le long de `path`.
// C'est la version « en clair » de la chaîne prouvée par les blocs d'appartenance ;
// elle DOIT coïncider avec PoseidonVerifyPath/racine de merkle_poseidon.go pour le
// même (leaf, path) (testé).
func spChainRoot(leaf [poseidonDigestLen]Felt, path SpendPath) [poseidonDigestLen]Felt {
	cur := leaf
	for lvl := 0; lvl < spendDepth; lvl++ {
		cur = spChainStep(cur, path.Siblings[lvl], path.Bits[lvl])
	}
	return cur
}

// ---------------------------------------------------------------------------
// Valeurs publiques absorbées dans le transcript
// ---------------------------------------------------------------------------

// spendPublicInputs assemble le vecteur de valeurs publiques absorbées par
// ProveAIR/VerifyAIR : MerkleRoot (4) | Nf (4) | OutCm (4) | Fee (1). L'ordre est
// FIGÉ et partagé prouveur/vérifieur. Le témoin n'y figure pas (zero-knowledge au
// sens « non publié »). Les colonnes de structure (modes/registres/fee par ligne)
// sont déjà absorbées via les contraintes de bord.
func spendPublicInputs(public SpendPublic) []Felt {
	out := make([]Felt, 0, 3*poseidonDigestLen+1)
	out = append(out, public.MerkleRoot[:]...)
	out = append(out, public.Nf[:]...)
	out = append(out, public.OutCm[:]...)
	out = append(out, public.Fee)
	return out
}

// ---------------------------------------------------------------------------
// Prouveur / Vérifieur de haut niveau
// ---------------------------------------------------------------------------

// ProveSpend construit une preuve STARK de dépense blindée pour le témoin `w` et
// les frais `fee`. Elle atteste les 5 contraintes liées (voir bandeau de
// poseidon_spend.go) SANS révéler le témoin :
//
//	(1)   inCm     = Commit(InValue, PoseidonHash(Nk), InRho)
//	(1bis) ownerTag = PoseidonHash(Nk)         (le propriétaire est le détenteur de Nk)
//	(2)   inCm ∈ arbre Merkle de racine MerkleRoot
//	(3)   Nf       = Hash2(Nk, inCm)
//	(4)   OutCm    = Commit(OutValue, OutOwnerTag, OutRho)
//	(5)   InValue  = OutValue + Fee
//
// Renvoie (public, proof). `public` (MerkleRoot, Nf, OutCm, Fee) est reconstruit
// par la trace, donc garanti cohérent avec ce qu'elle satisfait. Le témoin ne
// figure dans AUCUNE valeur publique de la preuve.
//
// Panique (erreur d'appelant) si un bit de direction n'est pas binaire.
func ProveSpend(w SpendWitness, fee Felt) (SpendPublic, AirProof) {
	trace, public := buildSpendTrace(w, fee)
	air := spendAIR{
		merkleRoot: public.MerkleRoot,
		nf:         public.Nf,
		outCm:      public.OutCm,
		fee:        public.Fee,
	}
	inputs := spendPublicInputs(public)
	proof := ProveAIR(air, trace, inputs...)
	return public, proof
}

// VerifySpend vérifie une preuve produite par ProveSpend pour l'énoncé public
// `public`. Renvoie true ssi la preuve atteste l'existence d'un témoin satisfaisant
// les 5 contraintes liées pour CET énoncé (MerkleRoot, Nf, OutCm, Fee).
//
// `public` est l'énoncé ; toute incohérence (mauvaise racine/nf/outCm/fee) viole un
// bord et/ou fait diverger le transcript => rejet. Ne panique JAMAIS sur preuve
// falsifiée : rejet propre (false).
func VerifySpend(public SpendPublic, proof AirProof) bool {
	air := spendAIR{
		merkleRoot: public.MerkleRoot,
		nf:         public.Nf,
		outCm:      public.OutCm,
		fee:        public.Fee,
	}
	inputs := spendPublicInputs(public)
	return VerifyAIR(air, proof, inputs...)
}
