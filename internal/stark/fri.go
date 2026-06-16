// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// FRI (Fast Reed-Solomon Interactive Oracle Proof of Proximity) — le cœur du
// STARK. FRI prouve, sans la révéler entièrement, qu'une fonction engagée sur
// un domaine d'évaluation est PROCHE (en distance de Hamming relative) d'un
// polynôme de degré borné. C'est la brique de « test de bas degré » sur
// laquelle repose la soundness d'un STARK.
//
// Idée géométrique du pliage (folding) :
//
//	Le domaine D est le sous-groupe multiplicatif d'ordre N = 2^k engendré par
//	une racine N-ième de l'unité ω : D = {ω^0, ω^1, ..., ω^(N-1)}. Ce groupe a
//	la propriété que x et -x = ω^(N/2)·x y cohabitent : l'élément d'indice i a
//	pour « jumeau » l'élément d'indice i + N/2 (car ω^(N/2) = -1).
//
//	Tout polynôme f se décompose en parties paire et impaire :
//	    f(x) = f_pair(x²) + x·f_impair(x²)
//	d'où, pour un point x du domaine et son jumeau -x :
//	    f_pair(x²)   = (f(x) + f(-x)) / 2
//	    f_impair(x²) = (f(x) - f(-x)) / (2x)
//	Le polynôme plié sous le défi β est
//	    f'(y) = f_pair(y) + β·f_impair(y),
//	de degré moitié, évalué sur le domaine D² = {(ω²)^0, ...} d'ordre N/2.
//	La valeur f'(x²) à l'indice i de D² se calcule donc à partir des valeurs
//	f(x), f(-x) aux indices i et i+N/2 de D. C'est la relation de cohérence que
//	le vérifieur recontrôle à chaque couche, aux positions interrogées.
//
// Phase d'engagement (prouveur) : on engage f (Merkle), tire β via le
// transcript, plie, engage la couche suivante, recommence jusqu'à une couche
// finale de taille <= un seuil ; on absorbe alors les coefficients de cette
// couche finale en clair.
//
// Phase de requêtes : le transcript tire des positions ; pour chacune, le
// prouveur ouvre, à chaque couche, la valeur au point ET la valeur au jumeau,
// avec leurs chemins de Merkle. Le vérifieur revérifie chaque ouverture,
// recalcule le pliage et le compare à la valeur ouverte dans la couche
// suivante, puis vérifie que la couche finale est bien de bas degré.
//
// DÉTERMINISME ABSOLU : aucun time / math/rand. Tout l'aléa provient du
// transcript Fiat-Shamir (SHAKE256). Prouveur et vérifieur sont reproductibles
// bit-à-bit.
package stark

// friDomain est l'étiquette de domaine du transcript FRI : sépare ce protocole
// de tout autre usage du transcript dans le STARK complet.
const friDomain = "stark/fri/v1"

// Conception du critère de bas degré (POINT CRUCIAL de soundness)
// ----------------------------------------------------------------
// Le domaine initial a une taille N = Blowup·(d+1) où d est la borne de degré
// que l'on prouve. À CHAQUE pliage, le domaine ET la borne de degré sont
// divisés par deux. On plie jusqu'à ce que le domaine atteigne la taille
// Blowup : à cet instant la borne de degré vaut N/2^k / ... = 1, autrement dit
// la couche finale DOIT être un polynôme CONSTANT.
//
// C'est là que réside la soundness : la couche finale est un vecteur de Blowup
// évaluations sur un domaine de taille Blowup. Si la fonction de départ était
// réellement de bas degré, ces Blowup évaluations sont toutes ÉGALES (constante)
// — il y a redondance Reed-Solomon. Si la fonction était aléatoire (loin de tout
// bas degré), la couche finale n'est PAS constante avec probabilité écrasante,
// et le vérifieur la rejette. Plier jusqu'à une couche de taille 1 rendrait au
// contraire le test VIDE (une seule valeur est trivialement « de degré 0 ») —
// c'est l'erreur classique à éviter.
//
// On exige donc Blowup >= 2 (déjà imposé) pour que la couche finale porte au
// moins deux évaluations redondantes : un vrai contrôle de degré.

