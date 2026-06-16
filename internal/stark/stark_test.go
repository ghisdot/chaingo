// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Tests du STARK Fibonacci de bout en bout. On vérifie :
//   - CHEMIN HEUREUX : une preuve honnête (bonne sortie publique) VÉRIFIE, pour
//     plusieurs tailles de trace n.
//   - SOUNDNESS (mauvaise sortie) : prouver/vérifier avec une sortie publique
//     fausse ÉCHOUE.
//   - SOUNDNESS (falsification) : toute altération de la preuve (racines,
//     valeurs hors-domaine, ouvertures, chemins de Merkle, paramètres n) ÉCHOUE.
//   - DÉTERMINISME : deux preuves de la même instance sont identiques bit-à-bit.
//
// Tout est déterministe : aucune dépendance à time / math/rand. On réutilise le
// PRNG à graine fixe et les utilitaires de clonage des autres fichiers de test.
package stark

import "testing"

// fibValue calcule la vraie valeur t[n-1] de la suite Fibonacci (t[0]=t[1]=1)
// dans le corps, indépendamment du prouveur — sert d'oracle pour les tests.
func fibValue(n int) Felt {
	t := buildTrace(n)
	return t[n-1]
}

// TestStarkHonnêteVérifie : pour diverses tailles n (puissances de 2 >= 4), une
// preuve produite avec la VRAIE sortie publique doit être acceptée.
func TestStarkHonnêteVérifie(t *testing.T) {
	for _, n := range []int{4, 8, 16, 32, 64, 128} {
		out := fibValue(n)
		proof := ProveFib(n, out)
		if !VerifyFib(proof, n, out) {
			t.Fatalf("preuve honnête rejetée pour n=%d (sortie=%d)", n, out.Uint64())
		}
	}
}

// TestStarkMauvaiseSortieÉchoue : prouver avec une sortie publique FAUSSE doit
// produire une preuve que la vérification rejette (les contraintes de bord ne
// tiennent pas, donc la composition n'est pas un vrai polynôme et/ou le contrôle
// hors-domaine échoue).
func TestStarkMauvaiseSortieÉchoue(t *testing.T) {
	for _, n := range []int{8, 16, 32} {
		vraie := fibValue(n)
		fausse := vraie.Add(One()) // off-by-one : sortie publique incorrecte

		// Le prouveur engage la VRAIE trace mais annonce la FAUSSE sortie : la
		// preuve produite ne doit pas vérifier contre la fausse sortie.
		proof := ProveFib(n, fausse)
		if VerifyFib(proof, n, fausse) {
			t.Fatalf("SOUNDNESS : preuve à sortie fausse acceptée pour n=%d", n)
		}

		// De plus, une preuve honnête (vraie sortie) ne doit PAS vérifier si on la
		// présente comme prouvant la fausse sortie.
		bonne := ProveFib(n, vraie)
		if VerifyFib(bonne, n, fausse) {
			t.Fatalf("SOUNDNESS : preuve honnête acceptée contre sortie fausse n=%d", n)
		}
	}
}

// TestStarkBonneSortieMauvaisN : vérifier une preuve avec un n différent de celui
// prouvé doit échouer (l'énoncé public n fait partie du transcript).
func TestStarkBonneSortieMauvaisN(t *testing.T) {
	n := 32
	out := fibValue(n)
	proof := ProveFib(n, out)
	// On garde la même sortie mais on change n : le transcript diverge, la
	// structure FRI attendue diverge => rejet.
	if VerifyFib(proof, 64, out) {
		t.Fatal("SOUNDNESS : preuve acceptée avec mauvais n (64 au lieu de 32)")
	}
	if VerifyFib(proof, 16, out) {
		t.Fatal("SOUNDNESS : preuve acceptée avec mauvais n (16 au lieu de 32)")
	}
}

// ---------------------------------------------------------------------------
// SOUNDNESS négatif par falsification ciblée d'une preuve honnête.
// ---------------------------------------------------------------------------

// honnête construit une preuve honnête de référence pour n donné.
func honnête(n int) (FibProof, Felt) {
	out := fibValue(n)
	return ProveFib(n, out), out
}

// TestStarkRacineTraceFalsifiée : altérer l'engagement de la trace doit être
// rejeté (authentification Merkle + défis rejoués).
func TestStarkRacineTraceFalsifiée(t *testing.T) {
	proof, out := honnête(32)
	if !VerifyFib(proof, 32, out) {
		t.Fatal("contrôle : preuve honnête doit passer")
	}
	bad := cloneFibProof(proof)
	bad.TraceRoot[0] ^= 0xFF
	if VerifyFib(bad, 32, out) {
		t.Fatal("SOUNDNESS : racine de trace falsifiée acceptée")
	}
}

