// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Tests du transcript Fiat-Shamir : déterminisme (mêmes absorptions => mêmes
// défis), divergence (une absorption qui diffère => défis différents), domaine
// des défis (felts dans [0,P), indices dans [0,max)), uniformité grossière, et
// tests de SOUNDNESS négatifs (un transcript falsifié ne reproduit pas les
// défis honnêtes). Entièrement déterministe : aucun time, aucun math/rand.
package stark

import "testing"

// --- Déterminisme ---------------------------------------------------------

// TestTranscriptDéterministeFelt vérifie que deux transcripts ayant subi les
// MÊMES absorptions, dans le MÊME ordre, produisent EXACTEMENT les mêmes défis
// (reproductibilité bit-à-bit : prouveur == vérifieur).
func TestTranscriptDéterministeFelt(t *testing.T) {
	build := func() *Transcript {
		tr := NewTranscript("test-protocole")
		tr.Absorb("racine-merkle", []byte("0123456789abcdef"))
		tr.AbsorbFelt("alpha", FromUint64(42))
		tr.Absorb("vide", nil)
		tr.AbsorbFelt("beta", FromUint64(P-1))
		return tr
	}

	a := build()
	b := build()

	for i := 0; i < 50; i++ {
		ca := a.Challenge("c")
		cb := b.Challenge("c")
		if !ca.Equal(cb) {
			t.Fatalf("défi %d divergent entre deux transcripts identiques : %d != %d", i, ca, cb)
		}
	}
}

// TestTranscriptDéterministeIndices : même chose pour ChallengeIndices.
func TestTranscriptDéterministeIndices(t *testing.T) {
	build := func() *Transcript {
		tr := NewTranscript("fri")
		tr.AbsorbFelt("commit", FromUint64(7))
		return tr
	}
	a := build()
	b := build()

	for round := 0; round < 10; round++ {
		ia := a.ChallengeIndices("queries", 32, 1<<20)
		ib := b.ChallengeIndices("queries", 32, 1<<20)
		if len(ia) != len(ib) {
			t.Fatalf("round %d : longueurs différentes %d vs %d", round, len(ia), len(ib))
		}
		for k := range ia {
			if ia[k] != ib[k] {
				t.Fatalf("round %d indice %d : %d != %d", round, k, ia[k], ib[k])
			}
		}
	}
}

// TestTranscriptCloneIndépendant : un clone reproduit l'original mais diverge
// dès qu'on absorbe différemment dans l'un des deux.
func TestTranscriptCloneIndépendant(t *testing.T) {
	tr := NewTranscript("p")
	tr.Absorb("x", []byte("commun"))

	clone := tr.Clone()

	// Tant qu'on fait pareil, ils coïncident.
	if !tr.Challenge("d").Equal(clone.Challenge("d")) {
		t.Fatal("clone ne reproduit pas l'original avant divergence")
	}

	// Re-cloner après un défi : doit toujours coïncider.
	tr2 := NewTranscript("p")
	tr2.Absorb("x", []byte("commun"))
	c2 := tr2.Clone()
	tr2.Absorb("branche", []byte("A"))
	c2.Absorb("branche", []byte("B"))
	if tr2.Challenge("d").Equal(c2.Challenge("d")) {
		t.Fatal("deux branches d'absorption distinctes donnent le même défi (collision)")
	}
}

// --- Divergence (sensibilité aux entrées) ---------------------------------

