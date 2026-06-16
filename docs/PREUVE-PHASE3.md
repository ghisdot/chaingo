# Dossier de preuve — Transactions blindées post-quantiques (ChainGO Phase 3)

> **Statut : prototype de R&D, vérifié par tests, HORS-CONSENSUS, NON AUDITÉ.**
> Tout est gaté `PrivacyEnabled` (OFF par défaut) et ne sera jamais activé en
> mainnet sans audit. Ce document rassemble, honnêtement, **ce qui est construit,
> ce qui est prouvé par des tests, et ce qui reste à auditer.** Il est fait pour
> être lu par des auditeurs (et des sceptiques).

## 1. En une phrase

ChainGO dispose d'un **système de transactions blindées fait-maison, entièrement
post-quantique** : un moteur **zk-STARK** (sécurité fondée sur le hachage seul,
sans courbe elliptique ni *trusted setup*), un hachage algébrique **Poseidon**,
et un **circuit de dépense** qui prouve en *zero-knowledge* qu'une dépense est
valide — **sans révéler le montant ni le lien émetteur↔destinataire** — vérifié
par une batterie de tests positifs et adverses.

## 2. Pourquoi « post-quantique »

| Brique | Choix ChainGO | Pourquoi PQ |
|---|---|---|
| Preuve ZK | **zk-STARK** (FRI) | sécurité = hachage (SHA3) uniquement ; pas de courbe, pas de setup de confiance |
| Hachage en circuit | **Poseidon** sur Goldilocks | algébrique, prouvable ; sécurité hachage |
| Chiffrement des notes | **ML-KEM-768** (FIPS 203) | KEM standardisé résistant au quantique |
| Signatures (chaîne) | **ML-DSA-65** (FIPS 204) | déjà tout le reste de ChainGO |

Aucune primitive cassable par un ordinateur quantique. C'est le différenciateur :
**privacy + post-quantique**, ce que ni Zcash (courbes/SNARK) ni Monero ne sont.

## 3. Où se passe le calcul (point clé de performance)

- **Prouver** (générer la preuve) : dans le **wallet de l'émetteur**, **hors-chaîne**,
  **une fois** par transaction. Coût actuel ≈ **95 s** (prototype non optimisé,
  mono-thread, Go pur). Optimisable à l'échelle de la seconde (NTT parallèle,
  inversion par lots, trace réduite).
- **Vérifier** : sur **chaque nœud**, en **millisecondes** (vérification STARK
  logarithmique). **Le réseau ne fait jamais le calcul lourd.**

## 4. La pile livrée (`internal/stark`, `internal/crypto`, `internal/shielded`)

| Composant | Fichier(s) | Commit |
|---|---|---|
| Corps fini Goldilocks + NTT | `field.go`, `ntt.go` | `1a70517` |
| Merkle (SHA3) + transcript Fiat-Shamir | `merkle.go`, `transcript.go` | `1a70517` |
| **FRI** (proximité bas degré) | `fri.go` | `1a70517` |
| STARK DEEP-ALI (jouet Fibonacci) | `stark.go`, `stark_air.go` | `1a70517` |
| Hachage **Poseidon** + Merkle algébrique | `poseidon.go`, `merkle_poseidon.go` | `72a1780` |
| Moteur **AIR multi-colonnes** | `stark_mc.go` | `1c680b3` |
| **AIR Poseidon complet** (cohérent avec le hash natif) | `poseidon_air_full.go` | `1c680b3` |
| **Circuit d'appartenance** Merkle (ZK) | `membership_air.go` | `9165b44` |
| **Circuit de dépense blindée** + masquage ZK | `poseidon_spend*.go` | `44aa0b9` |
| Clés de vue ML-KEM + chiffrement de notes | `internal/crypto/view.go` | `2be5ea4` |
| Modèle de pool blindé (notes, nullifiers) | `internal/shielded/` | `2be5ea4` |

## 5. Ce qui est PROUVÉ par des tests (reproductible)

```bash
go test ./internal/stark/ -count=1 -v     # toute la pile STARK + circuit blindé
go test ./internal/crypto/ -run View      # chiffrement de notes ML-KEM
go test ./internal/shielded/               # modèle de pool
```

> Résultat de référence : **`internal/stark` = 210 sous-tests PASS, 0 FAIL** (~9 min,
> car chaque preuve est régénérée). La lenteur est le *prouveur* non optimisé, pas un défaut.

**Circuit de dépense (`TestSpend*`, 24 tests, tous PASS) :**
- `TestSpend_PreuveHonnete` — une dépense valide produit une preuve qui vérifie.
- `TestSpend_TemoinNonPublie` — le témoin (montant, `nk`, chemin) n'apparaît dans
  aucune valeur publique.
