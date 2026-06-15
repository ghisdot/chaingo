# Feuille de route ChainGO

Suivi public de l'avancement. `[x]` = implémenté **et vérifié** ; `[~]` = première tranche livrée, suite en cours ; `[ ]` = à faire.

## Phase 1 — Fondations ✅ (juin 2026)

- [x] Cryptographie post-quantique partout : ML-DSA-65 (FIPS 204) + SHA3-256
- [x] Machine d'état : comptes, nonces, supply mint/burn
- [x] Consensus PoS « Aurora » : proposeur pondéré par le stake, déterministe
- [x] Rounds de secours (liveness avec validateurs hors-ligne — testé à 29 % de stake mort)
- [x] Frais dynamiques EIP-1559 : base fee brûlé + marché des tips
- [x] Émission ~3 %/an sur le stake, calculée par bloc
- [x] Staking : minimum 10 000 CGO, unbonding 21 j (5 min devnet), libération automatique
- [x] Tokens no-code : création/mint par transaction signée
- [x] Wallets : keystore scrypt + AES-256-GCM
- [x] P2P : gossip TCP, sync par lots, genèse partagée par URL
- [x] API REST complète (14 endpoints) + CORS
- [x] Persistance bbolt, reprise après redémarrage
- [x] Bench : 31 078 TPS bout-en-bout (objectif 1 500 dépassé ×20)
- [x] Site vitrine embarqué avec stats live (servi par le nœud)
- [x] **Délégation de stake** : déléguer dès 1 CGO à un validateur, récompenses au pro-rata (commission 10 %), unbonding au retrait

## Phase 2 — Sécurité de production

- [~] Votes de finalité BFT multi-validateurs (2f+1) — [#6](https://github.com/ghisdot/chaingo/issues/6)
      Livré : précommits signés ML-DSA-65, quorum strict > 2/3, gossip P2P,
      `chaingo keygen`. **Finalité persistante & vérifiable** : chaque bloc porte le
      *commit* (≥ 2/3) du bloc parent (`LastCommit` + `LastCommitRoot` au header) →
      `finalized_height` dérivé de la chaîne, survit au redémarrage (vérifié) et
      n'est plus recalculé localement. **Invariant anti-auto-équivocation** enforced (un
      nœud ne signe jamais 2 précommits à la même hauteur). Reste : verrouillage type
      Tendermint complet (prevote + locking), set de validateurs figé par hauteur, fork-choice
      — design : [docs/design/phase2-bft-safety.md](docs/design/phase2-bft-safety.md).