// FriParams regroupe les paramètres publics de l'instance FRI. Ils font partie
// de l'énoncé : prouveur et vérifieur DOIVENT s'accorder dessus (et ils sont
// absorbés dans le transcript pour lier la preuve à ses paramètres).
type FriParams struct {
	// Blowup est le facteur d'expansion Reed-Solomon : taille du domaine
	// divisée par (degré+1). Plus il est grand, meilleure est la soundness par
	// requête. Puissance de 2 >= 2 (ex. 8).
	Blowup int
	// NumQueries est le nombre de positions interrogées en phase de requêtes.
	// La soundness décroît exponentiellement avec ce nombre (ex. 32).
	NumQueries int
}

// QueryStep est l'ouverture, à une couche donnée et pour une requête donnée, du
// point interrogé et de son jumeau (-x), chacun avec son chemin de Merkle.
//
// Convention d'indices : à la couche c de taille Nc, la requête porte sur un
// indice pos dans [0, Nc/2). Value est l'évaluation à l'indice pos ; Sibling est
// l'évaluation à l'indice pos + Nc/2 (le jumeau -x). Path/SiblingPath sont leurs
// chemins de Merkle respectifs dans l'arbre de la couche c.
type QueryStep struct {
	Value       Felt       // f_c(x)   à l'indice pos
	Sibling     Felt       // f_c(-x)  à l'indice pos + Nc/2
	Path        [][32]byte // chemin de Merkle de Value
	SiblingPath [][32]byte // chemin de Merkle de Sibling
}

// FriProof est la preuve FRI sérialisable (en mémoire ; la sérialisation octets
// éventuelle se fera à l'étage supérieur). Elle contient les racines de Merkle
// de chaque couche pliée, les coefficients de la couche finale en clair, et —
// pour chaque position interrogée — la suite d'ouvertures couche par couche.
type FriProof struct {
	// LogDomain est log2(N) de la taille du domaine d'évaluation initial.
	LogDomain uint32
	// LayerRoots contient une racine de Merkle par couche pliée (de la couche 0
	// jusqu'à l'avant-dernière incluse ; la couche finale n'est pas engagée par
	// Merkle mais envoyée en clair).
	LayerRoots [][32]byte
	// FinalCoeffs sont les coefficients (ordre croissant) du polynôme de la
	// couche finale, reconstruits par interpolation des Blowup évaluations
	// finales et envoyés en clair. Pour une fonction réellement de bas degré, ce
	// polynôme est CONSTANT : seul FinalCoeffs[0] est non nul. Le vérifieur exige
	// que tous les coefficients de degré >= 1 soient nuls (critère de bas degré
	// terminal). On transmet le vecteur complet (longueur Blowup) pour que le
	// vérifieur recontrôle chaque évaluation finale, sans lui faire confiance.
	FinalCoeffs []Felt
	// Queries[q] est la suite d'ouvertures (une par couche pliée) pour la q-ème
	// position interrogée. Queries[q][c] concerne la couche c.
	Queries [][]QueryStep
}

// friLayer est l'état interne d'une couche côté prouveur : ses évaluations sur
// le domaine courant et l'arbre de Merkle qui les engage.
type friLayer struct {
	evals []Felt
	tree  *MerkleTree
}

// leafOf empaquette une évaluation (un Felt) en une feuille Merkle d'un seul
// élément. On garde une fonction dédiée pour que prouveur et vérifieur
// fabriquent EXACTEMENT la même feuille (déterminisme du hachage).
func leafOf(v Felt) []Felt {
	return []Felt{v}
}

// commitEvals engage un vecteur d'évaluations comme feuilles Merkle (une feuille
// par évaluation).
func commitEvals(evals []Felt) ([32]byte, *MerkleTree) {
	leaves := make([][]Felt, len(evals))
	for i, v := range evals {
		leaves[i] = leafOf(v)
	}
	return Commit(leaves)
}

