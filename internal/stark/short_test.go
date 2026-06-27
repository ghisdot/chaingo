package stark

import "testing"

// skipShort saute les tests qui génèrent une (ou plusieurs) preuve(s) zk-STARK —
// lents à la profondeur d'arbre de production (depth 12). En mode `-short`, on ne
// garde que les contrôles rapides (corps, NTT, Poseidon, Merkle, transcript, FRI,
// traces natives, codecs) ; le `go test` complet (CI) exécute tout.
func skipShort(t *testing.T) {
	if testing.Short() {
		t.Skip("preuve zk lourde (depth 12) — exclue en -short")
	}
}
