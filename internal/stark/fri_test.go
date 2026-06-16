// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Tests du protocole FRI : un polynôme de BAS DEGRÉ passe le test de proximité,
// tandis qu'une fonction ALÉATOIRE (loin de tout bas degré) échoue. Tests de
// SOUNDNESS négatifs : toute falsification (racine de couche, valeur ouverte,
// jumeau, chemin de Merkle, coefficients finaux, position, paramètres) DOIT
// être rejetée. Déterminisme bit-à-bit vérifié. PRNG à graine fixe : aucun
// hasard non reproductible.
package stark

import "testing"

// lowDegreeEvals fabrique les évaluations, sur le domaine {ω^0..ω^(N-1)}, d'un
// polynôme aléatoire de degré < deg+1, avec N = blowup*(deg+1) (étendu à la
// prochaine puissance de 2). C'est exactement le mot de code Reed-Solomon que
// FRI doit accepter.
func lowDegreeEvals(rng *prng, deg, blowup int) ([]Felt, int) {
	coeffs := make([]Felt, deg+1)
	for i := range coeffs {
		coeffs[i] = rng.felt()
	}
	n := nextPow2((deg + 1) * blowup)
	buf := make([]Felt, n)
	copy(buf, coeffs)
	NTT(buf) // évaluations sur le domaine d'ordre n
	return buf, n
}

// defaultParams renvoie des paramètres FRI raisonnables pour les tests.
func defaultParams() FriParams {
	return FriParams{Blowup: 8, NumQueries: 32}
}

// TestFriBasDegréPasse : un mot de code Reed-Solomon (polynôme de bas degré)
// doit être accepté pour diverses tailles de degré.
func TestFriBasDegréPasse(t *testing.T) {
	// deg >= 1 : avec blowup 8, le domaine N = nextPow2((deg+1)*8) est alors
	// strictement supérieur à Blowup (au moins un pliage). Un degré 0 (constante)
	// donnerait N == Blowup, cas dégénéré sans pliage exclu par contrat.
	for _, deg := range []int{1, 3, 7, 15, 31, 63} {
		rng := newPRNG(uint64(1000 + deg))
		evals, n := lowDegreeEvals(rng, deg, 8)
		params := defaultParams()
		proof := Prove(evals, params)
		if !Verify(proof, params) {
			t.Fatalf("bas degré rejeté : deg=%d N=%d", deg, n)
		}
	}
}

// TestFriDifférentsBlowups : le test passe pour plusieurs facteurs d'expansion.
func TestFriDifférentsBlowups(t *testing.T) {
	for _, blowup := range []int{2, 4, 8, 16} {
		rng := newPRNG(uint64(2000 + blowup))
		evals, _ := lowDegreeEvals(rng, 7, blowup)
		params := FriParams{Blowup: blowup, NumQueries: 24}
		proof := Prove(evals, params)
		if !Verify(proof, params) {
			t.Fatalf("bas degré rejeté avec blowup=%d", blowup)
		}
	}
}

// TestFriAléatoireÉchoue : une fonction aléatoire sur le domaine est, avec
// probabilité écrasante, loin de tout polynôme de bas degré ; FRI doit la
// rejeter. On répète sur plusieurs graines pour exclure une chance aberrante.
func TestFriAléatoireÉchoue(t *testing.T) {
	params := defaultParams()
	rejets := 0
	essais := 0
	for seed := uint64(0); seed < 12; seed++ {
		rng := newPRNG(3000 + seed)
		n := 256
		evals := make([]Felt, n)
		for i := range evals {
			evals[i] = rng.felt()
		}
		essais++
		proof := Prove(evals, params)
		if !Verify(proof, params) {
			rejets++
		}
	}
	// Avec 32 requêtes et un blowup de 8, la probabilité qu'une fonction
	// aléatoire passe est astronomiquement faible : on exige le rejet de TOUS
	// les essais.
	if rejets != essais {
		t.Fatalf("SOUNDNESS : fonction aléatoire acceptée %d/%d fois", essais-rejets, essais)
	}
}

// TestFriAléatoireÉchoueGrosDomaine : même test sur un domaine plus grand.
func TestFriAléatoireÉchoueGrosDomaine(t *testing.T) {
	params := FriParams{Blowup: 8, NumQueries: 40}
	rng := newPRNG(4242)
	n := 1024
	evals := make([]Felt, n)
	for i := range evals {
		evals[i] = rng.felt()
	}
	proof := Prove(evals, params)
	if Verify(proof, params) {
		t.Fatal("SOUNDNESS : fonction aléatoire (N=1024) acceptée")
	}
}

