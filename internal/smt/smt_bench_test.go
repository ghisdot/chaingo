package smt

import (
	"encoding/binary"
	"testing"
)

// BenchmarkUpdate démontre la propriété centrale de #9 : le coût d'une mise à
// jour (+ racine) est ~indépendant du nombre de feuilles déjà présentes
// (O(profondeur)), là où l'ancien hachage du JSON complet était O(n).
//
//   go test ./internal/smt/ -bench=Update -benchmem -run='^$'
//
// Comparer Update/preload=100 et Update/preload=100000 : le ns/op doit rester
// du même ordre (pas multiplié par 1000).
func BenchmarkUpdate(b *testing.B) {
	for _, preload := range []int{100, 10_000, 100_000} {
		b.Run(name(preload), func(b *testing.B) {
			tr := New()
			key := make([]byte, 8)
			for i := 0; i < preload; i++ {
				binary.BigEndian.PutUint64(key, uint64(i))
				tr.Update(key, []byte("v"))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				binary.BigEndian.PutUint64(key, uint64(preload+i))
				tr.Update(key, []byte("v"))
				_ = tr.Root()
			}
		})
	}
}

func name(n int) string {
	switch {
	case n >= 100_000:
		return "preload=100000"
	case n >= 10_000:
		return "preload=10000"
	default:
		return "preload=100"
	}
}
