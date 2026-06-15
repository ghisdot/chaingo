# Design — Arbre de Merkle creux pour la racine d'état (#9)

> Pré-audit. Remplace à terme le hachage O(n) du JSON complet de l'état.

## Problème

`state.rootLocked()` sérialise TOUT l'état (comptes, validateurs, tokens,
contrats) en JSON canonique et le hashe — **O(n) à chaque bloc**, où n = taille
de l'état. À l'échelle d'un mainnet (millions de comptes), c'est un goulot. Et
ce hash « blob » n'offre **aucune preuve d'inclusion** pour des clients légers.

## Solution

Un **arbre de Merkle creux** (`internal/smt`) :

- Clé logique (adresse) → chemin 256 bits = SHA3-256(clé). Valeur → feuille =
  SHA3-256(valeur).
- Sous-arbres vides repliés sur des hash par défaut précalculés (« creux »).
- **Mise à jour O(profondeur)** : seul le chemin feuille→racine est recalculé.
  **Racine O(1)** (nœud racine caché). Mesuré : ~250 µs/maj, *stable* de 100 à
  100 000 feuilles (vs O(n) aujourd'hui).
- **Preuves d'inclusion ET d'exclusion** vérifiables sans l'arbre — fondation
  des clients légers.
- Tout en SHA3-256 (invariant du projet). Racine **indépendante de l'ordre**
  des insertions (déterminisme inter-nœuds — testé).

## État : fondation livrée, câblage à venir

**Livré** : le package `internal/smt` autonome, testé (déterminisme, ordre,
update/delete, preuves inclusion/exclusion) + benchmark. Aucun impact consensus.

**À activer (changement CASSANT)** : remplacer `state.rootLocked()` par la racine
SMT. Cela change la **racine d'état** présente dans l'en-tête de bloc et vérifiée
par tous les nœuds → change aussi l'empreinte de genèse → mise à niveau
coordonnée (comme le passage P2P binaire / le format de stockage). À faire dans
une étape dédiée, avec :
1. Maintien d'un `smt.Tree` à côté de l'état, mis à jour à chaque mutation de
   compte/validateur/token/contrat.
2. `rootLocked()` renvoie `tree.Root()`.
3. Recalcul de l'empreinte de genèse de référence.
4. Tests : racine identique entre deux nœuds rejouant les mêmes blocs ; racine
   stable après redémarrage.

## Limite assumée (optimisation ultérieure)

Le store garde chaque nœud du chemin de chaque clé → mémoire O(clés × 256).
Convient au testnet. Pour des millions de comptes, ajouter la **compression de
chemin** (ne stocker que les nœuds de branchement, façon Jellyfish Merkle Tree)
— sans changer la sémantique de la racine.
