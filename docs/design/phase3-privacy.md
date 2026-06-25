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
| **Moteur STARK maison** (corps Goldilocks, NTT, Merkle, transcript, **FRI**, STARK jouet) | ✅ livré R&D — revue adverse (7 classes de forgerie rejetées) | hash-only PQ |
| **Hash algébrique Poseidon** + Merkle STARK-friendly | ✅ livré + testé | params maison à auditer |
| **Moteur AIR multi-colonnes** + **AIR Poseidon complet** (cohérent avec le hash natif) | ✅ livré + testé | — |
| **Circuit d'appartenance Merkle** (ZK, profondeur 8) | ✅ livré + testé | témoin privé |
| **Circuit de DÉPENSE blindée** (appartenance + nullifier + conservation) + **masquage ZK** | ✅ **livré + testé** (24 tests, montant non extractible) | **vraie preuve ZK** (≠ placeholder) |
| Tx on-chain `shield`/`shielded_transfer`/`unshield` + gate `PrivacyEnabled` | ⬜ étage 5 (final) |
| **Audit communautaire** (hackers) | ⬜ — **bloquant mainnet** |

> Dossier de preuve complet (composants, tests, reproductibilité, réserves, appel
> à audit) : [docs/PREUVE-PHASE3.md](../PREUVE-PHASE3.md).

### Réserves du moteur STARK (à durcir / auditer)

Le moteur (`internal/stark`) est un **prototype maison non audité**. La revue adverse
intégrée a confirmé que les forgeries testées sont rejetées. Le **durcissement** a
depuis résolu plusieurs réserves :
- **Grinding Fiat-Shamir livré** (`FriParams.GrindBits`, défaut 16) : proof-of-work
  anti-broyage avant le tirage des positions, +16 bits de soundness, coût vérifieur
  = 1 hachage. Reste : une cible « 128 bits » formellement prouvée (analyse fine).
- **Échantillonnage sans remise livré** (`ChallengeIndicesDistinct`) : positions de
  requête FRI deux à deux distinctes → soundness par requête exacte.
- **Profondeur de pliage FRI variable livrée** (`FoldStopBits`) ; **inversions par
  lots** (Montgomery) + dédup des dénominateurs + parallélisation → prouveur ~77×
  plus rapide ; **circuit M-entrées / N-sorties** (join-split) livré.

### Avertissements (à ne pas survendre)

- **Deux chemins coexistent.** Le `SpendWitness`/`VerifyTransparent` de
  `internal/shielded` est le **prototype TRANSPARENT** (révèle les montants) — il
  a servi à poser le modèle de pool. Le **vrai circuit zk-STARK** (`internal/stark`,
  `poseidon_spend*.go`) le **remplace** : il cache les montants (masquage ZK testé).
  Le câblage on-chain (étage 5) utilisera le circuit ZK, pas le placeholder.
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