- `TestSpendAdv_BitsMontantNonExtractiblesEnClair`, `…PasDeCelluleBruteDansPreuve`
  — le **masquage ZK** (LDE randomisé) : le montant n'est pas extractible de la preuve.
- Négatifs **tous rejetés** : `NullifierFaux`, `NoteHorsArbre`, `OutCmFalsifie`,
  `FeeAnnonceeFausse`, `RejeuAutreEnonce`, `DesequilibreProuve`,
  `VolNoteAutruiNkFaux` (voler la note d'autrui sans sa clé), `TraceFalsifieeRejeteeParSTARK`.

**Énoncé exact prouvé** (1 entrée / 1 sortie + frais) :
- PUBLIC : `merkleRoot`, `nullifier`, `outCm`, `fee`.
- PRIVÉ : `inValue`, `inRho`, `nk`, chemin Merkle ; `outValue`, `outOwnerTag`, `outRho`.
- CONTRAINTES : `inCm = commit(inValue, PoseidonHash(nk), inRho)` ; `inCm ∈ arbre(merkleRoot)` ;
  `nullifier = Hash2(nk, inCm)` ; `outCm = commit(...)` ; `inValue = outValue + fee`.

## 6. Revues adverses intégrées (forgeries tentées → rejetées)

Chaque étage a une passe « attaquant » (fichiers `*_adverse_test.go`, `*_forgerie_test.go`) :
- **FRI / STARK** : fonction aléatoire (non bas-degré) rejetée ; OOD mensonger mais
  cohérent en z rejeté ; greffe d'une preuve FRI étrangère rejetée ; rejeu sur un
  autre énoncé rejeté ; falsification Merkle (racine/feuille/chemin) rejetée.
- **Poseidon** : MDS prouvée réellement MDS ; constantes non dégénérées ; domaines
  Hash/Hash2 disjoints ; forger un digest arbitraire rejeté.
- **Multi-colonnes** : colonne non liée, OOD partiel/permuté, MDS fausse → rejetés.
- **Dépense** : vol de note sans `nk`, création de valeur, double-dépense (nullifier),
  extraction du montant → rejetés.

## 7. Réserves honnêtes — CE QU'IL FAUT AUDITER

C'est du **crypto maison, non audité**. Points connus à challenger en priorité :

1. **Paramètres Poseidon maison** (matrice MDS de Cauchy + constantes dérivées par
   SHAKE256). Pas un Poseidon standardisé. Résistance collision/préimage **non établie**.
2. **Soundness concrète non bornée** : `blowup=8`, `32 requêtes`, **sans facteur de
   grinding** dans le Fiat-Shamir. Pas de cible « 128 bits » prouvée.
3. **Échantillonnage FRI avec remise** (~26 positions distinctes sur 32) → requêtes
   effectives < `NumQueries`.
4. **Masquage ZK** : LDE randomisé présent et testé (montant non extractible), mais
   le *zero-knowledge formel* (indistinguabilité prouvée) n'est pas démontré.
5. **Périmètre du circuit** : profondeur d'arbre **fixe (8 = 256 feuilles)**, **1
   entrée / 1 sortie + frais** (pas de multi-in/out, pas de profondeur variable).
6. **Anonymat ML-KEM** : la *confidentialité* des notes est garantie ; la
   *non-liaison* (key-privacy de ML-KEM) reste à établir.
7. **Performance** : ~95 s/preuve, non optimisé.

## 8. Appel à audit communautaire

ChainGO **assume** la stratégie « crypto maison + audit par une communauté de
hackers ». Tout est ouvert (MIT) et reproductible. Cibles d'attaque suggérées :
forger une preuve de dépense pour une note inexistante, créer de la valeur, voler
une note sans `nk`, extraire un montant d'une preuve, casser la collision-résistance
de Poseidon, exploiter le grinding Fiat-Shamir. Le code vit dans `internal/stark`.

## 9. Statut & prochaine étape

- ✅ **Pile cryptographique + circuit blindé** : livrés, testés, hors-consensus.
- ⏭ **Câblage on-chain** (tx `shield`/`shielded_transfer`/`unshield`, arbre de
  commitments + ensemble de nullifiers dans la racine d'état, vérification STARK
  en consensus) — **gate `PrivacyEnabled`, OFF par défaut**. Étape finale, à venir.
- ⏭ **Audit communautaire** — **bloquant** avant toute activation mainnet.

> Rien dans ce dossier ne doit être lu comme « vos fonds sont anonymes et sûrs
> aujourd'hui ». C'est un **prototype de recherche fonctionnel et testé**, sur la
> voie d'un anonymat fort post-quantique, **ouvert à l'audit**.