// TestStarkRacineCompFalsifiée : altérer l'engagement de la composition doit
// être rejeté.
func TestStarkRacineCompFalsifiée(t *testing.T) {
	proof, out := honnête(32)
	bad := cloneFibProof(proof)
	bad.CompRoot[0] ^= 0xFF
	if VerifyFib(bad, 32, out) {
		t.Fatal("SOUNDNESS : racine de composition falsifiée acceptée")
	}
}

// TestStarkOodFalsifiée : altérer une valeur hors-domaine doit casser soit le
// contrôle algébrique en z, soit la cohérence DEEP aux positions de requête.
func TestStarkOodFalsifiée(t *testing.T) {
	proof, out := honnête(32)

	bad := cloneFibProof(proof)
	bad.OodTz = bad.OodTz.Add(One())
	if VerifyFib(bad, 32, out) {
		t.Fatal("SOUNDNESS : T(z) falsifié accepté")
	}

	bad2 := cloneFibProof(proof)
	bad2.OodHz = bad2.OodHz.Add(One())
	if VerifyFib(bad2, 32, out) {
		t.Fatal("SOUNDNESS : H(z) falsifié accepté")
	}

	bad3 := cloneFibProof(proof)
	bad3.OodTgz = bad3.OodTgz.Add(One())
	if VerifyFib(bad3, 32, out) {
		t.Fatal("SOUNDNESS : T(g·z) falsifié accepté")
	}

	bad4 := cloneFibProof(proof)
	bad4.OodTg2z = bad4.OodTg2z.Add(One())
	if VerifyFib(bad4, 32, out) {
		t.Fatal("SOUNDNESS : T(g²·z) falsifié accepté")
	}
}

// TestStarkValeurOuverteFalsifiée : modifier une valeur ouverte (trace ou
// composition) sans chemin valide doit être rejeté par Merkle ou par la
// cohérence DEEP.
func TestStarkValeurOuverteFalsifiée(t *testing.T) {
	proof, out := honnête(32)

	bad := cloneFibProof(proof)
	bad.Openings[0].TraceVal = bad.Openings[0].TraceVal.Add(One())
	if VerifyFib(bad, 32, out) {
		t.Fatal("SOUNDNESS : valeur de trace ouverte falsifiée acceptée")
	}

	bad2 := cloneFibProof(proof)
	bad2.Openings[0].CompVal = bad2.Openings[0].CompVal.Add(One())
	if VerifyFib(bad2, 32, out) {
		t.Fatal("SOUNDNESS : valeur de composition ouverte falsifiée acceptée")
	}

	bad3 := cloneFibProof(proof)
	bad3.Openings[0].DeepVal = bad3.Openings[0].DeepVal.Add(One())
	if VerifyFib(bad3, 32, out) {
		t.Fatal("SOUNDNESS : valeur DEEP ouverte falsifiée acceptée")
	}
}

// TestStarkCheminFalsifié : corrompre un hash d'un chemin de Merkle d'ouverture
// doit être rejeté.
func TestStarkCheminFalsifié(t *testing.T) {
	proof, out := honnête(32)

	bad := cloneFibProof(proof)
	if len(bad.Openings[0].TracePath) == 0 {
		t.Skip("chemin vide")
	}
	bad.Openings[0].TracePath[0][0] ^= 0xFF
	if VerifyFib(bad, 32, out) {
		t.Fatal("SOUNDNESS : chemin de Merkle de trace falsifié accepté")
	}

	bad2 := cloneFibProof(proof)
	bad2.Openings[0].DeepPath[0][0] ^= 0xFF
	if VerifyFib(bad2, 32, out) {
		t.Fatal("SOUNDNESS : chemin de Merkle DEEP falsifié accepté")
	}
}

// TestStarkPositionFalsifiée : annoncer une position d'ouverture incohérente avec
// celle tirée par le transcript doit être rejeté.
func TestStarkPositionFalsifiée(t *testing.T) {
	proof, out := honnête(32)
	bad := cloneFibProof(proof)
	bad.Openings[0].Pos = (bad.Openings[0].Pos + 1) % (starkBlowup * 32)
	if VerifyFib(bad, 32, out) {
		t.Fatal("SOUNDNESS : position d'ouverture falsifiée acceptée")
	}
}

// TestStarkFriFalsifié : altérer la preuve FRI interne (racine de couche, coeff
// final) doit être rejeté.
func TestStarkFriFalsifié(t *testing.T) {
	proof, out := honnête(32)

	bad := cloneFibProof(proof)
	bad.Fri.LayerRoots[0][0] ^= 0xFF
	if VerifyFib(bad, 32, out) {
		t.Fatal("SOUNDNESS : racine de couche FRI falsifiée acceptée")
	}

	bad2 := cloneFibProof(proof)
	bad2.Fri.FinalCoeffs[0] = bad2.Fri.FinalCoeffs[0].Add(One())
	if VerifyFib(bad2, 32, out) {
		t.Fatal("SOUNDNESS : coefficient final FRI falsifié accepté")
	}
}

