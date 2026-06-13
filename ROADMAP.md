# Feuille de route ChainGO

Suivi public de l'avancement. Une case cochée = implémenté **et vérifié** sur le devnet.

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

- [ ] Votes de finalité BFT multi-validateurs (2f+1) — [#6](https://github.com/ghisdot/chaingo/issues/6)
- [ ] Slashing : double-signature et inactivité (sur stake + fonds en unbonding) — [#7](https://github.com/ghisdot/chaingo/issues/7)
- [ ] Fork-choice et gestion des réorganisations
- [ ] Arbre de Merkle creux pour la racine d'état (remplace le hash O(n))
- [ ] Codec binaire compact (remplace JSON+base64 sur le réseau) — [#8](https://github.com/ghisdot/chaingo/issues/8)
- [ ] Tests unitaires et d'intégration systématiques ([#1](https://github.com/ghisdot/chaingo/issues/1)), fuzzing des entrées réseau
- [ ] Testnet public multi-validateurs
- [ ] Audit de sécurité externe

## Phase 3 — Anonymat fort

- [ ] Transferts confidentiels par preuves zk-STARK (résistantes au quantique)
- [ ] Adresses furtives
- [ ] Le mode `private` actuel devient un vrai bouclier de confidentialité

## Phase 4 — Smart contracts no-code

- [x] **Templates vesting + escrow** (livrés en avance, juin 2026) : fonds verrouillés
      on-chain, déblocage linéaire à l'horloge des blocs / séquestre acheteur-vendeur
      avec arbitre optionnel — une commande, zéro code
- [x] Déploiement en un appel API / une commande (`chaingo contract …`)
- [ ] Templates multisig et DAO
- [ ] VM WASM déterministe pour les développeurs

## Phase 5 — Écosystème

- [ ] Explorateur de blocs public (sur le site embarqué)
- [ ] SDK JavaScript et Python
- [ ] Version anglaise du site et de la documentation
- [ ] Programme de distribution genèse mainnet (« communauté d'abord » : 50/20/15/10/5)
