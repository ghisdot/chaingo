package stark

import "testing"

// BenchmarkProveSpend mesure le coût du prouveur de dépense blindée (baseline
// pour le durcissement perf). Fichier de mesure temporaire.
func BenchmarkProveSpend(b *testing.B) {
	w, fee, _ := spBuildScenario("bench", 5)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ProveSpend(w, fee)
	}
}

// BenchmarkVerifySpend mesure le coût du vérifieur (doit rester rapide).
func BenchmarkVerifySpend(b *testing.B) {
	w, fee, _ := spBuildScenario("bench", 5)
	public, proof := ProveSpend(w, fee)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !VerifySpend(public, proof) {
			b.Fatal("preuve honnête rejetée")
		}
	}
}
