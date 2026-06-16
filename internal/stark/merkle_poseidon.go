// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Arbre de Merkle binaire « STARK-friendly » : feuilles ET nœuds internes sont
// des digests [4]Felt, et la compression interne 2->1 est la fonction Hash2 de
// Poseidon (étage 2.1). À la différence du Merkle SHA3 (merkle.go) qui sert à
// FRI, cet arbre-ci est ALGÉBRIQUE : sa fonction de compression s'arithmétise
// directement en AIR, ce qui le rend PROUVABLE EN CIRCUIT. Il est destiné au
// pool blindé (engagement sur des digests Poseidon).
//
// ┌──────────────────────────────────────────────────────────────────────────┐
// │ AVERTISSEMENT — repose sur Poseidon, dont les PARAMÈTRES SONT CHOISIS PAR  │
// │ NOUS (matrice MDS + constantes de ronde dérivées par SHAKE256, voir       │
// │ poseidon.go) et NON AUDITÉS. La résistance aux secondes préimages et aux  │
// │ collisions de cet arbre n'est donc PAS établie cryptographiquement. Les   │
// │ tests valident la NON-RÉGRESSION et la soundness POSITIONNELLE, pas la    │
// │ sécurité. Ne pas utiliser en consensus / production.                      │
// └──────────────────────────────────────────────────────────────────────────┘
//
// Conception :
//   - Une feuille EST un digest [4]Felt (déjà la sortie d'un Hash/Hash2 en
//     amont) ; on ne re-hache pas la feuille avant de la placer au niveau 0.
//     L'ordre des digests fournis est significatif (c'est la position).
//   - Un nœud interne = Hash2(enfant gauche, enfant droit). L'ordre
//     gauche/droite est significatif (Hash2 n'est pas symétrique), ce qui lie
//     chaque ouverture à une position précise.
//   - Le nombre de feuilles est complété à la prochaine puissance de 2 en
//     dupliquant la DERNIÈRE feuille (padding déterministe), de sorte que
//     l'arbre soit parfait. Seules les `numLeaves` premières feuilles sont
//     ouvrables et vérifiables.
//
// NB séparation de domaine : contrairement au Merkle SHA3 qui préfixe feuilles
// et nœuds par des octets de domaine distincts, ici feuilles et nœuds sont dans
// le MÊME espace [4]Felt. La distinction structurelle vient de ce que la
// feuille n'est pas re-comprimée (un nœud = Hash2 de deux digests, une feuille
// = un digest brut). Une vraie séparation de domaine algébrique (étiquette dans
// la capacité) est un point À AUDITER pour un usage adverse hors prototype.
//
// Déterminisme ABSOLU : aucune dépendance à time / math/rand ; la seule source
// d'« aléa » est la dérivation déterministe des paramètres Poseidon.
package stark

// PoseidonMerkleTree est l'arbre algébrique matérialisé renvoyé par
// PoseidonCommit. Il conserve tous les niveaux de digests pour produire des
// ouvertures (PoseidonOpen) en O(log N) sans recalcul. La structure est opaque
// aux étages supérieurs : on n'expose que la racine et Open/VerifyPath.
//
// layers[0] est le niveau des feuilles (complété à une puissance de 2) ;
// layers[k+1] est obtenu en compressant les paires de layers[k] via Hash2. Le
// dernier niveau ne contient qu'un élément : la racine.
type PoseidonMerkleTree struct {
	// layers[0] = feuilles (taille = puissance de 2), ..., dernier niveau =
	// {racine}.
	layers [][][poseidonDigestLen]Felt
	// numLeaves est le nombre de feuilles RÉELLES (avant padding). Les indices
	// d'ouverture valides sont [0, numLeaves).
	numLeaves int
}

// Root renvoie la racine de l'arbre (commodité ; identique à la valeur renvoyée
// par PoseidonCommit).
func (t *PoseidonMerkleTree) Root() [poseidonDigestLen]Felt {
	return t.layers[len(t.layers)-1][0]
}

// NumLeaves renvoie le nombre de feuilles réelles engagées.
func (t *PoseidonMerkleTree) NumLeaves() int {
	return t.numLeaves
}