// foldEvals plie un vecteur d'évaluations de taille N (puissance de 2 paire) en
// un vecteur de taille N/2, sous le défi beta.
//
// Pour l'indice i de la couche suivante (point y = (ω²)^i = ω^(2i) = x²) :
//
//	x        = ω^i              (point de la couche courante, indice i)
//	-x       = ω^(i+N/2)        (jumeau, indice i+N/2)
//	f_pair   = (f(x)+f(-x))/2
//	f_impair = (f(x)-f(-x))/(2x)
//	f'(y)    = f_pair + beta·f_impair
//
// On précalcule les puissances de ω par multiplication itérative (déterministe).
func foldEvals(evals []Felt, beta Felt, omega Felt) []Felt {
	n := len(evals)
	half := n / 2
	out := make([]Felt, half)

	two := FromUint64(2)
	twoInv := two.Inv()

	// xi = ω^i, démarré à ω^0 = 1 et multiplié par ω à chaque pas.
	xi := One()
	for i := 0; i < half; i++ {
		fx := evals[i]       // f(x),  x = ω^i
		fmx := evals[i+half] // f(-x), -x = ω^(i+N/2)
		// f_pair = (f(x)+f(-x))/2
		fPair := fx.Add(fmx).Mul(twoInv)
		// f_impair = (f(x)-f(-x)) / (2x) = (f(x)-f(-x)) * (2x)^{-1}
		twoX := two.Mul(xi)
		fImpair := fx.Sub(fmx).Mul(twoX.Inv())
		// f'(y) = f_pair + beta·f_impair
		out[i] = fPair.Add(beta.Mul(fImpair))
		xi = xi.Mul(omega)
	}
	return out
}

// Prove construit une preuve FRI de proximité au bas degré pour la fonction
// `evals` évaluée sur le domaine {ω^0, ..., ω^(N-1)} d'ordre N = len(evals).
//
// Contrats :
//   - len(evals) DOIT être une puissance de 2 et STRICTEMENT supérieure à
//     params.Blowup (au moins un pliage avant d'atteindre le domaine final de
//     taille Blowup). Panique sinon.
//   - params.Blowup DOIT être une puissance de 2 >= 2, params.NumQueries >= 1.
//     Panique sinon.
//
// Note : Prove engage exactement la fonction fournie. Si cette fonction n'est
// PAS proche d'un polynôme de bas degré, la couche finale ne sera pas constante
// et/ou les contrôles de cohérence échoueront à la vérification — c'est le rôle
// des tests de soundness négatifs.
func Prove(evals []Felt, params FriParams) FriProof {
	n := len(evals)
	if !isPow2(params.Blowup) || params.Blowup < 2 {
		panic("stark: Prove: Blowup doit être une puissance de 2 >= 2")
	}
	if !isPow2(n) || n <= params.Blowup {
		panic("stark: Prove: len(evals) doit être une puissance de 2 > Blowup")
	}
	if params.NumQueries < 1 {
		panic("stark: Prove: NumQueries doit être >= 1")
	}

	logDomain := log2(n)

	tr := NewTranscript(friDomain)
	absorbParams(tr, params, logDomain)

	// --- Phase d'engagement : pliages successifs ---
	var layers []friLayer
	var layerRoots [][32]byte

	// Copie défensive : on ne mute pas le slice de l'appelant.
	cur := clonePoly(evals)
	omega := RootOfUnity(log2(len(cur))) // racine d'ordre = taille courante

	// On plie jusqu'à atteindre le domaine final de taille Blowup. À cet instant
	// la borne de degré est tombée à 1 : la couche finale doit être constante.
	for len(cur) > params.Blowup {
		root, tree := commitEvals(cur)
		tr.Absorb("fri/layer-root", root[:])
		layers = append(layers, friLayer{evals: cur, tree: tree})
		layerRoots = append(layerRoots, root)

		beta := tr.Challenge("fri/fold")
		cur = foldEvals(cur, beta, omega)
		// La racine du domaine suivant est le carré de l'actuelle (D -> D²).
		omega = omega.Mul(omega)
	}

	// --- Couche finale : envoyée en clair (coefficients) ---
	// `cur` est la table des Blowup évaluations finales sur le domaine d'ordre
	// Blowup ; on l'interpole pour transmettre les coefficients. Pour un mot de
	// code Reed-Solomon honnête, seul le coefficient constant est non nul.
	finalCoeffs := Interpolate(cur)
	absorbFinal(tr, finalCoeffs)

	// Par construction (boucle ci-dessus), il y a au moins une couche pliée
	// puisque n > Blowup.
	firstHalf := len(layers[0].evals) / 2
	positions := tr.ChallengeIndices("fri/query", params.NumQueries, firstHalf)

	queries := make([][]QueryStep, len(positions))
	for q, pos0 := range positions {
		steps := make([]QueryStep, len(layers))
		pos := pos0
		for c := range layers {
			half := len(layers[c].evals) / 2
			// Réduction de la position au domaine de la couche c. À chaque
			// pliage, l'indice est divisé par 2 (le domaine rétrécit de moitié)
			// et ramené modulo half pour rester dans [0, half).
			p := pos % half
			steps[c] = QueryStep{
				Value:       layers[c].evals[p],
				Sibling:     layers[c].evals[p+half],
				Path:        Open(layers[c].tree, p),
				SiblingPath: Open(layers[c].tree, p+half),
			}
			pos = p // la position de la couche suivante est p (dans [0, half))
		}
		queries[q] = steps
	}

	return FriProof{
		LogDomain:   logDomain,
		LayerRoots:  layerRoots,
		FinalCoeffs: finalCoeffs,
		Queries:     queries,
	}
}