// TestTranscriptDivergeSiAbsorptionDiffère : changer la moindre donnée absorbée
// change le défi. On teste plusieurs points de variation.
func TestTranscriptDivergeSiAbsorptionDiffère(t *testing.T) {
	base := func() *Transcript {
		tr := NewTranscript("proto")
		tr.Absorb("a", []byte("hello"))
		tr.AbsorbFelt("b", FromUint64(100))
		return tr
	}

	ref := base().Challenge("out")

	// 1) Donnée différente.
	v1 := func() *Transcript {
		tr := NewTranscript("proto")
		tr.Absorb("a", []byte("hellp")) // 1 octet change
		tr.AbsorbFelt("b", FromUint64(100))
		return tr
	}()
	if v1.Challenge("out").Equal(ref) {
		t.Fatal("changer un octet de donnée n'a pas changé le défi")
	}

	// 2) Felt différent.
	v2 := func() *Transcript {
		tr := NewTranscript("proto")
		tr.Absorb("a", []byte("hello"))
		tr.AbsorbFelt("b", FromUint64(101))
		return tr
	}()
	if v2.Challenge("out").Equal(ref) {
		t.Fatal("changer un felt absorbé n'a pas changé le défi")
	}

	// 3) Étiquette d'absorption différente.
	v3 := func() *Transcript {
		tr := NewTranscript("proto")
		tr.Absorb("A", []byte("hello")) // label diffère
		tr.AbsorbFelt("b", FromUint64(100))
		return tr
	}()
	if v3.Challenge("out").Equal(ref) {
		t.Fatal("changer une étiquette d'absorption n'a pas changé le défi")
	}

	// 4) Domaine de protocole différent.
	v4 := func() *Transcript {
		tr := NewTranscript("proto-2")
		tr.Absorb("a", []byte("hello"))
		tr.AbsorbFelt("b", FromUint64(100))
		return tr
	}()
	if v4.Challenge("out").Equal(ref) {
		t.Fatal("changer le domaine de protocole n'a pas changé le défi")
	}

	// 5) Étiquette de défi différente.
	if base().Challenge("autre").Equal(ref) {
		t.Fatal("changer l'étiquette de défi n'a pas changé le défi")
	}
}

// TestTranscriptOrdreImporte : l'ordre des absorptions est significatif.
func TestTranscriptOrdreImporte(t *testing.T) {
	ab := NewTranscript("o")
	ab.Absorb("k1", []byte("A"))
	ab.Absorb("k2", []byte("B"))

	ba := NewTranscript("o")
	ba.Absorb("k2", []byte("B"))
	ba.Absorb("k1", []byte("A"))

	if ab.Challenge("c").Equal(ba.Challenge("c")) {
		t.Fatal("l'ordre des absorptions ne change pas le défi (cadrage défaillant)")
	}
}

// TestTranscriptCadrageAmbiguïté : le cadrage longueur-préfixée doit empêcher
// qu'une frontière de découpage déplacée produise le même état. Absorber
// ("ab","c") doit différer d'absorber ("a","bc").
func TestTranscriptCadrageAmbiguïté(t *testing.T) {
	t1 := NewTranscript("f")
	t1.Absorb("ab", []byte("c"))

	t2 := NewTranscript("f")
	t2.Absorb("a", []byte("bc"))

	if t1.Challenge("x").Equal(t2.Challenge("x")) {
		t.Fatal("collision de cadrage : (label=ab,data=c) == (label=a,data=bc)")
	}
}

// TestChallengesSuccessifsDiffèrent : deux défis consécutifs SANS absorption
// intercalaire doivent différer (l'état avance à chaque squeeze).
func TestChallengesSuccessifsDiffèrent(t *testing.T) {
	tr := NewTranscript("s")
	tr.AbsorbFelt("x", FromUint64(1))

	seen := map[uint64]bool{}
	for i := 0; i < 100; i++ {
		c := tr.Challenge("rep").Uint64()
		if seen[c] {
			t.Fatalf("défi répété au tour %d (état n'avance pas) : %d", i, c)
		}
		seen[c] = true
	}
}

// --- Domaine des défis -----------------------------------------------------

// TestChallengeDansLeCorps : tout défi-felt est < P (résidu canonique).
func TestChallengeDansLeCorps(t *testing.T) {
	tr := NewTranscript("d")
	tr.Absorb("seed", []byte("graine"))
	for i := 0; i < 100000; i++ {
		c := tr.Challenge("c")
		if c.Uint64() >= P {
			t.Fatalf("défi hors du corps au tour %d : %d >= P", i, c.Uint64())
		}
	}
}

