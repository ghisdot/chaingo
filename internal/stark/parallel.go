// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Parallélisme du PROUVEUR uniquement, et STRICTEMENT déterministe. Les boucles
// parallélisées ici écrivent chacune dans des emplacements DISJOINTS d'un slice
// pré-alloué, à partir d'entrées IMMUABLES — il n'y a donc aucune course et le
// résultat est bit-à-bit identique à la version séquentielle, quel que soit le
// nombre de cœurs ou l'ordonnancement.
//
// Ce qui n'est JAMAIS parallélisé : le transcript Fiat-Shamir (ordre des
// absorptions = sécurité), et toute la phase de vérification. Le parallélisme ne
// touche que des calculs algébriques purs (interpolation/LDE par colonne,
// composition ligne par ligne, hachage Merkle par arbre).
package stark

import (
	"runtime"
	"sync"
)

// parallelFor découpe l'intervalle [0, total) en tranches contiguës et exécute
// body(start, end) sur chaque tranche en parallèle (une goroutine par tranche,
// plafonné au nombre de cœurs). body NE DOIT écrire que des emplacements indexés
// dans [start, end) d'un état pré-alloué et ne lire que des données immuables :
// à cette condition la parallélisation est déterministe et sans course.
//
// Pour de petits volumes (ou un seul cœur), exécute simplement en séquentiel afin
// d'éviter tout surcoût de goroutines.
func parallelFor(total int, body func(start, end int)) {
	if total <= 0 {
		return
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > total {
		workers = total
	}
	if workers <= 1 {
		body(0, total)
		return
	}

	// Tranches contiguës aussi égales que possible (les `rem` premières tranches
	// reçoivent un élément de plus). Découpe déterministe (ne dépend pas du temps).
	chunk := total / workers
	rem := total % workers

	var wg sync.WaitGroup
	start := 0
	for k := 0; k < workers; k++ {
		size := chunk
		if k < rem {
			size++
		}
		if size == 0 {
			continue
		}
		s, e := start, start+size
		start = e
		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			body(s, e)
		}(s, e)
	}
	wg.Wait()
}