// Verify rejoue le transcript et vérifie la preuve FRI. Renvoie true ssi :
//   - les paramètres et la taille de domaine concordent ;
//   - chaque ouverture (valeur + jumeau) est authentifiée par le bon arbre de
//     Merkle de couche ;
//   - la relation de pliage est respectée à chaque couche, aux positions
//     interrogées (la valeur pliée doit coïncider avec la valeur ouverte dans
//     la couche suivante, ou avec l'évaluation de la couche finale) ;
//   - la couche finale est CONSTANTE : son polynôme interpolé n'a qu'un terme
//     constant (tous les coefficients de degré >= 1 sont nuls). C'est le critère
//     de bas degré terminal, et la garantie centrale de soundness.
//
// Toute incohérence => false. Aucune panique sur preuve falsifiée : on rejette
// proprement (robustesse face à un prouveur malveillant). Verify N'utilise PAS
// les évaluations originales : seul l'engagement Merkle (racines), les
// coefficients finaux et les ouvertures sont consultés.
func Verify(proof FriProof, params FriParams) bool {
	// --- Contrôles structurels de base (preuve bien formée) ---
	if !isPow2(params.Blowup) || params.Blowup < 2 {
		return false
	}
	if params.NumQueries < 1 {
		return false
	}
	if proof.LogDomain > TwoAdicity() {
		return false
	}
	n := 1 << proof.LogDomain
	// Le domaine initial doit être > Blowup (au moins une couche pliée).
	if n <= params.Blowup {
		return false
	}

	// La couche finale couvre exactement le domaine de taille Blowup : on attend
	// donc Blowup coefficients. CRITÈRE DE BAS DEGRÉ : le polynôme final doit être
	// CONSTANT, donc tous les coefficients de degré >= 1 sont nuls. Une fonction
	// aléatoire produit ici un polynôme de degré plein => rejet.
	if len(proof.FinalCoeffs) != params.Blowup {
		return false
	}
	for i := 1; i < len(proof.FinalCoeffs); i++ {
		if !proof.FinalCoeffs[i].IsZero() {
			return false
		}
	}

	// Nombre de couches pliées attendu : on plie de N jusqu'à Blowup.
	expectedLayers := 0
	for sz := n; sz > params.Blowup; sz /= 2 {
		expectedLayers++
	}
	if len(proof.LayerRoots) != expectedLayers {
		return false
	}
	// Par construction n > Blowup => au moins une couche.
	if expectedLayers == 0 {
		return false
	}

	// --- Rejoue du transcript : mêmes absorptions, mêmes défis ---
	tr := NewTranscript(friDomain)
	absorbParams(tr, params, proof.LogDomain)

	betas := make([]Felt, expectedLayers)
	for c := 0; c < expectedLayers; c++ {
		tr.Absorb("fri/layer-root", proof.LayerRoots[c][:])
		betas[c] = tr.Challenge("fri/fold")
	}
	absorbFinal(tr, proof.FinalCoeffs)

	if len(proof.Queries) != params.NumQueries {
		return false
	}

	firstHalf := n / 2
	positions := tr.ChallengeIndices("fri/query", params.NumQueries, firstHalf)

	// Racine d'ordre du domaine de la première couche, pour reconstituer le
	// point x = ω^pos à chaque couche.
	omega0 := RootOfUnity(proof.LogDomain)

	// --- Vérification couche par couche pour chaque requête ---
	for q, pos0 := range positions {
		steps := proof.Queries[q]
		if len(steps) != expectedLayers {
			return false
		}

		pos := pos0
		omega := omega0
		layerSize := n

		for c := 0; c < expectedLayers; c++ {
			half := layerSize / 2
			p := pos % half
			step := steps[c]

			// 1) Authenticité des deux ouvertures contre la racine de la couche.
			if !VerifyPath(proof.LayerRoots[c], p, leafOf(step.Value), step.Path) {
				return false
			}
			if !VerifyPath(proof.LayerRoots[c], p+half, leafOf(step.Sibling), step.SiblingPath) {
				return false
			}

			// 2) Recalcul du pliage à la position p.
			//    x = ω^p ; f'(x²) = f_pair + beta·f_impair.
			x := omega.Exp(uint64(p))
			folded := foldOne(step.Value, step.Sibling, betas[c], x)

			// 3) Cohérence avec la couche suivante.
			if c+1 < expectedLayers {
				// La valeur pliée doit être l'évaluation de la couche suivante à
				// l'indice (p mod half'), que le prouveur a ouverte comme Value
				// ou Sibling selon la parité de la position. On la recontrôle
				// directement contre l'ouverture de la couche c+1.
				nextHalf := half / 2
				nextStep := steps[c+1]
				// L'indice plié dans la couche c+1 est p (car p < half =
				// taille de la couche c+1). Selon que p < nextHalf (le point est
				// la « moitié gauche », donc ouvert comme Value) ou p >= nextHalf
				// (moitié droite, ouvert comme Sibling), la valeur attendue est
				// Value ou Sibling de l'étape suivante.
				var expected Felt
				if p < nextHalf {
					expected = nextStep.Value
				} else {
					expected = nextStep.Sibling
				}
				if !folded.Equal(expected) {
					return false
				}
			} else {
				// Dernière couche pliée : la valeur pliée doit coïncider avec
				// l'évaluation du polynôme final au point y = x² = ω^(2p) du
				// domaine final.
				finalDomainSize := half
				yIndex := p % finalDomainSize
				// ω_final = ω² (le domaine a été carré une fois de plus).
				omegaFinal := omega.Mul(omega)
				y := omegaFinal.Exp(uint64(yIndex))
				if !folded.Equal(evalNaïfPoly(proof.FinalCoeffs, y)) {
					return false
				}
			}

			// Préparation de la couche suivante.
			pos = p
			omega = omega.Mul(omega)
			layerSize = half
		}
	}

	return true
}