// PoseidonCommit construit l'arbre de Merkle algébrique sur les digests fournis
// (chaque feuille est un [4]Felt) et renvoie la racine ainsi que l'arbre
// matérialisé (pour produire des ouvertures).
//
// Le nombre de feuilles est complété à la prochaine puissance de 2 par
// duplication de la dernière feuille (padding déterministe). Panique si
// `leaves` est vide : engager le vide n'a pas de sens et masquerait une erreur
// d'appelant.
func PoseidonCommit(leaves [][poseidonDigestLen]Felt) ([poseidonDigestLen]Felt, *PoseidonMerkleTree) {
	if len(leaves) == 0 {
		panic("stark: PoseidonCommit: aucune feuille à engager")
	}

	numLeaves := len(leaves)

	// Niveau 0 : on recopie les feuilles réelles telles quelles (déjà des
	// digests). Copie défensive : on ne conserve pas le slice de l'appelant.
	n := nextPow2(numLeaves)
	level := make([][poseidonDigestLen]Felt, n)
	for i := 0; i < numLeaves; i++ {
		level[i] = leaves[i]
	}
	// Padding : on duplique la DERNIÈRE feuille réelle jusqu'à la puissance de 2.
	// Choix déterministe et indépendant de l'aléa.
	last := level[numLeaves-1]
	for i := numLeaves; i < n; i++ {
		level[i] = last
	}

	layers := [][][poseidonDigestLen]Felt{level}

	// Remontée : on compresse les paires (gauche, droite) via Hash2 jusqu'à
	// n'avoir qu'un seul nœud (racine).
	for len(level) > 1 {
		next := make([][poseidonDigestLen]Felt, len(level)/2)
		for i := 0; i < len(next); i++ {
			next[i] = Hash2(level[2*i], level[2*i+1])
		}
		layers = append(layers, next)
		level = next
	}

	tree := &PoseidonMerkleTree{layers: layers, numLeaves: numLeaves}
	return tree.Root(), tree
}

// PoseidonOpen produit le chemin d'authentification (nœuds frères, du bas vers
// le haut) pour la feuille d'indice `index`. Le chemin contient exactement
// log2(N) digests, où N est la taille (puissance de 2) de l'arbre.
//
// Panique si l'index est hors de [0, numLeaves) : ouvrir une feuille de padding
// ou inexistante est une erreur d'appelant.
func PoseidonOpen(tree *PoseidonMerkleTree, index int) [][poseidonDigestLen]Felt {
	if index < 0 || index >= tree.numLeaves {
		panic("stark: PoseidonOpen: index de feuille hors bornes")
	}

	// Hauteur = nombre de niveaux internes = len(layers) - 1 (la racine n'est pas
	// dans le chemin).
	path := make([][poseidonDigestLen]Felt, 0, len(tree.layers)-1)
	idx := index
	for lvl := 0; lvl < len(tree.layers)-1; lvl++ {
		// Frère : si idx est pair, le frère est à droite (idx+1) ; sinon à gauche
		// (idx-1). XOR 1 bascule le bit de poids faible — c'est ce bit qui encode
		// la direction (gauche/droite) à chaque niveau.
		sibling := idx ^ 1
		path = append(path, tree.layers[lvl][sibling])
		idx >>= 1 // on monte d'un niveau
	}
	return path
}

// PoseidonVerifyPath recalcule la racine à partir de la feuille (un digest), de
// son indice et du chemin d'authentification, et la compare à `root`. Renvoie
// true ssi la reconstruction coïncide EXACTEMENT.
//
// La direction de combinaison (frère à gauche ou à droite) est déduite du bit
// de poids faible de l'indice à chaque niveau, EXACTEMENT comme dans
// PoseidonOpen : c'est ce qui lie le chemin à une position précise et empêche de
// présenter une ouverture valide pour un mauvais indice (soundness
// positionnelle). Hash2 étant sensible à l'ordre, intervertir gauche/droite
// (mauvaise direction) fait diverger la racine.
//
// La longueur du chemin DOIT correspondre à la hauteur impliquée par l'indice ;
// un chemin de longueur incohérente, un mauvais indice, une feuille falsifiée ou
// un frère falsifié font diverger la racine recalculée et renvoient false.
func PoseidonVerifyPath(root [poseidonDigestLen]Felt, index int, leaf [poseidonDigestLen]Felt, path [][poseidonDigestLen]Felt) bool {
	if index < 0 {
		return false
	}
	// L'arbre a 2^len(path) feuilles ; l'indice doit y tenir. Rejette aussi les
	// chemins trop longs pour l'indice (cohérence hauteur/position).
	if index >= (1 << uint(len(path))) {
		return false
	}

	acc := leaf
	idx := index
	for _, sibling := range path {
		if idx&1 == 0 {
			// idx pair : nœud courant à gauche, frère à droite.
			acc = Hash2(acc, sibling)
		} else {
			// idx impair : frère à gauche, nœud courant à droite.
			acc = Hash2(sibling, acc)
		}
		idx >>= 1
	}
	return acc == root
}
