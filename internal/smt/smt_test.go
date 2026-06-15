package smt

import (
	"bytes"
	"testing"
)

// TestEmptyRootIsDefault : l'arbre vide a la racine par défaut, stable.
func TestEmptyRootIsDefault(t *testing.T) {
	a, b := New(), New()
	if !bytes.Equal(a.Root(), b.Root()) {
		t.Fatal("deux arbres vides doivent avoir la même racine")
	}
	if !bytes.Equal(a.Root(), defaultHashes[0]) {
		t.Fatal("la racine vide doit être defaultHashes[0]")
	}
}

// TestRootChangesOnInsert : insérer une valeur change la racine ; la retirer
// la ramène EXACTEMENT à la racine vide (réversibilité = repli correct).
func TestRootChangesOnInsert(t *testing.T) {
	tr := New()
	empty := tr.Root()
	tr.Update([]byte("cg-alice"), []byte("100"))
	if bytes.Equal(tr.Root(), empty) {
		t.Fatal("la racine doit changer après insertion")
	}
	if tr.Len() != 1 {
		t.Fatalf("1 feuille attendue, got %d", tr.Len())
	}
	tr.Delete([]byte("cg-alice"))
	if !bytes.Equal(tr.Root(), empty) {
		t.Fatal("après suppression, la racine doit revenir à l'arbre vide")
	}
	if tr.Len() != 0 {
		t.Fatalf("0 feuille attendue après suppression, got %d", tr.Len())
	}
}

// TestRootIsOrderIndependent : la racine ne dépend QUE de l'ensemble clé→valeur,
// pas de l'ordre d'insertion (déterminisme inter-nœuds — invariant critique).
func TestRootIsOrderIndependent(t *testing.T) {
	kv := map[string]string{
		"cg-alice": "100", "cg-bob": "250", "cg-carol": "7",
		"cg-dave": "999999", "cg-erin": "1",
	}
	a := New()
	for k, v := range kv {
		a.Update([]byte(k), []byte(v))
	}
	// b : ordre inverse + une insertion superflue écrasée.
	b := New()
	b.Update([]byte("cg-erin"), []byte("0-bad"))
	keys := []string{"cg-erin", "cg-dave", "cg-carol", "cg-bob", "cg-alice"}
	for _, k := range keys {
		b.Update([]byte(k), []byte(kv[k]))
	}
	if !bytes.Equal(a.Root(), b.Root()) {
		t.Fatal("la racine doit être indépendante de l'ordre des insertions")
	}
}

// TestUpdateChangesRoot : changer la valeur d'une clé existante change la racine.
func TestUpdateChangesRoot(t *testing.T) {
	tr := New()
	tr.Update([]byte("cg-alice"), []byte("100"))
	r1 := tr.Root()
	tr.Update([]byte("cg-alice"), []byte("101"))
	if bytes.Equal(tr.Root(), r1) {
		t.Fatal("changer la valeur doit changer la racine")
	}
}

// TestInclusionProof : une preuve d'inclusion vérifie contre la racine, et est
// invalidée par une mauvaise racine.
func TestInclusionProof(t *testing.T) {
	tr := New()
	for _, k := range []string{"cg-a", "cg-b", "cg-c", "cg-d"} {
		tr.Update([]byte(k), []byte("v-"+k))
	}
	root := tr.Root()
	p := tr.Prove([]byte("cg-c"))
	if p.Leaf == nil {
		t.Fatal("cg-c est présent : la preuve doit être une inclusion")
	}
	if !p.Verify(root) {
		t.Fatal("la preuve d'inclusion doit vérifier contre la racine")
	}
	// Une racine altérée doit faire échouer la vérification.
	bad := append([]byte(nil), root...)
	bad[0] ^= 0xff
	if p.Verify(bad) {
		t.Fatal("la preuve ne doit pas vérifier contre une mauvaise racine")
	}
}

// TestExclusionProof : une preuve d'exclusion (clé absente) vérifie aussi.
func TestExclusionProof(t *testing.T) {
	tr := New()
	tr.Update([]byte("cg-a"), []byte("1"))
	tr.Update([]byte("cg-b"), []byte("2"))
	root := tr.Root()
	p := tr.Prove([]byte("cg-absent"))
	if p.Leaf != nil {
		t.Fatal("cg-absent n'existe pas : la preuve doit être une exclusion (Leaf nil)")
	}
	if !p.Verify(root) {
		t.Fatal("la preuve d'exclusion doit vérifier contre la racine")
	}
}

// TestProofBreaksAfterChange : une preuve devient invalide si l'arbre change
// (anti-rejeu d'une vieille preuve contre une nouvelle racine).
func TestProofBreaksAfterChange(t *testing.T) {
	tr := New()
	tr.Update([]byte("cg-a"), []byte("1"))
	p := tr.Prove([]byte("cg-a"))
	oldRoot := tr.Root()
	if !p.Verify(oldRoot) {
		t.Fatal("preuve valide contre la racine courante")
	}
	tr.Update([]byte("cg-b"), []byte("2")) // l'arbre évolue
	if p.Verify(tr.Root()) {
		t.Fatal("une vieille preuve ne doit pas vérifier contre la nouvelle racine")
	}
}