// TestStarkNombreOuverturesFalsifié : tronquer la liste des ouvertures doit être
// rejeté (le vérifieur exige exactement starkNumQueries ouvertures).
func TestStarkNombreOuverturesFalsifié(t *testing.T) {
	proof, out := honnête(32)
	bad := cloneFibProof(proof)
	bad.Openings = bad.Openings[:len(bad.Openings)-1]
	if VerifyFib(bad, 32, out) {
		t.Fatal("SOUNDNESS : nombre d'ouvertures tronqué accepté")
	}
}

// TestStarkDéterminisme : prouver deux fois la même instance produit une preuve
// identique bit-à-bit (racines, valeurs hors-domaine, FRI, ouvertures).
func TestStarkDéterminisme(t *testing.T) {
	n := 64
	out := fibValue(n)
	p1 := ProveFib(n, out)
	p2 := ProveFib(n, out)

	if p1.TraceRoot != p2.TraceRoot {
		t.Fatal("déterminisme : TraceRoot diffère")
	}
	if p1.CompRoot != p2.CompRoot {
		t.Fatal("déterminisme : CompRoot diffère")
	}
	if !p1.OodTz.Equal(p2.OodTz) || !p1.OodTgz.Equal(p2.OodTgz) ||
		!p1.OodTg2z.Equal(p2.OodTg2z) || !p1.OodHz.Equal(p2.OodHz) {
		t.Fatal("déterminisme : valeurs hors-domaine diffèrent")
	}
	if len(p1.Fri.LayerRoots) != len(p2.Fri.LayerRoots) {
		t.Fatal("déterminisme : nombre de racines FRI diffère")
	}
	for i := range p1.Fri.LayerRoots {
		if p1.Fri.LayerRoots[i] != p2.Fri.LayerRoots[i] {
			t.Fatalf("déterminisme : racine FRI %d diffère", i)
		}
	}
	if len(p1.Openings) != len(p2.Openings) {
		t.Fatal("déterminisme : nombre d'ouvertures diffère")
	}
	for i := range p1.Openings {
		a, b := p1.Openings[i], p2.Openings[i]
		if a.Pos != b.Pos || !a.TraceVal.Equal(b.TraceVal) ||
			!a.CompVal.Equal(b.CompVal) || !a.DeepVal.Equal(b.DeepVal) {
			t.Fatalf("déterminisme : ouverture %d diffère", i)
		}
	}
}

// TestStarkProveEntréeInvalidePanique : ProveFib panique sur n mal formé.
func TestStarkProveEntréeInvalidePanique(t *testing.T) {
	mustPanic := func(name string, f func()) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("%s : panique attendue", name)
			}
		}()
		f()
	}
	mustPanic("n non pow2", func() { ProveFib(6, One()) })
	mustPanic("n trop petit", func() { ProveFib(2, One()) })
	mustPanic("n nul", func() { ProveFib(0, One()) })
}

// TestStarkVerifyNeJamaisPaniquer : VerifyFib ne doit jamais paniquer, même sur
// des preuves structurellement aberrantes (preuve vide, n invalide).
func TestStarkVerifyNeJamaisPaniquer(t *testing.T) {
	// Preuve zéro-valeur : doit être rejetée proprement, sans panique.
	if VerifyFib(FibProof{}, 32, One()) {
		t.Fatal("preuve vide acceptée")
	}
	// n invalide : rejet propre.
	if VerifyFib(FibProof{}, 6, One()) {
		t.Fatal("n invalide accepté")
	}
	// Preuve honnête mais n incohérent avec la structure FRI.
	proof, out := honnête(16)
	if VerifyFib(proof, 8, out) {
		t.Fatal("n divergent accepté")
	}
}

// ---------------------------------------------------------------------------
// Clonage profond d'une FibProof pour les tests de falsification.
// ---------------------------------------------------------------------------

// cloneFibProof effectue une copie PROFONDE d'une FibProof afin que les
// falsifications d'un test ne contaminent pas la preuve d'origine.
func cloneFibProof(p FibProof) FibProof {
	out := FibProof{
		TraceRoot: p.TraceRoot,
		CompRoot:  p.CompRoot,
		OodTz:     p.OodTz,
		OodTgz:    p.OodTgz,
		OodTg2z:   p.OodTg2z,
		OodHz:     p.OodHz,
		Fri:       cloneProof(p.Fri), // helper de fri_test.go
		Openings:  make([]FibOpening, len(p.Openings)),
	}
	for i := range p.Openings {
		o := p.Openings[i]
		out.Openings[i] = FibOpening{
			Pos:       o.Pos,
			TraceVal:  o.TraceVal,
			TracePath: clone32(o.TracePath), // helper de merkle_test.go
			CompVal:   o.CompVal,
			CompPath:  clone32(o.CompPath),
			DeepVal:   o.DeepVal,
			DeepPath:  clone32(o.DeepPath),
		}
	}
	return out
}
