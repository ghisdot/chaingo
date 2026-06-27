# Feuille de route ChainGO

**ChainGO** est une blockchain **post-quantique** en Go : toute signature est en
ML-DSA-65 (FIPS 204), tout hachage en SHA3-256 — aucune courbe elliptique nulle
part. Ce document suit publiquement l'avancement, de façon exhaustive.

**Légende** : `[x]` implémenté **et vérifié** · `[~]` première tranche livrée, suite
en cours · `[ ]` planifié.

---

## État actuel

- **Testnet public en ligne 24/7** ([chaingo.org](https://chaingo.org)) — la chaîne
  **finalise en continu** (BFT) et reprend après redémarrage.
- **Performance** : **31 078 TPS** mesurés bout-en-bout (objectif 1 500 dépassé ×20).
- **Confidentialité** (transactions blindées zk-STARK post-quantiques) et
  **smart contracts WASM** : **actifs sur testnet/devnet**, prêts à l'essai.
- **Revue de sécurité interne (self-audit) + durcissement communautaire** sur les
  composants avancés (zk-STARK, VM WASM) avant leur activation sur le **mainnet**.
  Voir le [rapport de revue de sécurité](docs/SECURITY-REVIEW.md).
- **Décentralisation** : prochain jalon = des **validateurs indépendants** (entités
  tierces) avant le lancement mainnet.

---

## Phase 1 — Fondations ✅

- [x] **Cryptographie post-quantique partout** : signatures ML-DSA-65 (FIPS 204),
      hachage SHA3-256 ; adresses = empreinte SHA3 de la clé publique.
- [x] **Machine d'état** : comptes, nonces, supply (mint/burn), exécution atomique
      (snapshot/restore sur bloc reçu).
- [x] **Consensus PoS « Aurora »** : proposeur déterministe pondéré par le stake.
- [x] **Rounds de secours** : liveness garantie même avec des validateurs hors-ligne
      (testé jusqu'à 29 % de stake mort).
- [x] **Frais dynamiques EIP-1559** : base fee brûlé (déflation) + marché des tips.
- [x] **Émission ~3 %/an** sur le stake total, calculée par bloc.
- [x] **Staking** : minimum 10 000 CGO, unbonding 21 j (5 min en devnet, 24 h en
      testnet), libération automatique à l'échéance.
- [x] **Délégation de stake** : déléguer dès 1 CGO à un validateur, récompenses au
      pro-rata (commission 10 %), unbonding au retrait.
- [x] **Tokens no-code** : création et mint par simple transaction signée.
- [x] **Wallets** : keystore chiffré scrypt + AES-256-GCM.
- [x] **P2P** : gossip TCP, synchronisation par lots, genèse partagée par URL.
- [x] **API REST complète** (14 endpoints) + CORS.
- [x] **Persistance bbolt** + reprise après redémarrage.
- [x] **Bench** : 31 078 TPS bout-en-bout.
- [x] **Site vitrine embarqué** avec statistiques live, servi par le nœud.

## Phase 2 — Sécurité de production

- [~] **Finalité BFT multi-validateurs** (2f+1) — [#6](https://github.com/ghisdot/chaingo/issues/6).
      Livré : précommits signés ML-DSA-65, quorum strict > 2/3, gossip P2P, `chaingo
      keygen`. **Finalité persistante & vérifiable** : chaque bloc porte le *commit*
      (≥ 2/3) du parent (`LastCommit` + `LastCommitRoot` au header) → `finalized_height`
      dérivé de la chaîne, survit au redémarrage, jamais recalculé localement.
      **Invariant anti-auto-équivocation** appliqué (un nœud ne signe jamais deux
      précommits à la même hauteur). **Verrouillage POL + « prevote-the-lock »**
      (un nœud verrouillé ne prevote/précommet un bloc concurrent que sur polka de
      round strictement supérieur). **Reorg multi-blocs** (fork enterré) et **tests de
      fautes bout-en-bout** livrés — [design](docs/design/phase2-bft-safety.md).
- [x] **Slashing** — [#7](https://github.com/ghisdot/chaingo/issues/7) :
      **double-signature** (preuve d'équivocation dans le bloc, slash 5 % du
      stake+délégations, idempotent) **et inactivité (downtime)** : round inscrit au
      header (déterministe), comptage des slots de proposeur manqués, jail au seuil +
      slash 0,1 %, exclusion du tirage tant que jailé, sortie via `chaingo unjail`.
- [~] **Verrouillage BFT (locking POL)** : `Vote.Round` signé, détection de polka par
      round, règle de lock/unlock à l'émission du précommit, équivocation consciente du
      round (un changement légitime cross-round n'est pas slashable). Quorums mesurés
      contre le **set de validateurs figé par hauteur**. Reste : activation pleine par le
      fork-choice — [design](docs/design/phase2-locking-pol.md).
- [x] **Fork-choice et réorganisations** : checkpoint d'état par hauteur + bascule
      (reorg sûr avec restauration atomique en cas d'échec), validé par un test de
      partition. **Reorg MULTI-BLOCS** (fork enterré) livré : un nœud rembobine son
      sommet jusqu'au point de fork et abandonne plusieurs blocs sur preuve d'une polka
      de round supérieur — jamais sous la finalité, méta purgée seulement après succès.
- [x] **Arbre de Merkle creux (SMT)** pour la racine d'état (`internal/smt`) —
      [#9](https://github.com/ghisdot/chaingo/issues/9) : racine granulaire avec preuves
      d'inclusion par compte (clients légers). Gaté `SparseMerkleRoot` (fixé à la genèse).
- [x] **Codec binaire compact** — [#8](https://github.com/ghisdot/chaingo/issues/8).
      Primitives `internal/codec/` (varint, length-prefixed, anti-DoS). `Transaction`/
      `Block`/`Vote`/`DoubleSignEvidence` en binaire ; **protocole P2P binaire** (frame
      `[type][uvarint len][payload]`, plafond 16 MB) ; **stockage bbolt binaire** avec
      migration paresseuse rétrocompatible. Gains : **−27 %** taille tx / **−26 %** bloc ;
      **~23×** plus rapide sur tx / **~6,8×** sur bloc. `SigningBytes` reste JSON canonique
      → signatures valides après round-trip — [design](docs/design/binary-codec.md).
- [~] **Tests & fuzzing systématiques** — [#1](https://github.com/ghisdot/chaingo/issues/1) :
      unitaires (consensus, state, genesis) + **intégration multi-validateurs en mémoire**
      (4 nœuds convergent & finalisent, synchro d'un nœud tardif) + **fuzzing des décodeurs**
      (tx/block/vote + frames P2P), qui a révélé et corrigé une faille DoS (allocation non
      bornée). **Scénarios de fautes bout-en-bout** : proposeur hors-ligne, équivocation
      slashée, fallback soutenu, no-quorum (pas de finalité sans 2/3), reorg enterré.
- [x] **Mode `--testnet`** (chain_id `chaingo-testnet-1`, faucet ouvert rate-limité,
      unbonding 24 h).
- [x] **Gouvernance des mises à jour réseau** : version de protocole au handshake P2P,
      **kick** des nœuds trop vieux + **alerte** si le nœud local est en retard —
      [design](docs/design/network-upgrades.md).
- [x] **Testnet public en ligne 24/24** (chaingo.org) : finalité continue.
- [ ] **Validateurs INDÉPENDANTS** (≥ 4 entités distinctes) — jalon de décentralisation
      avant mainnet ([#12](https://github.com/ghisdot/chaingo/issues/12)).
- [ ] **Durcissement communautaire** du cœur consensus avant mainnet (self-audit
      livré, cf. [SECURITY-REVIEW.md](docs/SECURITY-REVIEW.md) ; bug bounty ouvert).

## Phase 3 — Anonymat fort (transactions blindées zk-STARK)

> Confidentialité **post-quantique** via une pile **zk-STARK maison** (sécurité
> hash-only : zéro courbe, zéro trusted setup). **Activée sur testnet/devnet**
> (gate `PrivacyEnabled` ON) ; **revue de sécurité en cours** avant activation
> mainnet. Dossier : [docs/PREUVE-PHASE3.md](docs/PREUVE-PHASE3.md) · conception :
> [docs/design/phase3-privacy.md](docs/design/phase3-privacy.md).

**Pile cryptographique** (`internal/stark`)
- [x] Corps fini **Goldilocks** + **NTT**, Merkle (SHA3), **transcript Fiat-Shamir** (SHAKE256).
- [x] **FRI** (test de proximité bas degré) + moteur **AIR multi-colonnes** (DEEP-ALI).
- [x] Hachage **Poseidon** algébrique + **AIR Poseidon complet** cohérent avec le hash natif.
- [x] **Circuit d'appartenance** Merkle en zero-knowledge.
- [x] Clés de vue **ML-KEM** + chiffrement des notes (destinataire caché, scan par le bénéficiaire).

**Circuit de dépense blindée**
- [x] **Dépense 1-entrée / 1-sortie** (`poseidon_spend*.go`) : prouve en ZK
      appartenance Merkle + nullifier (anti double-dépense) + **conservation de valeur**,
      **montants cachés** (masquage ZK testé) et **destinataire caché** ; batterie
      adverse (vol sans clé, création de valeur, double-dépense, extraction de montant…).
- [x] **Dépense M-entrées / N-sorties** (join-split, `poseidon_spendn.go`) : fusion ET
      fractionnement de notes en une preuve, **conservation `Σ in = Σ out + frais`** par
      accumulateur signé ; mode de glue « charge-témoin » pour enchaîner les entrées ;
      tests honnêtes + adverses (non-conservation, nullifier/outCm falsifiés) tous verts.
- [x] **Codec** des énoncés/preuves blindés (1-in/1-out **et** M-in/N-out), borné anti-DoS.

**Câblage on-chain** (gate `PrivacyEnabled`)
- [x] Transactions `shield` / `shielded_transfer` / `unshield` câblées **en consensus**
      (`state.go`) : arbre de commitments + ensemble de nullifiers dans la racine d'état,
      **vérification STARK en consensus**, CLI `chaingo shielded`, tests d'intégration.
- [x] **Activé sur devnet + testnet** (gate ON) — utilisable dès maintenant.
- [x] **Format M-in/N-out câblé on-chain** (format canonique) : `state.go` vérifie via
      `VerifySpendN`, marque les M nullifiers + insère les N commitments, avec **dédup
      intra-tx des nullifiers** (anti création de valeur) ; wallet `BuildWitnessMulti`,
      preuve via `ProveSpendN` ; test 2-in/1-out bout-en-bout + rejet du doublon d'entrée.

**Durcissement** (livré)
- [x] **Grinding Fiat-Shamir** (`FriParams.GrindBits`, +16 bits de soundness, coût
      vérifieur = 1 hachage).
- [x] **Échantillonnage des requêtes sans remise** (positions distinctes).
- [x] **Profondeur de pliage FRI variable** (`FoldStopBits`).
- [x] **Prouveur ~77× plus rapide** : 141 s → **~1,8 s** (inversion par lots façon
      Montgomery, `x^n` en suite géométrique, déduplication des dénominateurs de bord,
      parallélisation déterministe). Vérifieur ~45 ms.
- [x] **Range-proofs** (`snRangeBits=48`) : chaque valeur de note (entrée/sortie)
      bornée `< 2⁴⁸` par décomposition binaire dans le circuit — **ferme la création
      de valeur par débordement modulaire** du corps de Goldilocks. La couche état
      refuse en plus tout dépôt `shield ≥ 2⁴⁸` (note sinon indépensable).
- [x] **Soundness ≥128 bits (conjecturée)** : **40 requêtes** FRI (terme ≈136 b) +
      grinding 16 b, et **amplification OOD multi-points** (`mcExtraOodPoints=2` :
      3 points hors-domaine indépendants ⇒ erreur OOD ~2⁻¹⁴⁴ sur le corps 64 bits,
      qui n'est plus le terme limitant). Tests négatifs : valeur hors borne et OOD
      supplémentaire falsifié → rejet.

**À venir**
- [ ] **Revue de sécurité** du circuit blindé — bloquante avant activation mainnet
      (Poseidon non standardisé).
- [ ] **Soundness 128 bits *prouvée*** (et non plus seulement conjecturée façon
      Plonky2 / Winterfell) : nécessiterait un **corps d'extension** pour l'aléa de
      pliage FRI (analyse Johnson). Étape formelle documentée.
- [ ] **Zero-knowledge formel** : indistinguabilité prouvée (le masquage est testé,
      la preuve formelle reste à établir).
- [x] **Profondeur d'arbre portée à 12** (4096 notes ; capacité du pool ×256 vs 16).
- [ ] Profondeur d'arbre **variable par-preuve** (aujourd'hui un format unique).

## Phase 4 — Smart contracts no-code

- [x] **Templates vesting + escrow** : fonds verrouillés on-chain, déblocage linéaire à
      l'horloge des blocs / séquestre acheteur-vendeur avec arbitre optionnel — une
      commande, zéro code.
- [x] **Template multisig M-of-N** : coffre à N signataires, M approbations pour dépenser
      (propose/approve), exécution au seuil.
- [x] **Template DAO** (gouvernance on-chain) : trésorerie partagée, membres, propositions
      de paiement votées POUR/CONTRE, exécution au quorum, rejet auto si quorum
      inatteignable. CLI + studio + tests.
- [x] **Déploiement en un appel** API / une commande (`chaingo contract …`).
- [x] **Moteur WASM** (contrats arbitraires en WebAssembly, façon ETH/BNB) — **câblé en
      consensus sur testnet/devnet** (`internal/wasmvm`, runtime wazero Go pur). Tx
      `wasm_deploy` / `wasm_call`, stockage par contrat dans la racine d'état, déploiement
      via studio / CLI / API. **Déterminisme** : gas par instrumentation (fuzzé 5,3 M),
      opcodes restreints validés au déploiement, test multi-validateurs (4 nœuds, même
      racine) — [design](docs/design/wasm-vm.md).
- [x] **Activé sur testnet/devnet** ; sur mainnet, activation après **revue de sécurité**
      (exécution de bytecode arbitraire).
- [ ] **Revue de sécurité du moteur WASM** avant activation mainnet.

## Phase 5 — Écosystème & outils

- [x] **Wallet web** : génération de clés + signature ML-DSA-65 dans le navigateur (WASM),
      seed chiffrée AES-256-GCM côté client — créer/importer, solde, envoyer.
- [x] **Explorateur de blocs public** (`/explorer/`) — blocs, tx, comptes, validateurs,
      tokens en direct.
- [x] **Studio no-code** (`/studio/`) — créer un token et déployer vesting/escrow/multisig/
      DAO depuis le navigateur, signature ML-DSA-65 locale, coût de déploiement affiché.
- [x] **Validator Dashboard** (`/validator/`) — état, stake/unstake/unjail, liste publique,
      alerte « nœud à jour ».
- [x] **Profil validateur on-chain** (`validator_profile`) — moniker, identité et
      métadonnées publiques signées, exposées à l'API / au dashboard
      ([#25](https://github.com/ghisdot/chaingo/issues/25)).
- [x] **Banc d'essai web** (`/loadtest/`) — UI pour stresser le testnet, courbes live
      (hauteur, mempool, base fee, brûlé), signature ML-DSA-65 dans le navigateur.
- [x] **Hébergement gratuit** du site + wallet sur GitHub Pages (CI rebuild du WASM).
- [x] **Site bilingue FR/EN** (README EN + bascule de langue sur le site et le wallet).
- [x] **Outillage de genèse** (`chaingo genesis template|validate`, vesting on-chain à la
      genèse, empreinte déterministe).
- [x] **Test de distribution mainnet** (1 Md CGO réparti 50/20/15/10/5) vérifié on-chain :
      supply exacte, vesting équipe réclamable à mi-parcours et au-delà.
- [x] **Runbook mainnet** + checklist de pré-lancement ([docs/MAINNET.md](docs/MAINNET.md)).
- [~] **SDK JavaScript & Python** ([#4](https://github.com/ghisdot/chaingo/issues/4)) —
      **repos dédiés** (`chaingo-sdk-js` → npm, `chaingo-sdk-py` → PyPI), versionnés
      indépendamment du nœud, consommant l'API REST ; signature ML-DSA-65 identique au
      nœud. Scaffolds prêts ; publication des paquets à venir.
- [ ] **Documentation `docs/` traduite en anglais**.

## Avant le mainnet

Le mainnet ouvre une fois ces jalons franchis :

1. **Validateurs indépendants** (≥ 4 entités distinctes) — décentralisation réelle.
2. **Revue de sécurité** du cœur consensus, du circuit blindé zk-STARK et du moteur WASM
   — les fonctionnalités avancées restent activées **sur testnet** d'ici là.
3. **Distribution genèse « communauté d'abord »** (50/20/15/10/5) exécutée au lancement.

Détails techniques par sujet dans [`docs/`](docs/) et les
[issues GitHub](https://github.com/ghisdot/chaingo/issues).
