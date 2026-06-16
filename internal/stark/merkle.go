// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Arbre de Merkle binaire sur des feuilles []Felt, brique d'engagement pour
// FRI. Le hachage est SHA3-256 (golang.org/x/crypto/sha3), seule dépendance
// hors bibliothèque standard autorisée. Tout est déterministe : aucune
// utilisation de time, math/rand, ni d'aléa hors transcript Fiat-Shamir.
//
// Conception :
//   - Une feuille est un vecteur de Felt (par ex. les valeurs d'un point
//     d'évaluation, ou un coset entier replié). On la sérialise en octets
//     big-endian (Felt.Bytes) puis on la hache avec une étiquette de domaine
//     « feuille ».
//   - Un nœud interne hache la concaténation de ses deux enfants avec une
//     étiquette de domaine « nœud », distincte de celle des feuilles.
//   - DOMAINE-SÉPARATION : préfixer feuille et nœud par des octets de domaine
//     distincts empêche un attaquant de présenter le hash d'un nœud interne
//     comme s'il s'agissait d'une feuille (attaque par confusion de type, qui
//     casserait la résistance aux secondes préimages de l'arbre).
//   - Le nombre de feuilles est complété à la prochaine puissance de 2 en
//     dupliquant la DERNIÈRE feuille (padding déterministe), de sorte que
//     l'arbre soit parfait. Ce choix est purement interne : seules les
//     `numLeaves` premières feuilles sont ouvrables et vérifiables.
package stark

import (
	"golang.org/x/crypto/sha3"
)

// Étiquettes de domaine-séparation : un seul octet de préfixe avant les
// données hachées. Les deux valeurs DOIVENT différer.
const (
	domainLeaf byte = 0x00 // préfixe des hash de feuille
	domainNode byte = 0x01 // préfixe des hash de nœud interne
)

// MerkleTree est l'arbre matérialisé renvoyé par Commit. Il conserve tous les
// niveaux de hash pour pouvoir produire des ouvertures (Open) en O(log N) sans
// recalcul. La structure est opaque aux étages supérieurs : on n'expose que la
// racine et les opérations Open/VerifyPath.
//
// layers[0] est le niveau des feuilles (déjà hachées et complétées à une
// puissance de 2) ; layers[k+1] est obtenu en hachant les paires de layers[k].
// Le dernier niveau ne contient qu'un élément : la racine.
type MerkleTree struct {
	// layers[0] = hash de feuilles (taille = puissance de 2), ... , dernier
	// niveau = {racine}.
	layers [][][32]byte
	// numLeaves est le nombre de feuilles RÉELLES (avant padding). Les indices
	// d'ouverture valides sont [0, numLeaves).
	numLeaves int
}

// Root renvoie la racine de l'arbre (commodité ; identique à la valeur
// renvoyée par Commit).
func (t *MerkleTree) Root() [32]byte {
	return t.layers[len(t.layers)-1][0]
}

// NumLeaves renvoie le nombre de feuilles réelles engagées.
func (t *MerkleTree) NumLeaves() int {
	return t.numLeaves
}

