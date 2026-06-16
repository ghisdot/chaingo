// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Transformée Numérique Théorique (NTT) sur le corps de Goldilocks.
//
// La NTT est l'analogue de la FFT mais dans un corps fini : elle évalue un
// polynôme sur les puissances d'une racine 2^logN-ième de l'unité, en
// O(N log N) au lieu de O(N^2). Utilisée pour évaluer/interpoler les
// polynômes de trace et de contrainte du STARK.
//
// Implémentation Cooley-Tukey radix-2, décimation en temps (DIT), en place,
// avec permutation bit-reverse. Entièrement déterministe.
package stark

import "math/bits"

// isPow2 indique si n est une puissance de 2 strictement positive.
func isPow2(n int) bool {
	return n > 0 && (n&(n-1)) == 0
}

// log2 renvoie l'exposant logN tel que 2^logN == n. n DOIT être une puissance
// de 2 (vérifié par l'appelant).
func log2(n int) uint32 {
	return uint32(bits.TrailingZeros(uint(n)))
}

// bitReverseInPlace permute le tableau selon l'inversion des bits des indices,
// préparant l'itération Cooley-Tukey décimation-en-temps.
func bitReverseInPlace(a []Felt) {
	n := len(a)
	logN := log2(n)
	for i := 0; i < n; i++ {
		// bits.Reverse renverse les 64 bits ; on décale pour ne garder que
		// les logN bits significatifs.
		j := int(bits.Reverse(uint(i)) >> (bits.UintSize - uint(logN)))
		if j > i {
			a[i], a[j] = a[j], a[i]
		}
	}
}

// nttCore exécute la NTT/INTT en place. Si inverse vaut false, on utilise la
// racine ω ; sinon son inverse ω^{-1}. La normalisation par 1/N (pour l'INTT)
// est appliquée par INTT, pas ici.
func nttCore(a []Felt, inverse bool) {
	n := len(a)
	if n <= 1 {
		return
	}
	logN := log2(n)

	bitReverseInPlace(a)

	// Racine principale d'ordre n (ou son inverse).
	root := RootOfUnity(logN)
	if inverse {
		root = root.Inv()
	}

	// Étages successifs : tailles de papillon 2, 4, 8, ..., n.
	for s := uint32(1); s <= logN; s++ {
		m := 1 << s    // taille du bloc courant
		half := m >> 1 // moitié (distance entre les deux pattes du papillon)
		// wm = racine principale d'ordre m = root^(n/m).
		wm := root.Exp(uint64(n / m))
		for k := 0; k < n; k += m {
			w := One()
			for j := 0; j < half; j++ {
				// Papillon Cooley-Tukey :
				//   u = a[k+j]
				//   t = w * a[k+j+half]
				//   a[k+j]      = u + t
				//   a[k+j+half] = u - t
				u := a[k+j]
				t := w.Mul(a[k+j+half])
				a[k+j] = u.Add(t)
				a[k+j+half] = u.Sub(t)
				w = w.Mul(wm)
			}
		}
	}
}

// NTT effectue la transformée directe en place sur a.
// len(a) DOIT être une puissance de 2 (panique sinon) et <= 2^32.
// Après l'appel, a[i] contient P(ω^i) où P est le polynôme dont les
// coefficients étaient initialement dans a (ordre croissant des degrés).
func NTT(a []Felt) {
	if !isPow2(len(a)) {
		panic("stark: NTT: la taille doit être une puissance de 2")
	}
	nttCore(a, false)
}

// INTT effectue la transformée inverse en place sur a, normalisation 1/N
// comprise. NTT puis INTT (ou l'inverse) redonne le tableau d'origine.
// len(a) DOIT être une puissance de 2 (panique sinon).
func INTT(a []Felt) {
	if !isPow2(len(a)) {
		panic("stark: INTT: la taille doit être une puissance de 2")
	}
	n := len(a)
	nttCore(a, true)
	// Normalisation : division par N (i.e. multiplication par N^{-1}).
	nInv := FromUint64(uint64(n)).Inv()
	for i := range a {
		a[i] = a[i].Mul(nInv)
	}
}

// Evaluate renvoie les évaluations du polynôme `coeffs` (coefficients en ordre
// croissant des degrés) sur le domaine {ω^0, ω^1, ..., ω^(N-1)} où N est une
// puissance de 2 >= len(coeffs). Le résultat est un NOUVEAU slice de longueur
// N ; `coeffs` n'est pas modifié.
//
// Si len(coeffs) n'est pas une puissance de 2, on complète par des zéros
// jusqu'à la prochaine puissance de 2.
func Evaluate(coeffs []Felt) []Felt {
	n := nextPow2(len(coeffs))
	buf := make([]Felt, n)
	copy(buf, coeffs)
	NTT(buf)
	return buf
}

// Interpolate est l'inverse d'Evaluate : à partir des évaluations sur le
// domaine {ω^0, ..., ω^(N-1)}, renvoie les coefficients du polynôme
// interpolant (ordre croissant des degrés). len(evals) DOIT être une
// puissance de 2. Le résultat est un NOUVEAU slice ; `evals` n'est pas
// modifié.
func Interpolate(evals []Felt) []Felt {
	if !isPow2(len(evals)) {
		panic("stark: Interpolate: la taille doit être une puissance de 2")
	}
	buf := make([]Felt, len(evals))
	copy(buf, evals)
	INTT(buf)
	return buf
}

// MulPoly multiplie deux polynômes (coefficients en ordre croissant des
// degrés) via la NTT et renvoie le polynôme produit. Le résultat a pour
// longueur len(a)+len(b)-1 (degré exact), sans zéros de bourrage superflus
// au-delà. Si a ou b est vide, renvoie un slice vide.
//
// Méthode : on choisit N = prochaine puissance de 2 >= len(a)+len(b)-1, on
// évalue les deux polynômes sur le domaine NTT de taille N, on multiplie
// point-à-point, puis on interpole.
func MulPoly(a, b []Felt) []Felt {
	if len(a) == 0 || len(b) == 0 {
		return []Felt{}
	}
	resLen := len(a) + len(b) - 1
	n := nextPow2(resLen)

	fa := make([]Felt, n)
	fb := make([]Felt, n)
	copy(fa, a)
	copy(fb, b)

	NTT(fa)
	NTT(fb)
	for i := 0; i < n; i++ {
		fa[i] = fa[i].Mul(fb[i])
	}
	INTT(fa)

	// On tronque au degré exact du produit.
	return fa[:resLen]
}

// nextPow2 renvoie la plus petite puissance de 2 >= n. Pour n <= 1 renvoie 1.
func nextPow2(n int) int {
	if n <= 1 {
		return 1
	}
	// bits.Len(n-1) donne le nombre de bits nécessaires ; 1<<ce nombre est la
	// puissance de 2 supérieure ou égale.
	return 1 << bits.Len(uint(n-1))
}