- [x] Slashing — [#7](https://github.com/ghisdot/chaingo/issues/7) : **double-signature**
      (preuve d'équivocation dans le bloc, slash 5 % stake+délégations, idempotent) **et
      inactivité (downtime)** : round inscrit dans l'en-tête (déterministe), comptage des
      slots de proposeur manqués, jail au seuil + slash 0,1 %, exclusion du pouvoir/tirage
      tant que jailé, sortie via `chaingo unjail` après le délai.
- [~] **Verrouillage BFT (locking POL)** — machinerie complète : `Vote.Round` signé, détection
      de polka par round, règle de lock/unlock à l'émission du précommit, équivocation consciente
      du round (un changement légitime cross-round n'est plus slashable). Quorums mesurés contre
      le **set de validateurs figé par hauteur**. Design : [phase2-locking-pol.md](docs/design/phase2-locking-pol.md).
      Reste l'activation par le fork-choice (ci-dessous).
- [~] **Fork-choice et réorganisations** : checkpoint d'état par hauteur + bascule du SOMMET
      vers le bloc couvert par la polka de plus haut round (reorg sûr avec restauration en cas
      d'échec). Valide par un test de simulation de partition (convergence sans double-finalité).
      Active la machinerie de verrou. Reste : reorg multi-blocs (fork enterré) — durcissement.
- [ ] Arbre de Merkle creux pour la racine d'état (remplace le hash O(n))
- [x] **Codec binaire compact** — [#8](https://github.com/ghisdot/chaingo/issues/8)
      Terminé (tranches 1 à 5). Primitives `internal/codec/` (varint, length-prefixed,
      protections taille max et octets parasites). `Transaction`/`Block`/`Vote`/`DoubleSignEvidence`
      en binaire. **Protocole P2P binaire** : frame `[type][uvarint len][payload]`, anti-DoS
      16 MB/frame, re-gossip de la frame brute. **Stockage bbolt binaire** avec migration
      paresseuse rétrocompatible (les anciennes bases JSON restent lisibles). Gains : **−27 %**
      taille sur tx / **−26 %** sur bloc · **~23×** plus rapide sur tx / **~6,8×** sur bloc.
      `SigningBytes` reste JSON canonique → toutes les signatures restent valides après
      round-trip binaire. Doc : [docs/design/binary-codec.md](docs/design/binary-codec.md).
- [~] Tests unitaires et d'intégration systématiques ([#1](https://github.com/ghisdot/chaingo/issues/1)) :
      unitaires (consensus, state, genesis) + **intégration multi-validateurs en mémoire**
      (4 nœuds convergent + finalisent, synchro d'un nœud tardif) + **fuzzing des décodeurs**
      (tx/block/vote + frames P2P) qui a révélé et corrigé une faille DoS (allocation non
      bornée sur compteur de slice). À étendre : scénarios de fautes (proposeur hors-ligne,
      équivocation) bout-en-bout.
- [x] Mode `--testnet` (chain_id `chaingo-testnet-1`, faucet ouvert, unbonding 24 h) + faucet rate-limité
- [x] **Gouvernance des mises à jour réseau** : version de protocole au handshake P2P, **kick** des
      nœuds trop vieux + **alerte** si le nœud local est en retard — voir [network-upgrades.md](docs/design/network-upgrades.md)
- [x] Testnet public **en ligne 24/24** (chaingo.org) — la chaîne finalise en continu
- [ ] **Validateurs INDÉPENDANTS** (≥ 4 entités distinctes) — aujourd'hui sur les machines du mainteneur ;
      c'est le vrai jalon de décentralisation avant mainnet ([#12](https://github.com/ghisdot/chaingo/issues/12))
- [ ] Audit de sécurité externe (communautaire/gratuit envisagé)

## Phase 3 — Anonymat fort

- [ ] Transferts confidentiels par preuves zk-STARK (résistantes au quantique)
- [ ] Adresses furtives
- [ ] Le mode `private` actuel devient un vrai bouclier de confidentialité

## Phase 4 — Smart contracts no-code

- [x] **Templates vesting + escrow** (livrés en avance, juin 2026) : fonds verrouillés
      on-chain, déblocage linéaire à l'horloge des blocs / séquestre acheteur-vendeur
      avec arbitre optionnel — une commande, zéro code
- [x] Déploiement en un appel API / une commande (`chaingo contract …`)
- [x] **Template multisig M-of-N** : coffre à N signataires, M approbations pour dépenser
      (propose/approve), exécution au seuil — pour les coffres trésorerie/communauté
- [x] **Template DAO** (gouvernance on-chain) : trésorerie partagée, membres, propositions
      de paiement votées POUR/CONTRE, exécution au quorum, rejet automatique si le quorum
      devient inatteignable. CLI + studio + tests.
- [~] **Moteur WASM** (contrats arbitraires façon ETH/BNB, en WebAssembly) — **preview
      expérimentale livrée** (`internal/wasmvm`, runtime wazero Go pur) : charge et exécute
      réellement du WASM, mais **HORS-CONSENSUS** (gas wall-clock non déterministe). Le moteur
      consensus-grade (gas déterministe par instrumentation, API hôte d'état, tx deploy/call,
      audit) est le plus gros chantier restant — voir [docs/design/wasm-vm.md](docs/design/wasm-vm.md).

## Phase 5 — Écosystème

- [x] **Wallet web** : génération de clés + signature ML-DSA-65 dans le navigateur (WASM),
      seed chiffrée AES-256-GCM côté client — créer/importer, solde, envoyer
- [x] **Hébergement gratuit** du site + wallet sur GitHub Pages (CI rebuild du WASM)
- [x] Site bilingue FR/EN (README EN + bascule de langue sur le site et le wallet)
- [x] **Banc d'essai web** (`/loadtest/`) — UI pour stresser le testnet, courbes live
      (hauteur, mempool, base fee, brûlé), signature ML-DSA-65 dans le navigateur via WASM
- [x] **Test de distribution mainnet** (1 Md CGO réparti 50/20/15/10/5) vérifié on-chain :
      supply pile, vesting équipe réclamable à mi-parcours et au-delà
- [x] **Explorateur de blocs public** (`/explorer/`) — blocs, tx, comptes, validateurs, tokens en direct
- [x] **Studio no-code** (`/studio/`) — créer un token et déployer vesting/escrow/multisig/DAO depuis
      le navigateur, signature ML-DSA-65 locale, coût de déploiement « gas » affiché
- [x] **Validator Dashboard** (`/validator/`) — état, stake/unstake/unjail, liste publique, alerte « nœud à jour »
- [ ] SDK JavaScript et Python ([#4](https://github.com/ghisdot/chaingo/issues/4))
- [ ] Documentation (docs/) traduite en anglais
- [x] Outillage de genèse (`chaingo genesis template|validate`, vesting on-chain à la genèse, empreinte déterministe)
- [x] Runbook mainnet + checklist de pré-lancement ([docs/MAINNET.md](docs/MAINNET.md))
- [ ] Programme de distribution genèse mainnet exécuté (« communauté d'abord » : 50/20/15/10/5) — au lancement