// TestChallengeIndicesDansBorne : tous les indices sont dans [0, max).
func TestChallengeIndicesDansBorne(t *testing.T) {
	tr := NewTranscript("d")
	tr.Absorb("seed", []byte("graine"))

	maxes := []int{1, 2, 3, 7, 16, 17, 1000, 1 << 16, (1 << 20) + 1}
	for _, max := range maxes {
		idx := tr.ChallengeIndices("q", 200, max)
		if len(idx) != 200 {
			t.Fatalf("max=%d : attendu 200 indices, obtenu %d", max, len(idx))
		}
		for _, v := range idx {
			if v < 0 || v >= max {
				t.Fatalf("max=%d : indice hors borne : %d", max, v)
			}
		}
	}
}

// TestChallengeIndicesMaxUn : max==1 force tous les indices à 0.
func TestChallengeIndicesMaxUn(t *testing.T) {
	tr := NewTranscript("d")
	idx := tr.ChallengeIndices("q", 10, 1)
	if len(idx) != 10 {
		t.Fatalf("attendu 10 indices, obtenu %d", len(idx))
	}
	for i, v := range idx {
		if v != 0 {
			t.Fatalf("max=1 : indice %d non nul : %d", i, v)
		}
	}
}

// TestChallengeIndicesCountZéro : count==0 renvoie un slice vide non nil.
func TestChallengeIndicesCountZéro(t *testing.T) {
	tr := NewTranscript("d")
	idx := tr.ChallengeIndices("q", 0, 1000)
	if idx == nil {
		t.Fatal("count=0 devrait renvoyer un slice vide non nil")
	}
	if len(idx) != 0 {
		t.Fatalf("count=0 devrait être vide, obtenu %d", len(idx))
	}
}

// TestChallengeIndicesPanics : entrées invalides => panique.
func TestChallengeIndicesPanics(t *testing.T) {
	mustPanic := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatalf("%s : panique attendue, aucune levée", name)
			}
		}()
		f()
	}
	tr := NewTranscript("d")
	mustPanic("max=0", func() { tr.ChallengeIndices("q", 5, 0) })
	mustPanic("max<0", func() { tr.ChallengeIndices("q", 5, -3) })
	mustPanic("count<0", func() { tr.ChallengeIndices("q", -1, 10) })
}

// TestChallengeUniformitéGrossière : distribution grossièrement uniforme du bit
// de poids faible des défis-felt (sanity check, pas un test statistique fin).
// Sur un grand échantillon, ~50% de bits faibles à 1.
func TestChallengeUniformitéGrossière(t *testing.T) {
	tr := NewTranscript("u")
	tr.Absorb("seed", []byte{1, 2, 3})

	const n = 20000
	ones := 0
	for i := 0; i < n; i++ {
		if tr.Challenge("c").Uint64()&1 == 1 {
			ones++
		}
	}
	// Tolérance large (3%) : on cherche juste l'absence de biais grossier.
	low, high := n*47/100, n*53/100
	if ones < low || ones > high {
		t.Fatalf("biais grossier du bit faible : %d/%d à 1 (attendu ~%d)", ones, n, n/2)
	}
}

// TestIndicesUniformitéGrossière : pour un max non puissance de 2, le rejet de
// biais doit produire une couverture grossièrement uniforme de [0,max). On
// vérifie qu'aucune valeur n'écrase les autres et que toutes les valeurs sont
// atteintes sur un grand échantillon avec un petit max.
func TestIndicesUniformitéGrossière(t *testing.T) {
	tr := NewTranscript("u")
	tr.Absorb("seed", []byte{9})

	const max = 5 // non puissance de 2 : c'est là que le biais apparaîtrait
	const n = 50000
	counts := make([]int, max)
	idx := tr.ChallengeIndices("q", n, max)
	for _, v := range idx {
		counts[v]++
	}
	expected := n / max
	for v, c := range counts {
		// Tolérance 10% : un masque sans rejet biaiserait fortement (certaines
		// valeurs ~2x plus fréquentes), bien au-delà de cette borne.
		if c < expected*90/100 || c > expected*110/100 {
			t.Fatalf("valeur %d : %d occurrences (attendu ~%d) — biais suspect", v, c, expected)
		}
	}
}

// --- SOUNDNESS négatif -----------------------------------------------------

