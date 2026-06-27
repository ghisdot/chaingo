# ChainGO

[![CI](https://github.com/ghisdot/chaingo/actions/workflows/ci.yml/badge.svg)](https://github.com/ghisdot/chaingo/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)

🇬🇧 [English version](README.en.md) · 🌐 [chaingo.org](https://chaingo.org)

**Blockchain post-quantique** écrite en Go. Toutes les signatures
(transactions, blocs, votes) utilisent **ML-DSA-65** (FIPS 204, niveau de
sécurité NIST 3), le standard de signature résistant aux ordinateurs
quantiques. Hachage SHA3-256.

**Testnet public en ligne.** Mainnet en préparation après audit externe et
finalisation du consensus BFT (Phase 2).

- 🔐 **Sécurité post-quantique** native, partout dans la chaîne.
- ⚡ **~31 000 TPS** mesurés bout-en-bout (vérification PQ parallèle + exécution).
- 🔥 **Économie déflationniste** : frais EIP-1559 brûlés, supply élastique.
- 🪙 **No-code** : tokens, vesting, escrow, multisig, DAO — déployables **depuis le navigateur** (studio), sans écrire de smart contract.
- 🌐 **P2P** pair-à-pair, rejoignable par n'importe qui, avec gouvernance des mises à jour (version de protocole).

---

## Pour qui ?

### 👤 Vous voulez utiliser ChainGO (transferts, wallet, tokens)

Pas besoin d'installer quoi que ce soit. Tout passe par le navigateur :

- **Wallet web post-quantique** : <https://chaingo.org/wallet/>
  Création d'un wallet, faucet testnet, envoi de transactions, gestion des
  tokens et contrats no-code. Les clés sont générées et conservées **dans
  votre navigateur** (jamais envoyées à un serveur).
- **Obtenir des CGO de test** : voir [docs/GET-CGO.md](docs/GET-CGO.md).
- **Studio no-code** : <https://chaingo.org/studio/>
  Créer un token ou déployer un contrat (vesting, escrow, multisig, DAO) en
  quelques clics, signature post-quantique dans le navigateur.
- **Explorateur de blocs** : <https://chaingo.org/explorer/>
  Parcourir les blocs, transactions, comptes, validateurs et tokens en direct.
- **Validator Dashboard** : <https://chaingo.org/validator/>
  Gérer son nœud validateur (stake, délégations, sortie de prison) et voir le set.
- **Référence API** : <https://chaingo.org/api/>

### 🛡️ Vous voulez faire tourner un nœud ou devenir validateur

- [Guide opérateur de nœud](docs/TESTNET-DEPLOY.md) — rejoindre le testnet
  public en 15 minutes, ou bootstrapper sa propre chaîne.
- [Guide validateur & délégateur](docs/VALIDATOR.md) — staking, délégation,
  slashing, rendement.

### 💻 Vous voulez contribuer au code

- [Guide de contribution](CONTRIBUTING.md) — règles du projet, processus,
  invariants à respecter (crypto post-quantique, déterminisme).
- [Feuille de route](ROADMAP.md) — ce qui est livré, ce qui reste.
- [Référence API](docs/API.md) — pour développer des clients ou intégrer.
- **[Rapport de revue de sécurité](docs/SECURITY-REVIEW.md)** — self-audit interne
  + dossier de preuve (consensus, état, anonymat zk-STARK), reproductible.
- Politique de sécurité : [SECURITY.md](SECURITY.md).

---

## Règles de la chaîne

Les règles économiques vivent dans le **document de genèse**, donc chaque
réseau ChainGO choisit les siennes. Valeurs par défaut :

| Règle | Valeur | Détail |
|---|---|---|
| Supply à la genèse | **1 milliard CGO** | 9 décimales (1 CGO = 10⁹ ucgo) |
| Max supply | **aucun plafond dur** | supply élastique : émission ~3 %/an sur le stake, contrebalancée par le burn |
| Distribution genèse mainnet | « communauté d'abord » | 50 % communauté · 20 % trésorerie · 15 % équipe (vesting 4 ans) · 10 % écosystème · 5 % validateurs genèse / liquidité |
| Émission | **~3 %/an sur le stake total** | mintée au proposeur de chaque bloc, pondéré par le stake |
| Frais | **EIP-1559 dynamiques** | base fee ajusté à la congestion et **brûlé** ; tip libre au validateur |
| Création de token | 10 CGO brûlés | anti-spam |
| Smart contracts no-code | vesting, escrow, multisig, DAO | templates natifs paramétrés, 1 CGO brûlé à la création |
| Stake validateur | **minimum 10 000 CGO** | en dessous : transaction rejetée |
| Délégation | dès **1 CGO**, commission 10 % | les petits holders délèguent à un validateur et touchent leur part au pro-rata |
| Unbonding | **21 jours** (mainnet), 24 h (testnet) | s'applique au stake et aux délégations retirées |
| Blocs | 500 ms, max 2000 tx | défini dans la genèse |

## Consensus « Aurora » (PoS + BFT)

- Proposeur tiré au sort **pondéré par le stake** et déterministe, seedé par
  `(hash précédent, hauteur, round)`.
- **Rounds de secours** pour la liveness : si le proposeur élu ne produit pas
  dans l'intervalle de bloc, le round suivant désigne un autre validateur.
- **Finalité BFT persistante** : chaque bloc embarque le `LastCommit`
  (précommits ≥ 2/3 du parent) — la finalité se vérifie depuis la chaîne et
  survit aux redémarrages.
- **Slashing** : 5 % en cas de double-signature, 0,1 % et jail en cas
  d'inactivité prolongée. Brûlé sur stake **et** délégations.

## Démarrage rapide (développement local)

Compile et lance un nœud de développement local en une commande :

```bash
git clone https://github.com/ghisdot/chaingo
cd chaingo
go build -o chaingo ./cmd/chaingo

# 1. Lancer un nœud devnet local (validateur + faucet inclus)
./chaingo node start --dev

# 2. Créer un wallet (clés ML-DSA-65)
./chaingo wallet new alice

# 3. Demander des CGO au faucet, transférer
./chaingo faucet --to alice --amount 500
./chaingo send --from alice --to <cg-adresse> --amount 42.5 --fast

# 4. Créer un token sans code, ou un contrat
./chaingo token create --from alice --symbol MONTOK --name "Mon Token" --supply 1000000
./chaingo contract multisig --from alice --signers a,b,c --threshold 2 --amount 100

# 5. Mesurer le débit
./chaingo bench --txs 10000
```

> Le binaire compile nativement sur Windows, Linux et macOS. Sur Windows :
> `go build -o chaingo.exe ./cmd/chaingo` puis `.\chaingo.exe ...`.

Documentation complète :

- [Référence API](docs/API.md) — tous les endpoints + signature des transactions
- [Guide opérateur de nœud](docs/TESTNET-DEPLOY.md) — installation, HTTPS, sauvegarde
- [Hébergement 24/24](docs/DEPLOYMENT.md) — déploiement express, Docker, systemd
- [Guide validateur & délégateur](docs/VALIDATOR.md)
- [Préparation du mainnet](docs/MAINNET.md) — distribution, vesting on-chain, cérémonie
- [Feuille de route](ROADMAP.md) · [Contribuer](CONTRIBUTING.md) · [Sécurité](SECURITY.md)

## État du projet

- **Phase 1 — Fondations** : ✅ complète.
- **Phase 2 — Sécurité de production** : 🟢 consensus BFT durci (set de validateurs figé
  par hauteur, verrouillage POL, slashing complet, fork-choice + reorg testé en partition),
  codec binaire, fuzzing, gouvernance des mises à jour. **Reste** : audit externe et, surtout,
  des **validateurs indépendants** (aujourd'hui sur les machines du mainteneur — c'est le vrai
  jalon de décentralisation avant mainnet).
- **Phase 4 — Smart contracts no-code** : 🟢 templates vesting / escrow / multisig / **DAO**
  livrés + déployables depuis le studio. Moteur **WASM** (contrats arbitraires, en WebAssembly) :
  **câblé en consensus sur testnet/devnet** — on déploie du bytecode (`wasm_deploy`) et on
  l'appelle (`wasm_call`) via le studio, la CLI ou l'API. Déterminisme tenu par le gas
  (instrumentation, fuzzé 5,3 M exéc.), un jeu d'opcodes restreint validé au déploiement et
  l'interpréteur wazero ; vérifié par un test multi-validateurs (4 nœuds, même racine d'état).
  **Désactivé sur mainnet (`WasmEnabled=false`) jusqu'à audit externe** — voir
  [docs/design/wasm-vm.md](docs/design/wasm-vm.md).
- **Phase 5 — Écosystème** : 🟢 wallet web, explorateur, studio, dashboard validateur, banc d'essai.
  **Reste** : SDK JS/Python, doc EN complète.
- **Phase 3 — Anonymat fort (zk-STARK)** : 🟡 **R&D avancée**. Pile **zk-STARK
  maison post-quantique** (corps Goldilocks, FRI, AIR multi-colonnes, hachage
  Poseidon) + **circuit de transaction blindée fonctionnel** : prouve en
  zero-knowledge une dépense valide (appartenance Merkle + nullifier + conservation
  de valeur), **montants cachés** (masquage ZK testé) et **destinataire caché**
  (notes chiffrées ML-KEM). Sécurité hash-only (zéro courbe, zéro trusted setup).
  **Durcissement livré** : **grinding Fiat-Shamir** (PoW anti-broyage, +16 bits de
  soundness), **échantillonnage des requêtes sans remise**, **profondeur de pliage
  FRI variable**, **transactions blindées M-entrées / N-sorties** (join-split :
  fusion et fractionnement de notes, conservation `Σ in = Σ out + frais` prouvée),
  et **prouveur ~77× plus rapide** (141 s → **1,8 s** : inversion par lots, suite
  géométrique, parallélisation). Le **format M-in/N-out est câblé on-chain** (state +
  wallet/CLI, dédup des nullifiers anti création de valeur) et la **capacité du pool
  est passée à 4096 notes**. **Range-proofs livrés** (valeurs de note bornées
  `< 2⁴⁸`, fermeture de la création de valeur par débordement modulaire) et
  **soundness ≥128 bits conjecturée** (40 requêtes FRI + grinding 16 bits +
  amplification OOD multi-points). Le tout est **activé sur devnet + testnet** (gate
  `PrivacyEnabled` ON) — utilisable dès maintenant ; **revue de sécurité en cours**
  avant le mainnet. Dossier de preuve :
  [docs/PREUVE-PHASE3.md](docs/PREUVE-PHASE3.md). Reste : revue de sécurité ;
  soundness 128 bits *prouvée* (non conjecturée, via corps d'extension).

Voir [ROADMAP.md](ROADMAP.md) pour le détail complet et honnête.

## Licence

Apache 2.0 — voir [LICENSE](LICENSE) et [NOTICE](NOTICE).
