package stark

import (
	"math/bits"
	"testing"
)

// --- Grinding Fiat-Shamir ---

// Le nonce trouvé par Grind satisfait la preuve de travail, un vérifieur qui
// rejoue les MÊMES absorptions l'accepte, et les transcripts restent synchrones
// (un défi tiré après Grind/VerifyGrind coïncide des deux côtés).
func TestGrind_RoundtripEtSync(t *testing.T) {
	const grindBits = 12 // 2^12 hachages : instantané

	mk := func() *Transcript {
		tr := NewTranscript("test/grind")
		tr.Absorb("ctx", []byte("engagements"))
		return tr
	}

	// Prouveur.
	pr := mk()
	nonce := pr.Grind("pow", grindBits)
	provChal := pr.Challenge("after")

	// Le nonce DOIT avoir au moins grindBits zéros de tête sur le digest.
	probe := mk()
	if !probe.grindOK("pow", nonce, grindBits) {
		t.Fatalf("nonce %d ne satisfait pas la PoW de %d bits", nonce, grindBits)
	}

	// Vérifieur : rejoue VerifyGrind avec le nonce, puis le même défi.
	ve := mk()
	if !ve.VerifyGrind("pow", nonce, grindBits) {
		t.Fatalf("VerifyGrind rejette un nonce honnête")
	}
	verChal := ve.Challenge("after")
	if !provChal.Equal(verChal) {
		t.Fatalf("transcripts désynchronisés après grind: %v != %v", provChal, verChal)
	}
}

// Grind renvoie le PLUS PETIT nonce valide (parcours croissant) : tous les nonces
// strictement inférieurs échouent à la PoW.
func TestGrind_PlusPetitNonce(t *testing.T) {
	const grindBits = 10
	tr := NewTranscript("test/grind-min")
	tr.Absorb("ctx", []byte("x"))
	nonce := tr.Grind("pow", grindBits)

	probe := NewTranscript("test/grind-min")
	probe.Absorb("ctx", []byte("x"))
	for cand := uint64(0); cand < nonce; cand++ {
		if probe.grindOK("pow", cand, grindBits) {
			t.Fatalf("nonce %d valide mais < %d renvoyé par Grind", cand, nonce)
		}
	}
}

// Un mauvais nonce est rejeté par VerifyGrind.
func TestGrind_MauvaisNonceRejete(t *testing.T) {
	const grindBits = 16
	tr := NewTranscript("test/grind-bad")
	tr.Absorb("ctx", []byte("y"))
	good := tr.Grind("pow", grindBits)

	ve := NewTranscript("test/grind-bad")
	ve.Absorb("ctx", []byte("y"))
	// good+1 est presque sûrement invalide pour 16 bits.
	if ve.VerifyGrind("pow", good+1, grindBits) {
		t.Fatalf("VerifyGrind accepte un nonce invalide")
	}
}

// bits <= 0 désactive le grinding : nonce 0 accepté immédiatement.
func TestGrind_Desactive(t *testing.T) {
	tr := NewTranscript("test/grind-off")
	if n := tr.Grind("pow", 0); n != 0 {
		t.Fatalf("grinding désactivé devrait renvoyer le nonce 0, obtenu %d", n)
	}
}

// --- Échantillonnage sans remise ---

// ChallengeIndicesDistinct renvoie des indices deux à deux distincts, dans la
// plage, en quantité exacte, et de façon déterministe.
func TestChallengeIndicesDistinct_Proprietes(t *testing.T) {
	const count, max = 32, 1024
	mk := func() *Transcript {
		tr := NewTranscript("test/distinct")
		tr.Absorb("ctx", []byte("z"))
		return tr
	}
	a := mk().ChallengeIndicesDistinct("q", count, max)
	b := mk().ChallengeIndicesDistinct("q", count, max)

	if len(a) != count {
		t.Fatalf("attendu %d indices, obtenu %d", count, len(a))
	}
	seen := map[int]bool{}
	for _, v := range a {
		if v < 0 || v >= max {
			t.Fatalf("indice hors plage: %d", v)
		}
		if seen[v] {
			t.Fatalf("doublon malgré sans-remise: %d", v)
		}
		seen[v] = true
	}
	// Déterminisme : deux dérivations identiques.
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non déterministe à l'indice %d: %d != %d", i, a[i], b[i])
		}
	}
}

// count == max : on doit obtenir TOUTE la plage (permutation déterministe).
func TestChallengeIndicesDistinct_Permutation(t *testing.T) {
	const max = 64
	tr := NewTranscript("test/perm")
	got := tr.ChallengeIndicesDistinct("q", max, max)
	if len(got) != max {
		t.Fatalf("attendu %d, obtenu %d", max, len(got))
	}
	seen := make([]bool, max)
	for _, v := range got {
		if seen[v] {
			t.Fatalf("doublon %d dans une permutation", v)
		}
		seen[v] = true
	}
}

// count > max doit paniquer (distinctes impossibles).
func TestChallengeIndicesDistinct_PaniqueCountTropGrand(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("count > max aurait dû paniquer")
		}
	}()
	tr := NewTranscript("test/panic")
	tr.ChallengeIndicesDistinct("q", 10, 9)
}

// Cohérence du compteur de zéros de tête local avec math/bits.
func TestLeadingZeros64_Coherence(t *testing.T) {
	for _, x := range []uint64{0, 1, 2, 255, 1 << 30, 1 << 63, ^uint64(0)} {
		if leadingZeros64(x) != bits.LeadingZeros64(x) {
			t.Fatalf("leadingZeros64(%d)=%d != %d", x, leadingZeros64(x), bits.LeadingZeros64(x))
		}
	}
}
