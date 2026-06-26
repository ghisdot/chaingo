# Dossier de preuve — Transactions blindées post-quantiques (ChainGO Phase 3)

> **Statut : activé sur testnet/devnet, vérifié par une large batterie de tests
> (positifs + adverses), revue de sécurité en cours.** La gate `PrivacyEnabled` est
> ON sur les réseaux de test et reste OFF sur le mainnet jusqu'à la fin de la revue
> de sécurité. Ce document rassemble **ce qui est construit, ce qui est prouvé par
> des tests, et ce qui est sous revue.** Il est fait pour être lu par des auditeurs
> (et des sceptiques).

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
  **une fois** par transaction. Coût actuel ≈ **1,8 s** (≈**77×** plus rapide qu'à
  l'origine : NTT, inversion par lots façon Montgomery, déduplication des dénominateurs
  de bord, parallélisation déterministe — Go pur).
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
| **Circuit de dépense blindée** 1-in/1-out + masquage ZK | `poseidon_spend*.go` | `44aa0b9` |
| **Circuit M-entrées / N-sorties** (join-split) | `poseidon_spendn.go` | (Phase 3 durcissement) |
| Clés de vue ML-KEM + chiffrement de notes | `internal/crypto/view.go` | `2be5ea4` |
| Modèle de pool blindé (notes, nullifiers) | `internal/shielded/` | `2be5ea4` |

**Durcissement du STARK** (axe « zk-STARK hardening ») :
- **Grinding Fiat-Shamir** (`FriParams.GrindBits`) : preuve-de-travail anti-broyage
  avant le tirage des positions, +16 bits de soundness, coût vérifieur = 1 hachage.
- **Échantillonnage des requêtes SANS REMISE** (`ChallengeIndicesDistinct`).
- **Profondeur de pliage FRI variable** (`FoldStopBits`).
- **Prouveur ~77× plus rapide** (141 s → ~1,8 s) : inversion par lots (Montgomery),
  `x^n` en suite géométrique, dédup des dénominateurs de bord, parallélisation
  déterministe (`parallel.go`). Sorties bit-à-bit identiques (déterminisme préservé).

## 5. Ce qui est PROUVÉ par des tests (reproductible)

```bash
go test ./internal/stark/ -count=1 -v     # toute la pile STARK + circuit blindé
go test ./internal/crypto/ -run View      # chiffrement de notes ML-KEM
go test ./internal/shielded/               # modèle de pool
```

> Résultat de référence : **`internal/stark` = tous les sous-tests PASS, 0 FAIL**.
> Depuis le durcissement, le prouveur tourne en **~1,8 s/preuve** (contre ~95–140 s
> auparavant) : l'inversion par lots, la dédup des dénominateurs et la parallélisation
> ont apporté un facteur **~77×**, sans changer le résultat (déterminisme vérifié).

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

**Circuit M-entrées / N-sorties (`TestSpendN_*`, join-split)** — généralise le
circuit au-delà du 1-in/1-out (fusion ET fractionnement de notes) :
- PUBLIC : `merkleRoot`, `nullifier_i` (i<M), `outCm_j` (j<N), `fee`.
- CONTRAINTES : pour chaque entrée i, `inCm_i = commit(...) ∈ arbre(merkleRoot)` et
  `nf_i = Hash2(nk_i, inCm_i)` ; pour chaque sortie j, `outCm_j = commit(...)` ; et
  la **conservation par accumulateur signé** `Σ inValue_i = Σ outValue_j + fee`.
- Mécanique : enchaînement linéaire des blocs Poseidon avec un mode de glue
  « charge-témoin » (`mPackNk`) pour démarrer chaque entrée sur une clé fraîche, et
  complétion en puissance de 2 par blocs identité.
- Tests PASS : preuves honnêtes (1,1)(1,2)(2,1)(2,2) ; **négatifs rejetés** :
  non-conservation, nullifier falsifié, `outCm` falsifié ; déterminisme.

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
- **M-entrées / N-sorties** : non-conservation (`Σ in ≠ Σ out + fee`), nullifier
  falsifié, `outCm` falsifié → rejetés.

## 7. Points sous revue de sécurité

Crypto **faite-maison** : voici les points actuellement **sous revue de sécurité**,
à challenger en priorité :

1. **Paramètres Poseidon maison** (matrice MDS de Cauchy + constantes dérivées par
   SHAKE256). Pas un Poseidon standardisé. Résistance collision/préimage **non établie**.
2. **Soundness concrète** : `blowup=8`, `32 requêtes` + **grinding 16 bits** (livré).
   La borne s'en trouve renforcée, mais une cible « 128 bits » formellement prouvée
   reste à établir (analyse fine de la soundness FRI + paramétrage cible).
3. ~~Échantillonnage avec remise~~ → **résolu** : échantillonnage **sans remise**
   (positions distinctes) livré.
4. **Masquage ZK** : LDE randomisé présent et testé (montant non extractible), mais
   le *zero-knowledge formel* (indistinguabilité prouvée) n'est pas démontré.
5. **Périmètre du circuit** : profondeur d'arbre **fixe (spendDepth=4 = 16 feuilles
   dans le circuit blindé)** reste fixe. En revanche **multi-entrées / multi-sorties
   livré** (join-split) et **profondeur de pliage FRI variable** livrée.
6. **Anonymat ML-KEM** : la *confidentialité* des notes est garantie ; la
   *non-liaison* (key-privacy de ML-KEM) reste à établir.
7. ~~Performance ~95 s~~ → **résolu** : **~1,8 s/preuve** (≈77× plus rapide).

## 8. Appel à audit communautaire

ChainGO **assume** la stratégie « crypto maison + audit par une communauté de
hackers ». Tout est ouvert (Apache 2.0) et reproductible. Cibles d'attaque suggérées :
forger une preuve de dépense pour une note inexistante, créer de la valeur, voler
une note sans `nk`, extraire un montant d'une preuve, casser la collision-résistance
de Poseidon, exploiter le grinding Fiat-Shamir. Le code vit dans `internal/stark`.

## 9. Statut & prochaine étape

- ✅ **Pile cryptographique + circuit blindé** : livrés, testés.
- ✅ **Durcissement zk-STARK** : grinding Fiat-Shamir, échantillonnage sans remise,
  profondeur FRI variable, circuit **M-entrées / N-sorties**, prouveur ~77× plus
  rapide (~1,8 s). Livrés, testés.
- ✅ **Câblage on-chain** : tx `shield`/`shielded_transfer`/`unshield` câblées en
  consensus (arbre de commitments + ensemble de nullifiers dans la racine d'état,
  vérification STARK), **activées sur testnet/devnet** (gate `PrivacyEnabled` ON).
- ⏭ **Intégration `state`/wallet du format M-in/N-out** (le 1-in/1-out est actif).
- ⏭ **Revue de sécurité** — en cours ; à finaliser avant l'activation mainnet.

> C'est un **système fonctionnel et testé**, activé sur les réseaux de test, sur la
> voie d'un anonymat fort post-quantique — la revue de sécurité est en cours avant
> l'ouverture mainnet.