// hashLeaf calcule le hash d'une feuille (vecteur de Felt) avec sa séparation
// de domaine. On sérialise chaque Felt en 8 octets big-endian.
//
// Remarque déterminisme : l'ordre des Felt dans la feuille est significatif et
// préservé ; deux feuilles de contenus différents produisent (sauf collision
// SHA3) des hash différents.
func hashLeaf(leaf []Felt) [32]byte {
	h := sha3.New256()
	h.Write([]byte{domainLeaf})
	for _, f := range leaf {
		h.Write(f.Bytes()) // 8 octets big-endian, longueur fixe
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// hashNode calcule le hash d'un nœud interne à partir de ses deux enfants
// (gauche puis droite), avec séparation de domaine. L'ordre gauche/droite est
// significatif et reproduit à la vérification.
func hashNode(left, right [32]byte) [32]byte {
	h := sha3.New256()
	h.Write([]byte{domainNode})
	h.Write(left[:])
	h.Write(right[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Commit construit l'arbre de Merkle sur les feuilles fournies et renvoie la
// racine ainsi que l'arbre matérialisé (pour produire des ouvertures).
//
// Le nombre de feuilles est complété à la prochaine puissance de 2 par
// duplication de la dernière feuille hachée (padding déterministe). Panique si
// `leaves` est vide : engager le vide n'a pas de sens et masquerait une erreur
// d'appelant.
func Commit(leaves [][]Felt) ([32]byte, *MerkleTree) {
	if len(leaves) == 0 {
		panic("stark: Commit: aucune feuille à engager")
	}

	numLeaves := len(leaves)

	// Niveau 0 : hash de chaque feuille réelle.
	n := nextPow2(numLeaves)
	level := make([][32]byte, n)
	for i := 0; i < numLeaves; i++ {
		level[i] = hashLeaf(leaves[i])
	}
	// Padding : on duplique le hash de la dernière feuille réelle jusqu'à la
	// puissance de 2. Choix déterministe et indépendant de l'aléa.
	last := level[numLeaves-1]
	for i := numLeaves; i < n; i++ {
		level[i] = last
	}

	layers := [][][32]byte{level}

	// Remontée : on hache les paires jusqu'à n'avoir qu'un seul nœud (racine).
	for len(level) > 1 {
		next := make([][32]byte, len(level)/2)
		for i := 0; i < len(next); i++ {
			next[i] = hashNode(level[2*i], level[2*i+1])
		}
		layers = append(layers, next)
		level = next
	}

	tree := &MerkleTree{layers: layers, numLeaves: numLeaves}
	return tree.Root(), tree
}

// Open produit le chemin d'authentification (nœuds frères, du bas vers le haut)
// pour la feuille d'indice `index`. Le chemin contient exactement log2(N)
// hash, où N est la taille (puissance de 2) de l'arbre.
//
// Panique si l'index est hors de [0, numLeaves) : ouvrir une feuille de padding
// ou inexistante est une erreur d'appelant.
func Open(tree *MerkleTree, index int) [][32]byte {
	if index < 0 || index >= tree.numLeaves {
		panic("stark: Open: index de feuille hors bornes")
	}

	// Hauteur = nombre de niveaux internes = len(layers) - 1 (on n'ajoute pas la
	// racine au chemin).
	path := make([][32]byte, 0, len(tree.layers)-1)
	idx := index
	for lvl := 0; lvl < len(tree.layers)-1; lvl++ {
		// Frère : si idx est pair, le frère est à droite (idx+1) ; sinon à
		// gauche (idx-1). XOR 1 bascule le bit de poids faible.
		sibling := idx ^ 1
		path = append(path, tree.layers[lvl][sibling])
		idx >>= 1 // on monte d'un niveau
	}
	return path
}

// VerifyPath recalcule la racine à partir de la feuille, de son indice et du
// chemin d'authentification, et la compare à `root`. Renvoie true ssi la
// reconstruction coïncide exactement.
//
// La direction de combinaison (frère à gauche ou à droite) est déduite du bit
// de poids faible de l'indice à chaque niveau, EXACTEMENT comme dans Open :
// c'est ce qui lie le chemin à une position précise et empêche de présenter une
// ouverture valide pour un mauvais indice (soundness positionnelle).
//
// La longueur du chemin DOIT correspondre à la hauteur impliquée par l'indice ;
// un chemin de longueur incohérente, un mauvais indice ou un frère falsifié
// font diverger la racine recalculée et renvoient false.
func VerifyPath(root [32]byte, index int, leaf []Felt, path [][32]byte) bool {
	if index < 0 {
		return false
	}
	// L'arbre a 2^len(path) feuilles ; l'indice doit y tenir.
	if index >= (1 << uint(len(path))) {
		return false
	}

	acc := hashLeaf(leaf)
	idx := index
	for _, sibling := range path {
		if idx&1 == 0 {
			// idx pair : nœud courant à gauche, frère à droite.
			acc = hashNode(acc, sibling)
		} else {
			// idx impair : frère à gauche, nœud courant à droite.
			acc = hashNode(sibling, acc)
		}
		idx >>= 1
	}
	return acc == root
}
