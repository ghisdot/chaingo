package shieldedwallet

import (
	"testing"

	"chaingo/internal/stark"
)

// Ces tests sont RAPIDES : ils ne génèrent AUCUNE preuve zk-STARK. Ils valident la
// construction du témoin (chemin de Merkle aligné sur le pool) et ses gardes —
// l'alignement bout-en-bout avec le consensus est prouvé dans le test
// d'intégration internal/state (qui, lui, génère l'unique preuve).

// poolRootLike reproduit EXACTEMENT le calcul de racine de la machine d'état
// (internal/state.poolRoot) : feuilles = engagements puis digests NULS jusqu'à
// 2^SpendDepth(), puis PoseidonCommit. Sert de référence aux tests.
func poolRootLike(t *testing.T, commitments [][]byte) [digestLen]stark.Felt {
	t.Helper()
	full := 1 << uint(stark.SpendDepth())
	leaves := make([][digestLen]stark.Felt, full)
	for i, cm := range commitments {
		d, err := bytesToDigest(cm)
		if err != nil {
			t.Fatalf("engagement %d: %v", i, err)
		}
		leaves[i] = d
	}
	root, _ := stark.PoseidonCommit(leaves)
	return root
}

// sibsFromPath extrait les siblings du SpendPath sous la forme attendue par
// PoseidonVerifyPath (du bas vers le haut).
func sibsFromPath(p stark.SpendPath) [][digestLen]stark.Felt {
	out := make([][digestLen]stark.Felt, stark.SpendDepth())
	for i := range out {
		out[i] = p.Siblings[i]
	}
	return out
}

// TestBuildWitnessPathMatchesPoolRoot : le chemin construit par BuildWitness
// reconstruit BIEN la racine du pool (même padding, même convention de bits) — sans
// aucune preuve. C'est l'invariant critique « arbre wallet == arbre consensus ».
func TestBuildWitnessPathMatchesPoolRoot(t *testing.T) {
	// Plusieurs positions pour couvrir des motifs de bits variés.
	for _, index := range []int{0, 1, 2, 5} {
		commits := make([][]byte, index+3) // un pool de quelques notes
		var inNote Note
		for i := range commits {
			n := Note{
				Value: uint64(100 + i),
				Nk:    DeriveNk([]byte("wtest/nk"), uint64(i)),
				Rho:   DeriveRho([]byte("wtest/rho"), uint64(i)),
			}
			commits[i] = n.CommitmentBytes()
			if i == index {
				inNote = n
			}
		}
		out := Note{Value: inNote.Value - 7, Nk: DeriveNk([]byte("wtest/out"), 0), Rho: DeriveRho([]byte("wtest/out"), 1)}
		w, _, err := BuildWitness(commits, SpendPlan{In: inNote, Out: out, Fee: 7})
		if err != nil {
			t.Fatalf("index %d: BuildWitness: %v", index, err)
		}
		root := poolRootLike(t, commits)
		leaf := inNote.Commitment()
		if !stark.PoseidonVerifyPath(root, index, leaf, sibsFromPath(w.Path)) {
			t.Fatalf("index %d: chemin reconstruit ne vérifie pas contre la racine du pool", index)
		}
	}
}

// TestBuildWitnessRejectsAbsentNote : dépenser une note absente du pool échoue.
func TestBuildWitnessRejectsAbsentNote(t *testing.T) {
	commits := [][]byte{
		(Note{Value: 1, Nk: DeriveNk([]byte("a"), 0), Rho: DeriveRho([]byte("a"), 0)}).CommitmentBytes(),
	}
	absent := Note{Value: 99, Nk: DeriveNk([]byte("ghost"), 0), Rho: DeriveRho([]byte("ghost"), 0)}
	out := Note{Value: 90, Nk: DeriveNk([]byte("o"), 0), Rho: DeriveRho([]byte("o"), 0)}
	if _, _, err := BuildWitness(commits, SpendPlan{In: absent, Out: out, Fee: 9}); err == nil {
		t.Fatal("une note absente du pool aurait dû être rejetée")
	}
}

// TestBuildWitnessRejectsBrokenConservation : in != out + fee est refusé.
func TestBuildWitnessRejectsBrokenConservation(t *testing.T) {
	in := Note{Value: 100, Nk: DeriveNk([]byte("c"), 0), Rho: DeriveRho([]byte("c"), 0)}
	commits := [][]byte{in.CommitmentBytes()}
	out := Note{Value: 50, Nk: DeriveNk([]byte("o"), 0), Rho: DeriveRho([]byte("o"), 0)}
	// 100 != 50 + 10 -> conservation rompue.
	if _, _, err := BuildWitness(commits, SpendPlan{In: in, Out: out, Fee: 10}); err == nil {
		t.Fatal("conservation rompue (in != out + fee) aurait dû être rejetée")
	}
}

// TestBuildWitnessRejectsFullPool : au-delà de 2^SpendDepth() engagements, refus.
func TestBuildWitnessRejectsFullPool(t *testing.T) {
	full := 1 << uint(stark.SpendDepth())
	commits := make([][]byte, full+1) // un de trop
	for i := range commits {
		commits[i] = (Note{Value: uint64(i + 1), Nk: DeriveNk([]byte("f"), uint64(i)), Rho: DeriveRho([]byte("f"), uint64(i))}).CommitmentBytes()
	}
	in := Note{Value: 1, Nk: DeriveNk([]byte("f"), 0), Rho: DeriveRho([]byte("f"), 0)}
	out := Note{Value: 0, Nk: DeriveNk([]byte("o"), 0), Rho: DeriveRho([]byte("o"), 0)}
	if _, _, err := BuildWitness(commits, SpendPlan{In: in, Out: out, Fee: 1}); err == nil {
		t.Fatal("un pool plein (> capacité) aurait dû être rejeté")
	}
}