// TestFriDéterminisme : prouver deux fois la même entrée produit une preuve
// identique bit-à-bit (mêmes racines, mêmes coefficients finaux, mêmes
// ouvertures). Exigence de reproductibilité du prouveur.
func TestFriDéterminisme(t *testing.T) {
	rng1 := newPRNG(5000)
	rng2 := newPRNG(5000)
	evals1, _ := lowDegreeEvals(rng1, 15, 8)
	evals2, _ := lowDegreeEvals(rng2, 15, 8)
	params := defaultParams()

	p1 := Prove(evals1, params)
	p2 := Prove(evals2, params)

	if p1.LogDomain != p2.LogDomain {
		t.Fatal("déterminisme : LogDomain diffère")
	}
	if len(p1.LayerRoots) != len(p2.LayerRoots) {
		t.Fatal("déterminisme : nombre de racines diffère")
	}
	for i := range p1.LayerRoots {
		if p1.LayerRoots[i] != p2.LayerRoots[i] {
			t.Fatalf("déterminisme : racine de couche %d diffère", i)
		}
	}
	if len(p1.FinalCoeffs) != len(p2.FinalCoeffs) {
		t.Fatal("déterminisme : nombre de coefficients finaux diffère")
	}
	for i := range p1.FinalCoeffs {
		if !p1.FinalCoeffs[i].Equal(p2.FinalCoeffs[i]) {
			t.Fatalf("déterminisme : coefficient final %d diffère", i)
		}
	}
	if len(p1.Queries) != len(p2.Queries) {
		t.Fatal("déterminisme : nombre de requêtes diffère")
	}
	for q := range p1.Queries {
		if len(p1.Queries[q]) != len(p2.Queries[q]) {
			t.Fatalf("déterminisme : nombre d'étapes requête %d diffère", q)
		}
		for c := range p1.Queries[q] {
			a, b := p1.Queries[q][c], p2.Queries[q][c]
			if !a.Value.Equal(b.Value) || !a.Sibling.Equal(b.Sibling) {
				t.Fatalf("déterminisme : valeurs requête %d couche %d diffèrent", q, c)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// SOUNDNESS négatif : toute preuve falsifiée DOIT être rejetée.
// ---------------------------------------------------------------------------

// proofForTampering construit une preuve honnête servant de base aux tests de
// falsification.
func proofForTampering(seed uint64) (FriProof, FriParams) {
	rng := newPRNG(seed)
	evals, _ := lowDegreeEvals(rng, 15, 8)
	params := defaultParams()
	return Prove(evals, params), params
}

// TestSoundnessRacineCoucheFalsifiée : altérer une racine de couche casse à la
// fois l'authentification Merkle et les défis rejoués => rejet.
func TestSoundnessRacineCoucheFalsifiée(t *testing.T) {
	proof, params := proofForTampering(6000)
	if !Verify(proof, params) {
		t.Fatal("contrôle : la preuve honnête aurait dû passer")
	}
	for k := range proof.LayerRoots {
		bad := cloneProof(proof)
		bad.LayerRoots[k][0] ^= 0xFF
		if Verify(bad, params) {
			t.Fatalf("SOUNDNESS : racine de couche %d falsifiée acceptée", k)
		}
	}
}

// TestSoundnessValeurOuverteFalsifiée : modifier une valeur ouverte (sans
// rechemin valide) doit être rejeté par la vérification Merkle.
func TestSoundnessValeurOuverteFalsifiée(t *testing.T) {
	proof, params := proofForTampering(6001)
	bad := cloneProof(proof)
	bad.Queries[0][0].Value = bad.Queries[0][0].Value.Add(One())
	if Verify(bad, params) {
		t.Fatal("SOUNDNESS : valeur ouverte falsifiée acceptée")
	}
}

// TestSoundnessJumeauFalsifié : modifier la valeur du jumeau (-x) casse soit la
// vérification Merkle, soit la relation de pliage => rejet.
func TestSoundnessJumeauFalsifié(t *testing.T) {
	proof, params := proofForTampering(6002)
	bad := cloneProof(proof)
	bad.Queries[0][0].Sibling = bad.Queries[0][0].Sibling.Add(One())
	if Verify(bad, params) {
		t.Fatal("SOUNDNESS : jumeau falsifié accepté")
	}
}

// TestSoundnessFriCheminFalsifié : corrompre un hash du chemin de Merkle d'une
// ouverture doit être rejeté.
func TestSoundnessFriCheminFalsifié(t *testing.T) {
	proof, params := proofForTampering(6003)
	bad := cloneProof(proof)
	if len(bad.Queries[0][0].Path) == 0 {
		t.Skip("chemin vide : couche trop petite")
	}
	bad.Queries[0][0].Path[0][0] ^= 0xFF
	if Verify(bad, params) {
		t.Fatal("SOUNDNESS : chemin de Merkle falsifié accepté")
	}
}

// TestSoundnessCoeffsFinauxFalsifiés : altérer la couche finale (de bas degré)
// rompt la cohérence avec le dernier pliage => rejet.
func TestSoundnessCoeffsFinauxFalsifiés(t *testing.T) {
	proof, params := proofForTampering(6004)
	bad := cloneProof(proof)
	bad.FinalCoeffs[0] = bad.FinalCoeffs[0].Add(One())
	if Verify(bad, params) {
		t.Fatal("SOUNDNESS : coefficients finaux falsifiés acceptés")
	}
}

// TestSoundnessCoucheFinaleNonConstante : la couche finale doit être un
// polynôme CONSTANT (critère de bas degré terminal). Y introduire un
// coefficient de degré >= 1 non nul — l'attaque « je prétends un degré élevé,
// je n'ai jamais vraiment plié » — doit être rejeté structurellement.
func TestSoundnessCoucheFinaleNonConstante(t *testing.T) {
	proof, params := proofForTampering(6005)
	bad := cloneProof(proof)
	// On rend la couche finale non constante : coefficient de degré 1 non nul.
	if len(bad.FinalCoeffs) < 2 {
		t.Fatalf("couche finale attendue de longueur Blowup=%d", params.Blowup)
	}
	bad.FinalCoeffs[1] = One()
	if Verify(bad, params) {
		t.Fatal("SOUNDNESS : couche finale non constante acceptée")
	}
}

// TestSoundnessCoucheFinaleMauvaiseLongueur : une couche finale dont la longueur
// ne vaut pas Blowup est mal formée et doit être rejetée.
func TestSoundnessCoucheFinaleMauvaiseLongueur(t *testing.T) {
	proof, params := proofForTampering(6105)
	bad := cloneProof(proof)
	bad.FinalCoeffs = bad.FinalCoeffs[:len(bad.FinalCoeffs)-1]
	if Verify(bad, params) {
		t.Fatal("SOUNDNESS : couche finale de mauvaise longueur acceptée")
	}
}

// TestSoundnessNombreRequêtesFalsifié : retirer des requêtes (preuve tronquée)
// doit être rejeté (le vérifieur exige exactement NumQueries requêtes).
func TestSoundnessNombreRequêtesFalsifié(t *testing.T) {
	proof, params := proofForTampering(6006)
	bad := cloneProof(proof)
	bad.Queries = bad.Queries[:len(bad.Queries)-1]
	if Verify(bad, params) {
		t.Fatal("SOUNDNESS : preuve avec requêtes manquantes acceptée")
	}
}

// TestSoundnessNombreCouchesFalsifié : retirer une racine de couche (donc
// désaligner le nombre de couches) doit être rejeté.
func TestSoundnessNombreCouchesFalsifié(t *testing.T) {
	proof, params := proofForTampering(6007)
	bad := cloneProof(proof)
	bad.LayerRoots = bad.LayerRoots[:len(bad.LayerRoots)-1]
	if Verify(bad, params) {
		t.Fatal("SOUNDNESS : nombre de couches falsifié accepté")
	}
}

// TestSoundnessLogDomainFalsifié : annoncer une mauvaise taille de domaine doit
// être rejeté.
func TestSoundnessLogDomainFalsifié(t *testing.T) {
	proof, params := proofForTampering(6008)
	bad := cloneProof(proof)
	bad.LogDomain++
	if Verify(bad, params) {
		t.Fatal("SOUNDNESS : LogDomain falsifié accepté")
	}
	bad2 := cloneProof(proof)
	bad2.LogDomain-- // change le nombre de couches attendu
	if Verify(bad2, params) {
		t.Fatal("SOUNDNESS : LogDomain réduit accepté")
	}
}

// TestSoundnessParamsDivergents : vérifier avec des paramètres différents de
// ceux de la preuve (changement de défis Fiat-Shamir) doit échouer.
func TestSoundnessParamsDivergents(t *testing.T) {
	proof, params := proofForTampering(6009)
	// NumQueries différent : le vérifieur attend un autre nombre de requêtes.
	if Verify(proof, FriParams{Blowup: params.Blowup, NumQueries: params.NumQueries + 1}) {
		t.Fatal("SOUNDNESS : NumQueries divergent accepté")
	}
	// Blowup différent : change l'absorption initiale donc tous les défis.
	if Verify(proof, FriParams{Blowup: params.Blowup * 2, NumQueries: params.NumQueries}) {
		t.Fatal("SOUNDNESS : Blowup divergent accepté")
	}
}

// TestSoundnessRelationPliageRompue : on remplace une couche pliée par un
// engagement honnête d'un AUTRE polynôme valide. La vérification Merkle passe,
// mais la relation de pliage avec la couche précédente est rompue => rejet.
// C'est le test crucial : il isole le contrôle de cohérence du pliage de la
// simple authenticité Merkle.
func TestSoundnessRelationPliageRompue(t *testing.T) {
	rng := newPRNG(7000)
	evals, _ := lowDegreeEvals(rng, 15, 8)
	params := defaultParams()
	proof := Prove(evals, params)
	if !Verify(proof, params) {
		t.Fatal("contrôle : preuve honnête doit passer")
	}
	if len(proof.LayerRoots) < 2 {
		t.Skip("pas assez de couches pour ce test")
	}

	// On reprouve une fonction de bas degré DIFFÉRENTE et on greffe l'une de ses
	// couches (racine + ouvertures) dans la preuve d'origine. Les ouvertures
	// seront authentiques vis-à-vis de la racine greffée mais incohérentes avec
	// le pliage de la couche précédente d'origine.
	rng2 := newPRNG(7001)
	evals2, _ := lowDegreeEvals(rng2, 15, 8)
	proof2 := Prove(evals2, params)

	bad := cloneProof(proof)
	// Greffe la couche 1 (racine + toutes les ouvertures de couche 1) depuis
	// proof2. La couche 0 reste celle d'origine ; la relation couche0->couche1
	// est donc cassée.
	bad.LayerRoots[1] = proof2.LayerRoots[1]
	for q := range bad.Queries {
		bad.Queries[q][1] = proof2.Queries[q][1]
	}
	if Verify(bad, params) {
		t.Fatal("SOUNDNESS : couche greffée incohérente (pliage rompu) acceptée")
	}
}

// TestSoundnessÉchangeValeurJumeau : on échange Value et Sibling (avec leurs
// chemins). Ces deux feuilles sont à des positions différentes de l'arbre (p et
// p+half) ; l'authentification positionnelle de Merkle (liaison index<->chemin)
// doit donc rejeter, et à défaut la relation de pliage casse.
func TestSoundnessÉchangeValeurJumeau(t *testing.T) {
	proof, params := proofForTampering(6010)
	bad := cloneProof(proof)
	// Échange Value<->Sibling (et leurs chemins) : la valeur de la position p se
	// retrouve annoncée pour la position p+half et vice-versa => l'index ne colle
	// plus aux chemins => rejet Merkle (ou pliage).
	v := bad.Queries[0][0].Value
	vp := bad.Queries[0][0].Path
	bad.Queries[0][0].Value = bad.Queries[0][0].Sibling
	bad.Queries[0][0].Path = bad.Queries[0][0].SiblingPath
	bad.Queries[0][0].Sibling = v
	bad.Queries[0][0].SiblingPath = vp
	if Verify(bad, params) {
		t.Fatal("SOUNDNESS : échange valeur/jumeau accepté")
	}
}

// TestProveEntréeInvalidePanique : Prove panique sur des entrées mal formées.
func TestProveEntréeInvalidePanique(t *testing.T) {
	mustPanic := func(name string, f func()) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("%s : panique attendue", name)
			}
		}()
		f()
	}
	good := make([]Felt, 64)
	mustPanic("taille non pow2", func() { Prove(make([]Felt, 3), defaultParams()) })
	mustPanic("blowup non pow2", func() { Prove(good, FriParams{Blowup: 3, NumQueries: 8}) })
	mustPanic("num-queries nul", func() { Prove(good, FriParams{Blowup: 8, NumQueries: 0}) })
}

// cloneProof effectue une copie PROFONDE d'une FriProof pour que les tests de
// falsification ne contaminent pas la preuve d'origine.
func cloneProof(p FriProof) FriProof {
	out := FriProof{
		LogDomain:   p.LogDomain,
		LayerRoots:  clone32(p.LayerRoots),
		FinalCoeffs: clone(p.FinalCoeffs),
		Queries:     make([][]QueryStep, len(p.Queries)),
	}
	for q := range p.Queries {
		steps := make([]QueryStep, len(p.Queries[q]))
		for c := range p.Queries[q] {
			s := p.Queries[q][c]
			steps[c] = QueryStep{
				Value:       s.Value,
				Sibling:     s.Sibling,
				Path:        clone32(s.Path),
				SiblingPath: clone32(s.SiblingPath),
			}
		}
		out.Queries[q] = steps
	}
	return out
}