// foldOne calcule la valeur pliée à un point, à partir de f(x), f(-x), du défi
// beta et du point x. C'est la version « un point » de foldEvals, partagée par
// le prouveur (implicitement, via foldEvals) et le vérifieur.
//
//	f_pair   = (f(x)+f(-x))/2
//	f_impair = (f(x)-f(-x))/(2x)
//	résultat = f_pair + beta·f_impair
func foldOne(fx, fmx, beta, x Felt) Felt {
	two := FromUint64(2)
	twoInv := two.Inv()
	fPair := fx.Add(fmx).Mul(twoInv)
	twoX := two.Mul(x)
	fImpair := fx.Sub(fmx).Mul(twoX.Inv())
	return fPair.Add(beta.Mul(fImpair))
}

// evalNaïfPoly évalue un polynôme (coefficients en ordre croissant) en x par
// schéma de Horner. Utilisé pour contrôler la couche finale de bas degré sans
// dépendre de la NTT (le vérifieur n'a que quelques coefficients).
func evalNaïfPoly(coeffs []Felt, x Felt) Felt {
	acc := Zero()
	for i := len(coeffs) - 1; i >= 0; i-- {
		acc = acc.Mul(x).Add(coeffs[i])
	}
	return acc
}

// absorbParams absorbe les paramètres publics et la taille de domaine dans le
// transcript : lie la preuve à son énoncé (un changement de paramètre change
// tous les défis, donc invalide la preuve).
func absorbParams(tr *Transcript, params FriParams, logDomain uint32) {
	tr.AbsorbFelt("fri/blowup", FromUint64(uint64(params.Blowup)))
	tr.AbsorbFelt("fri/num-queries", FromUint64(uint64(params.NumQueries)))
	tr.AbsorbFelt("fri/log-domain", FromUint64(uint64(logDomain)))
}

// absorbFinal absorbe les coefficients de la couche finale dans le transcript
// (ils sont publics et lient la preuve à sa terminaison de bas degré).
func absorbFinal(tr *Transcript, coeffs []Felt) {
	tr.AbsorbFelt("fri/final-len", FromUint64(uint64(len(coeffs))))
	for _, c := range coeffs {
		tr.AbsorbFelt("fri/final-coeff", c)
	}
}

// clonePoly duplique un slice de Felt (copie défensive).
func clonePoly(a []Felt) []Felt {
	c := make([]Felt, len(a))
	copy(c, a)
	return c
}
