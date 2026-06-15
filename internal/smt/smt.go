// Package smt : arbre de Merkle creux (Sparse Merkle Tree) pour la racine
// d'état. Remplace à terme le hachage O(n) du JSON complet de l'état (qui
// rehashe TOUT l'état à chaque bloc) par :
//   - des mises à jour en O(profondeur) : seul le chemin de la feuille modifiée
//     jusqu'à la racine est recalculé ;
//   - une racine en O(1) (nœud racine caché) ;
//   - des preuves d'inclusion/exclusion (utiles aux futurs clients légers).
//
// Conception :
//   - Clé logique (ex : adresse de compte) → chemin de 256 bits = SHA3-256(clé).
//   - Valeur → feuille = SHA3-256(valeur). Une valeur vide « supprime » la clé.
//   - Sous-arbres vides « repliés » : on ne stocke QUE les nœuds non vides ;
//     un sous-arbre vide vaut son hash par défaut précalculé defaultHashes[d].
//     D'où « creux » (sparse). Un nœud qui redevient vide est retiré du store.
//   - Tout le hachage passe par crypto.Hash (SHA3-256) — invariant du projet.
//
// Déterminisme : la racine ne dépend QUE de l'ensemble {clé→valeur}, jamais de
// l'ordre des insertions/suppressions.
//
// Limite assumée (optimisation suivante) : on stocke chaque nœud du chemin de
// chaque clé → mémoire O(nb_clés × 256). Convient au testnet (centaines de
// comptes). Pour un mainnet à millions de comptes, ajouter la COMPRESSION DE
// CHEMIN (ne stocker que les nœuds de branchement, façon Jellyfish Merkle Tree)
// — sans changer la sémantique de la racine.
package smt

import (
	"bytes"

	"chaingo/internal/crypto"
)

// treeDepth : longueur du chemin = taille du hash de clé en bits (SHA3-256).
const treeDepth = 256

// hashSize : taille d'un hash SHA3-256 en octets.
const hashSize = 32

// defaultHashes[d] = hash d'un sous-arbre entièrement vide à la profondeur d.
// defaultHashes[treeDepth] = feuille vide ; chaque niveau au-dessus = hash de
// deux enfants vides identiques. Précalculé une fois au chargement du package.
var defaultHashes = func() [][]byte {
	dh := make([][]byte, treeDepth+1)
	dh[treeDepth] = make([]byte, hashSize) // feuille vide = 32 octets à zéro
	for d := treeDepth - 1; d >= 0; d-- {
		dh[d] = crypto.Hash(dh[d+1], dh[d+1])
	}
	return dh
}()

// Tree : arbre de Merkle creux. NON thread-safe — l'appelant (l'état) sérialise
// déjà les accès derrière son propre verrou.
type Tree struct {
	// nodes : hash de nœud NON vide, indexé par son préfixe de chemin (chaîne de
	// bits '0'/'1' de longueur = profondeur). La racine est la clé "" (profondeur
	// 0) ; une feuille est une clé de 256 bits. Les nœuds par défaut ne sont pas
	// stockés (repliés).
	nodes map[string][]byte
}

// New crée un arbre vide.
func New() *Tree { return &Tree{nodes: map[string][]byte{}} }

// pathBits : chemin de 256 bits (chaîne '0'/'1') dérivé de la clé logique.
func pathBits(key []byte) string {
	h := crypto.Hash(key)
	b := make([]byte, treeDepth)
	for i := 0; i < treeDepth; i++ {
		if (h[i/8]>>(7-uint(i%8)))&1 == 1 {
			b[i] = '1'
		} else {
			b[i] = '0'
		}
	}
	return string(b)
}

// nodeHash : hash stocké du nœud au préfixe `prefix`, ou son hash par défaut.
func (t *Tree) nodeHash(prefix string) []byte {
	if h, ok := t.nodes[prefix]; ok {
		return h
	}
	return defaultHashes[len(prefix)]
}