// TestSoundnessTranscriptFalsifié simule un vérifieur honnête et un prouveur
// malhonnête. Le prouveur tente de « rejouer » un défi obtenu pour un message
// m, mais s'engage en réalité sur un message m' != m. Comme le défi dépend de
// tout l'historique absorbé, le vérifieur (qui réabsorbe m') obtient un défi
// DIFFÉRENT : la falsification est rejetée.
func TestSoundnessTranscriptFalsifié(t *testing.T) {
	// Vérifieur honnête : absorbe l'engagement réellement transmis, puis tire
	// le défi.
	verif := func(engagement []byte) Felt {
		tr := NewTranscript("soundness-demo")
		tr.Absorb("engagement", engagement)
		return tr.Challenge("alpha")
	}

	// Le prouveur a calculé sa réponse pour le défi associé à m.
	défiPourM := verif([]byte("message-honnête"))

	// Mais il a en fait commis m' (falsification). Le vérifieur réabsorbe m' et
	// recalcule le défi : il NE DOIT PAS coïncider avec celui de m.
	défiPourMPrime := verif([]byte("message-falsifié"))

	if défiPourM.Equal(défiPourMPrime) {
		t.Fatal("SOUNDNESS : un engagement falsifié produit le même défi (Fiat-Shamir cassé)")
	}
}

// TestSoundnessTroncatureHistorique : un prouveur qui « oublie » d'absorber un
// engagement (transcript tronqué) ne reproduit pas le défi du vérifieur complet.
func TestSoundnessTroncatureHistorique(t *testing.T) {
	complet := NewTranscript("p")
	complet.Absorb("c1", []byte("X"))
	complet.Absorb("c2", []byte("Y"))
	cComplet := complet.Challenge("z")

	tronqué := NewTranscript("p")
	tronqué.Absorb("c1", []byte("X"))
	// "c2" omis intentionnellement.
	cTronqué := tronqué.Challenge("z")

	if cComplet.Equal(cTronqué) {
		t.Fatal("SOUNDNESS : omettre une absorption ne change pas le défi")
	}
}

// TestSoundnessRejetDeBiaisExact : vérifie que la borne d'acceptation du rejet
// de biais correspond bien à (2^64 mod P) == epsilon. Si la constante était
// fausse, soit on rejetterait à tort des valeurs valides (défis non
// reproductibles entre versions), soit on accepterait des valeurs biaisées.
func TestSoundnessRejetDeBiaisExact(t *testing.T) {
	// 2^64 ≡ epsilon (mod P), donc maxAccept = 2^64 - epsilon = P (le seul
	// multiple de P qui tient sur 64 bits, car P > 2^63). On vérifie cette
	// identité arithmétique sans manipuler la constante 2^64 (qui déborde).
	//
	// (1) P doit bien être un multiple de P (trivial mais documente l'intention
	//     : la borne d'acceptation EST P).
	const maxAccept uint64 = P
	if maxAccept%P != 0 {
		t.Fatalf("borne de rejet non multiple de P : %d mod P = %d", maxAccept, maxAccept%P)
	}
	// (2) Le multiple suivant (2*P) doit déborder 64 bits : 2*P >= 2^64, ce qui
	//     équivaut à P > 2^63 - 1, soit P > maxUint64/2.
	const maxUint64 uint64 = 1<<64 - 1
	if P <= maxUint64/2 {
		t.Fatalf("P n'est pas > 2^63 : la borne de rejet ne serait pas maximale (P=%d)", uint64(P))
	}
	// (3) Cohérence avec le reste 2^64 mod P = epsilon : P + epsilon doit
	//     reboucler exactement à 0 modulo 2^64 (débordement), i.e. P+epsilon ==
	//     2^64. On le teste à l'exécution (variables) pour autoriser le wrap
	//     uint64 — en constante, P+epsilon déborderait à la compilation.
	pv := uint64(P)
	ev := uint64(epsilon)
	if pv+ev != 0 {
		t.Fatalf("identité 2^64 = P + epsilon non vérifiée : (P+epsilon) mod 2^64 = %d", pv+ev)
	}
}
