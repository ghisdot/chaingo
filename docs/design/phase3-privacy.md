# Design — Phase 3 : Anonymat fort (pool de transactions blindées)

> Statut : **prototype de R&D livré** (`internal/shielded` + clés de vue ML-KEM
> dans `internal/crypto`). **HORS-CONSENSUS · NON AUDITÉ · paramètre OFF partout
> · interdit en mainnet jusqu'à audit externe.** La couche de chiffrement est du
> crypto standardisé (sûr) ; la preuve de validité de dépense est un **placeholder
> transparent** (pas encore zero-knowledge) — voir « État » plus bas.

## Objectif et modèle de menace

Aujourd'hui ChainGO est **pseudonyme** : les adresses ne sont pas nominatives,
mais l'historique est **public** (qui paie qui, combien). « Anonymat fort » =
cacher, on-chain :

1. le **destinataire** (impossible de lier un paiement à l'adresse qui reçoit) ;
2. le **montant** (les valeurs ne sont pas lisibles).

Le tout en restant **post-quantique** — c'est la contrainte qui élimine la
quasi-totalité de l'outillage de confidentialité existant.

## Pourquoi on ne peut PAS réutiliser le crypto de privacy classique

| Brique classique | Problème pour ChainGO |
|---|---|
| Adresses furtives (ECDH : `O = S + H(s)·G`) | repose sur l'**homomorphisme des courbes elliptiques** — cassé par un ordinateur quantique, et ML-DSA/ML-KEM **n'ont pas** cette propriété. |
| Commitments de Pedersen, Bulletproofs (montants/range) | courbes elliptiques → **non post-quantique**. |
| zk-SNARK (Groth16, PLONK…) | « trusted setup » + courbes à couplage → **non post-quantique**. |

La **seule** famille de preuves à divulgation nulle plausible en post-quantique
est le **zk-STARK** (sécurité fondée uniquement sur des fonctions de hachage,
transparent, sans setup de confiance).

## Architecture retenue : pool de notes (façon Zcash-Sapling, adapté PQ)

Plutôt que des comptes, le pool blindé manipule des **notes** non-dépensées :

- **Note** = (montant, clé de vue du destinataire, aléa `rho`).
- **Commitment** `cm = SHA3("cm" ‖ montant ‖ H(owner) ‖ rho)` — **cachant** (grâce
  à `rho`) et **liant**. C'est lui qu'on publie, jamais (montant, destinataire).
- **Arbre de commitments** (accumulateur de Merkle) : permet de prouver qu'une
  note existe **sans dire laquelle** (preuve d'appartenance en ZK).
- **Nullifier** `nf = SHA3("nf" ‖ nk ‖ cm)` : révélé à la dépense pour empêcher le
  double-spend. Dérivé d'une **clé de nullifier** `nk` que seul le propriétaire
  détient → non-liable au `cm` sans `nk`.
- **Livraison chiffrée + scan** : la note est chiffrée vers la **clé de vue
  ML-KEM** du destinataire et publiée ; le destinataire **scanne** les notes et
  ouvre les siennes — **aucun index on-chain ne lie la note à son adresse**.
  C'est le mécanisme qui cache le destinataire (et il ne dépend PAS de
  l'homomorphisme manquant).
- **Preuve de dépense (zk-STARK)** : à la dépense, on prouve en zero-knowledge
  que (a) chaque entrée est dans l'arbre, (b) on connaît son ouverture et sa `nk`,
  (c) les nullifiers sont bien formés, (d) **Σentrées = Σsorties + frais** — sans
  révéler montants ni notes. **C'est le cœur recherche+audit.**

## État : ce qui est livré (tranche 1)

| Élément | État | Sûreté |
|---|---|---|
| Clés de vue **ML-KEM-768** (FIPS 203) dérivées du seed (`crypto.DeriveViewKey`) | ✅ livré | **standardisé** (CIRCL) |
| Chiffrement/ouverture de note + **scan** (`SealTo`/`OpenWith`) | ✅ livré + testé | **confidentialité réelle** |
| Note, **commitment** (hash), **nullifier**, sérialisation (`internal/shielded`) | ✅ livré + testé | hash PQ (ROM) |
| Pool + double-spend + conservation de valeur (flux bout-en-bout) | ✅ livré + testé | logique correcte |
| **Moteur STARK maison** (`internal/stark` : corps Goldilocks, NTT, Merkle, transcript Fiat-Shamir, **FRI**, STARK jouet Fibonacci) | ✅ livré R&D — 90 tests dont preuves de soundness, **revue adverse** (7 classes de forgerie rejetées) | hash-only PQ ; **réserves documentées** ci-dessous |
| **Preuve de dépense** (circuit blindé sur le moteur STARK) | 🟡 **placeholder TRANSPARENT** | **PAS encore zero-knowledge** |
| Hash algébrique (Poseidon/Rescue sur Goldilocks) pour Merkle « STARK-friendly » | ⬜ tranche 3a |
| Arbre de Merkle des commitments | ⬜ tranche 4 (réutilisera `internal/smt` ou le Merkle algébrique) |
| Tx on-chain `shield`/`shielded_transfer`/`unshield` + gate `PrivacyEnabled` | ⬜ tranche 4 |
| **Audit communautaire** (hackers) | ⬜ — **bloquant mainnet** |

### Réserves du moteur STARK (à durcir / auditer)

Le moteur (`internal/stark`) est un **prototype maison non audité**. La revue adverse
intégrée a confirmé que les forgeries testées sont rejetées, mais a relevé :
- **Soundness concrète non paramétrée** : pas d'objectif de bits de sécurité explicite,
  et **pas de facteur de grinding** (proof-of-work) dans le Fiat-Shamir. Avec `Blowup=8`
  et `NumQueries=32`, c'est un prototype, pas une garantie 128 bits prouvée.
- **Échantillonnage avec remise** : les positions de requête FRI sont tirées avec remise
  (~26 distinctes sur 32) → le nombre *effectif* de requêtes, donc la soundness réelle,
  est inférieur à `NumQueries`. À corriger (tirage sans remise / plus de requêtes) lors du durcissement.
- Pas de coset, inversions non batchées, borne de degré implicite : sans impact soundness, à optimiser.

### Avertissements (à ne pas survendre)

- **Le `SpendWitness` actuel RÉVÈLE les montants** : `VerifyTransparent` vérifie
  l'accounting en clair. Il existe pour faire **tourner et tester l'architecture**,
  PAS pour fournir de la confidentialité. Tant que le circuit zk-STARK (tranche 3)
  ne l'a pas remplacé, **les montants ne sont pas cachés**.
- **Anonymat ≠ confidentialité.** Le chiffrement ML-KEM garantit que personne ne
  **lit** une note. Que la note soit **non-liable** à une clé de vue *connue*
  dépend de la *key-privacy* (anonymat) de ML-KEM — propriété distincte **à
  vérifier/auditer**. On revendique aujourd'hui la **confidentialité**, pas
  encore l'anonymat formel.
- **Rien n'est câblé en consensus.** Aucune tx blindée n'existe encore ; c'est un
  banc de R&D (`internal/shielded`) avec ses tests.

## Plan par tranches

| # | Contenu | Risque |
|---|---|---|
| 1 | Clés de vue ML-KEM + notes/commitment/nullifier + chiffrement/scan + pool transparent (+ tests) | faible (hors-consensus) |
| 2 | Arbre de Merkle des commitments (via `internal/smt`) + tx `shield`/`transfer`/`unshield`, **gate `PrivacyEnabled` OFF** | moyen (état, toujours gaté) |
| 3 | **Circuit zk-STARK** (appartenance + propriété + nullifier + conservation) — prouveur/vérifieur déterministe | **élevé — recherche** |
| 4 | **Audit externe** + revue key-privacy ML-KEM + vecteurs de test | bloquant |
| 5 | Activation mainnet par gouvernance (param `PrivacyEnabled`) | post-audit |

## Décision

L'anonymat fort est un **différenciateur majeur** (privacy **post-quantique**),
mais son cœur — le circuit zk-STARK — est du crypto **recherche+audit**. On
construit donc l'architecture par étapes, chaque cran **gaté et hors-consensus**,
et **on ne branchera jamais le pool blindé en consensus mainnet avant un audit
externe**. Un bug de privacy est silencieux (les utilisateurs se croient anonymes
sans l'être) ou pire (vol de fonds) : ici, « codé soigneusement » ne suffit pas —
il faut une revue formelle. La tranche 1 livre les **briques sûres et
standardisées** (chiffrement, scan, modèle de notes) sur lesquelles tout le reste
s'appuie.