// Update insère/met à jour la valeur d'une clé (feuille = SHA3-256(value)).
// Une valeur vide supprime la clé. Recalcule le chemin feuille→racine en
// O(profondeur).
func (t *Tree) Update(key, value []byte) {
	path := pathBits(key)
	if len(value) == 0 {
		t.setLeaf(path, nil)
		return
	}
	t.setLeaf(path, crypto.Hash(value))
}

// Delete retire une clé (no-op si absente).
func (t *Tree) Delete(key []byte) { t.setLeaf(pathBits(key), nil) }

// setLeaf place (ou retire si leaf == nil) la feuille au chemin `path`, puis
// recalcule chaque ancêtre jusqu'à la racine, en repliant (supprimant du store)
// tout nœud redevenu vide.
func (t *Tree) setLeaf(path string, leaf []byte) {
	store := func(prefix string, h []byte) {
		if h == nil || bytes.Equal(h, defaultHashes[len(prefix)]) {
			delete(t.nodes, prefix)
		} else {
			t.nodes[prefix] = h
		}
	}
	store(path, leaf) // profondeur 256 : la feuille
	for d := treeDepth - 1; d >= 0; d-- {
		prefix := path[:d]
		h := crypto.Hash(t.nodeHash(prefix+"0"), t.nodeHash(prefix+"1"))
		store(prefix, h)
	}
}

// Root renvoie la racine de Merkle (O(1) : nœud racine caché). Une copie est
// renvoyée pour que l'appelant ne puisse pas muter l'état interne.
func (t *Tree) Root() []byte {
	return append([]byte(nil), t.nodeHash("")...)
}

// Len renvoie le nombre de feuilles non vides.
func (t *Tree) Len() int {
	n := 0
	for p := range t.nodes {
		if len(p) == treeDepth {
			n++
		}
	}
	return n
}

// ---- Preuves d'inclusion / exclusion ----

// Proof : preuve de Merkle pour une clé. Siblings[i] est le hash du frère au
// niveau i (i=0 = frère de la feuille, profondeur 256 ; i croît vers la racine).
// Leaf est le hash de feuille prouvé (nil = preuve d'EXCLUSION : la clé est
// absente). Vérifiable contre une racine sans accès à l'arbre.
type Proof struct {
	Key      []byte
	Leaf     []byte   // SHA3-256(value), ou nil si la clé est absente
	Siblings [][]byte // longueur = treeDepth, du bas (feuille) vers le haut
}

// Prove construit la preuve pour `key` (inclusion si présente, exclusion sinon).
func (t *Tree) Prove(key []byte) *Proof {
	path := pathBits(key)
	p := &Proof{Key: append([]byte(nil), key...), Siblings: make([][]byte, treeDepth)}
	if h, ok := t.nodes[path]; ok {
		p.Leaf = append([]byte(nil), h...)
	}
	// Frères du bas (profondeur 256) vers le haut (profondeur 1).
	for d := treeDepth - 1; d >= 0; d-- {
		prefix := path[:d]
		var sibling string
		if path[d] == '0' {
			sibling = prefix + "1"
		} else {
			sibling = prefix + "0"
		}
		p.Siblings[treeDepth-1-d] = append([]byte(nil), t.nodeHash(sibling)...)
	}
	return p
}

// Verify recompute la racine à partir de la preuve et la compare à `root`.
// Fonctionne pour l'inclusion (Leaf != nil) comme l'exclusion (Leaf == nil :
// la feuille vaut le hash par défaut).
func (p *Proof) Verify(root []byte) bool {
	if len(p.Siblings) != treeDepth {
		return false
	}
	path := pathBits(p.Key)
	cur := p.Leaf
	if cur == nil {
		cur = defaultHashes[treeDepth]
	}
	// Remonte de la feuille (profondeur 256) à la racine.
	for d := treeDepth - 1; d >= 0; d-- {
		sib := p.Siblings[treeDepth-1-d]
		if path[d] == '0' {
			cur = crypto.Hash(cur, sib)
		} else {
			cur = crypto.Hash(sib, cur)
		}
	}
	return bytes.Equal(cur, root)
}
