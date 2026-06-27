# Design — Phase 3 : Anonymat fort (pool de transactions blindées)

> Statut : **livré et câblé en consensus sur testnet/devnet** (gate
> `PrivacyEnabled` **ON** sur testnet/devnet, **OFF sur mainnet** jusqu'au
> durcissement communautaire). La couche de chiffrement est du crypto standardisé
> (ML-KEM-768, sûr) ; la preuve de validité de dépense est un **vrai circuit
> zk-STARK** M-entrées/N-sorties (montants cachés, range-proofs, soundness ≥128
> bits conjecturée) — **non audité par un tiers**, voir « État » plus bas et le
> [rapport de revue de sécurité](../SECURITY-REVIEW.md).

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

## État : ce qui est livré

| Élément | État | Sûreté |
|---|---|---|
| Clés de vue **ML-KEM-768** (FIPS 203) dérivées du seed (`crypto.DeriveViewKey`) | ✅ livré | **standardisé** (CIRCL) |
| Chiffrement/ouverture de note + **scan** (`SealTo`/`OpenWith`) | ✅ livré + testé | **confidentialité réelle** |
| Note, **commitment** (hash), **nullifier**, sérialisation (`internal/shielded`) | ✅ livré + testé | hash PQ (ROM) |
| Pool + double-spend + conservation de valeur (flux bout-en-bout) | ✅ livré + testé | logique correcte |
| **Moteur STARK maison** (corps Goldilocks, NTT, Merkle, transcript, **FRI**, STARK jouet) | ✅ livré R&D — revue adverse (7 classes de forgerie rejetées) | hash-only PQ |
| **Hash algébrique Poseidon** + Merkle STARK-friendly | ✅ livré + testé | params maison à auditer |
| **Moteur AIR multi-colonnes** + **AIR Poseidon complet** (cohérent avec le hash natif) | ✅ livré + testé | — |
| **Circuit d'appartenance Merkle** (ZK, profondeur 8) | ✅ livré + testé | témoin privé |
| **Circuit de DÉPENSE blindée** M-in/N-out (appartenance + nullifier + conservation + **range-proofs**) + **masquage ZK** | ✅ **livré + testé** (montant non extractible, soundness ≥128 b conjecturée) | **vraie preuve ZK** |
| Tx on-chain `shield`/`shielded_transfer`/`unshield` + gate `PrivacyEnabled` | ✅ **câblé** (state + wallet/CLI, dédup nullifiers), **ON testnet/devnet** |
| **Durcissement communautaire** (bug bounty) | ⬜ — **bloquant mainnet** |

> Dossier de preuve complet (composants, tests, reproductibilité, réserves, appel
> à audit) : [docs/PREUVE-PHASE3.md](../PREUVE-PHASE3.md).

### Réserves du moteur STARK (à durcir / auditer)

Le moteur (`internal/stark`) est un **prototype maison non audité**. La revue adverse
intégrée a confirmé que les forgeries testées sont rejetées. Le **durcissement** a
depuis résolu plusieurs réserves :
- **Grinding Fiat-Shamir livré** (`FriParams.GrindBits`, défaut 16) : proof-of-work
  anti-broyage avant le tirage des positions, +16 bits de soundness, coût vérifieur
  = 1 hachage.
- **Échantillonnage sans remise livré** (`ChallengeIndicesDistinct`) : positions de
  requête FRI deux à deux distinctes → soundness par requête exacte.
- **Profondeur de pliage FRI variable livrée** (`FoldStopBits`) ; **inversions par
  lots** (Montgomery) + dédup des dénominateurs + parallélisation → prouveur ~77×
  plus rapide ; **circuit M-entrées / N-sorties** (join-split) livré.
- **Range-proofs livrés** : valeurs de note bornées `< 2⁴⁸` (décomposition binaire)
  → **création de valeur par débordement modulaire fermée**.
- **Soundness ≥128 bits conjecturée livrée** : 40 requêtes FRI + grinding +
  **amplification OOD multi-points** (3 points hors-domaine indépendants). Reste :
  la borne **prouvée** (non conjecturée), qui exigerait un corps d'extension pour
  l'aléa de pliage FRI.

### Avertissements (à ne pas survendre)

- **Le vrai circuit ZK est le format on-chain.** Le `SpendWitness`/`VerifyTransparent`
  de `internal/shielded` était le prototype transparent (révèle les montants) ayant
  servi à poser le modèle ; il est **remplacé** par le **vrai circuit zk-STARK**
  M-in/N-out (`internal/stark`, `poseidon_spendn.go`), qui cache les montants et
  **est celui câblé en consensus** (state + wallet/CLI).
- **Anonymat ≠ confidentialité.** Le chiffrement ML-KEM garantit que personne ne
  **lit** une note. Que la note soit **non-liable** à une clé de vue *connue*
  dépend de la *key-privacy* (anonymat) de ML-KEM — propriété distincte **à
  vérifier**. On revendique aujourd'hui la **confidentialité**, pas encore
  l'anonymat formel.
- **Crypto maison, non auditée par un tiers.** C'est pourquoi le gate
  `PrivacyEnabled` reste **OFF sur mainnet** jusqu'au durcissement communautaire,
  bien qu'il soit **ON sur testnet/devnet** (utilisable, à éprouver).

## Plan par tranches

| # | Contenu | État |
|---|---|---|
| 1 | Clés de vue ML-KEM + notes/commitment/nullifier + chiffrement/scan + pool transparent (+ tests) | ✅ livré |
| 2 | Arbre de Merkle des commitments (via `internal/smt`) + tx `shield`/`transfer`/`unshield`, **gate `PrivacyEnabled`** | ✅ livré |
| 3 | **Circuit zk-STARK** M-in/N-out (appartenance + propriété + nullifier + conservation + range-proofs) — prouveur/vérifieur déterministe | ✅ livré |
| 4 | **Durcissement communautaire** (bug bounty) + revue key-privacy ML-KEM + vecteurs de test | ⬜ en cours — bloquant mainnet |
| 5 | Activation mainnet par gouvernance (param `PrivacyEnabled`) | ⬜ post-durcissement (ON testnet/devnet) |

## Décision

L'anonymat fort est un **différenciateur majeur** (privacy **post-quantique**),
mais son cœur — le circuit zk-STARK — est du crypto **maison non audité par un
tiers**. On l'a donc construit par étapes, **gaté** (`PrivacyEnabled`) : il est
**ON sur testnet/devnet** (utilisable, à éprouver par la communauté) et **restera
OFF sur mainnet jusqu'au durcissement communautaire**. Un bug de privacy est
silencieux (les utilisateurs se croient anonymes sans l'être) ou pire (vol de
fonds) : « codé soigneusement » ne suffit pas — d'où la phase de durcissement
ouverte (bug bounty) avant toute activation mainnet.
