# ChainGO

[![CI](https://github.com/ghisdot/chaingo/actions/workflows/ci.yml/badge.svg)](https://github.com/ghisdot/chaingo/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)

🇬🇧 [English version](README.en.md)

Blockchain **post-quantique** écrite en Go — toutes les signatures (transactions, blocs, validateurs) utilisent **ML-DSA-65** (FIPS 204, niveau de sécurité NIST 3), le standard de signature résistant au quantique. Hachage SHA3-256.

**Mesuré sur cette machine : ~31 000 TPS bout-en-bout** (vérification PQ parallèle + exécution) — objectif de 1 500 TPS dépassé ×20.

## Les règles de la chaîne (décidées, pas codées en dur)

Les règles économiques vivent dans le **document de genèse** (`params`) : chaque réseau ChainGO choisit les siennes. Valeurs par défaut (`types.DefaultParams()`) :

| Règle | Valeur | Détail |
|---|---|---|
| Supply à la genèse | **1 milliard CGO** | 9 décimales (1 CGO = 10⁹ ucgo) |
| Max supply | **aucun plafond dur** | supply élastique : l'émission (~3 %/an sur le stake) est contrebalancée par le burn — avec un réseau actif, la supply diminue |
| Distribution genèse (mainnet) | **communauté d'abord** | 50 % communauté (airdrops testnet, programmes d'adoption), 20 % trésorerie, 15 % créateur/équipe (vesting 4 ans), 10 % écosystème, 5 % validateurs genèse + liquidité |
| Émission | **~3 %/an sur le stake total** | minté au proposeur de chaque bloc ; le tirage étant pondéré par le stake, le rendement attendu est ~3 %/an pour chaque validateur |
| Frais | **EIP-1559 dynamiques** | base fee ajusté à la congestion (cible 1000 tx/bloc, ±12.5 % max par bloc, plancher 0.0001 CGO) et **brûlé** ; tip libre au validateur (marché) |
| Burn | automatique | base fee + surcoût `private` + frais de création de token → la chaîne devient déflationniste quand l'usage dépasse l'émission |
| Mode `private` | burn supplémentaire = 2 × base fee | v1 : voie réservée — la confidentialité forte (zk-STARK) arrive en Phase 3 |
| Création de token | 10 CGO brûlés | anti-spam |
| Smart contracts no-code | **vesting + escrow**, 1 CGO brûlé à la création | templates natifs paramétrés — aucun code à écrire ni auditer ; multisig/DAO en Phase 4 |
| Validateurs | **stake minimum 10 000 CGO** | en dessous : transaction rejetée |
| Délégation | **dès 1 CGO**, commission validateur 10 % | les petits holders délèguent à un validateur et touchent leur part des récompenses au pro-rata, à chaque bloc qu'il propose |
| Unbonding | **21 jours** (mainnet) / 5 min (devnet) | s'applique au stake ET aux délégations retirées ; futurs slashing appliqués dessus (Phase 2) |
| Blocs | 500 ms, max 2000 tx | défini dans la genèse, pas par nœud |

## Consensus « Aurora » (PoS)

- Proposeur tiré au sort **pondéré par le stake**, seedé par `(hash précédent, hauteur, round)` — déterministe sur tous les nœuds.
- **Rounds de secours** : si le proposeur élu ne produit pas dans l'intervalle de bloc (validateur hors-ligne), le round suivant désigne un autre validateur. Testé : la chaîne avance sans accroc avec 29 % du stake mort.
- Finalité immédiate en devnet ; les votes BFT 2f+1 et le slashing arrivent en Phase 2.

## Documentation

- [Référence API](docs/API.md) — tous les endpoints + comment construire et signer une transaction
- [Hébergement 24/24](docs/DEPLOYMENT.md) — VPS, systemd, Docker, HTTPS, sauvegardes
- [Guide validateur & délégateur](docs/VALIDATOR.md)
- [Préparation du mainnet](docs/MAINNET.md) — distribution, vesting on-chain, cérémonie de genèse
- [Feuille de route](ROADMAP.md) · [Contribuer](CONTRIBUTING.md) · [Sécurité](SECURITY.md)

## Démarrage rapide

Multi-plateforme : le code Go compile nativement sur **Windows, Linux et macOS**
(`go build ./cmd/chaingo`, ou `.\scripts\build-all.ps1` pour tout compiler dans `dist/`,
ou Docker — la CI GitHub produit les binaires des 5 plateformes à chaque release).

```powershell
go build -o chaingo.exe ./cmd/chaingo

# 1. Lancer un devnet (génère validateur + faucet)
.\chaingo.exe node start --dev

# 2. Créer un wallet (clés ML-DSA-65)
.\chaingo.exe wallet new alice

# 3. Se financer via le faucet devnet
.\chaingo.exe faucet --to alice --amount 500

# 4. Transférer — tip par défaut, --fast (tip x4) ou --tip libre
.\chaingo.exe send --from alice --to bob --amount 42.5 --fast --memo "hello"

# 4bis. Wallet web (signature ML-DSA-65 dans le navigateur) — construire le WASM une fois :
.\scripts\build-wasm.ps1
# puis ouvrir http://localhost:8545/wallet/

# 5. Créer un token SANS CODE
.\chaingo.exe token create --from alice --symbol MONTOK --name "Mon Token" --supply 1000000 --mintable

# 5bis. Smart contracts SANS CODE : vesting (déblocage progressif) et escrow (séquestre)
.\chaingo.exe contract vesting --from alice --beneficiary <adresse> --amount 100 --duration 720h
.\chaingo.exe contract escrow --from alice --seller <adresse> --amount 50 [--arbiter <adresse>]
.\chaingo.exe contract claim --from bob --id <contrat>      # le bénéficiaire récupère la part débloquée
.\chaingo.exe contract release --from alice --id <contrat>  # l'acheteur libère le séquestre

# 6. Devenir validateur (minimum 10 000 CGO)…
.\chaingo.exe stake --from alice --amount 12000
# … ou déléguer dès 1 CGO à un validateur et toucher sa part des récompenses
.\chaingo.exe delegate --from bob --to <adresse_validateur> --amount 50
# En sortir (unbonding : 5 min en devnet, 21 j en mainnet)
.\chaingo.exe unstake --from alice --amount 12000
.\chaingo.exe undelegate --from bob --to <adresse_validateur> --amount 50

# Mesurer le débit local
.\chaingo.exe bench --txs 10000
```

### Rejoindre un réseau (multi-nœuds)

```powershell
.\chaingo.exe node start --datadir .\node2 --api 127.0.0.1:8546 --p2p 127.0.0.1:9001 `
    --genesis-url http://127.0.0.1:8545/v1/genesis --peers 127.0.0.1:9000
```

Le nouveau nœud récupère la genèse (et donc les règles), se synchronise par lots, puis suit les blocs en gossip. Sans clé validateur il sert l'API et relaie ; avec `--validator-seed` et du stake il propose des blocs.

## API REST (`http://localhost:8545/v1`)

| Méthode | Route | Description |
|---|---|---|
| GET | `/v1/status` | hauteur, chain_id, pairs, supply, base fee, **params de la chaîne** |
| GET | `/v1/fees` | base fee courant, max conseillé, tips suggérés, coûts (token, stake min, unbonding) |
| GET | `/v1/supply` | total / minté / **brûlé** (+ format lisible) |
| GET | `/v1/blocks?limit=20` | derniers blocs |
| GET | `/v1/blocks/{hauteur\|latest}` | un bloc |
| POST | `/v1/tx` | soumettre une tx signée ML-DSA-65 |
| GET | `/v1/tx/{hash}` | statut + bloc d'inclusion |
| GET | `/v1/accounts/{adresse}` | soldes, nonce, stake, **unbonding** |
| GET | `/v1/validators` | set de validateurs (stake, blocs, récompenses) |
| GET | `/v1/tokens` · `/v1/tokens/{symbole}` | registre des tokens no-code |
| GET | `/v1/mempool` | taille de la file |
| GET | `/v1/genesis` | document de genèse (pour rejoindre le réseau) |
| POST | `/v1/dev/faucet` | `{address, amount}` — devnet uniquement |
| POST | `/v1/dev/wallet` | génère un wallet — devnet uniquement |

Construire une tx côté client : `GET /v1/fees` → remplir `max_base_fee` (plafond accepté) et `tip` (enchère), signer en ML-DSA-65, `POST /v1/tx`.

## Architecture

```
cmd/chaingo/          CLI : node, wallet, send, token, stake, faucet, bench
internal/crypto/      ML-DSA-65 (cloudflare/circl) + SHA3-256, adresses cg…
internal/types/       Transaction (5 types), Block, Merkle, Params (règles)
internal/state/       comptes, tokens, validateurs, unbonding, supply, base fee
internal/mempool/     file ordonnée par tip (marché), chaînes de nonces
internal/consensus/   moteur PoS Aurora : production, validation, rounds de secours
internal/p2p/         gossip TCP : hello/tx/block/get_blocks
internal/store/       persistance bbolt : blocs, index des tx, état
internal/api/         serveur REST
internal/wallet/      keystore scrypt + AES-256-GCM
internal/genesis/     document de genèse (chain_id, params, allocations)
```

## Limites v1 assumées & feuille de route

La v1 est un **devnet honnête** : rapide, post-quantique, complet fonctionnellement, mais pas encore prêt pour de la valeur réelle.

- **Phase 2 — consensus BFT complet** : votes de finalité multi-validateurs (2f+1), **slashing** (les fonds en unbonding y sont déjà préparés), fork-choice ; arbre de Merkle creux pour la racine d'état (actuellement O(n) par bloc) ; codec binaire compact (les signatures ML-DSA font 3,3 Ko — le JSON+base64 limite la bande passante réseau).
- **Phase 3 — confidentialité réelle** : le flag `--private` deviendra des transferts confidentiels à base de preuves **zk-STARK** (résistantes au quantique, contrairement aux ring signatures classiques) + adresses furtives.
- **Phase 4 — smart contracts no-code** : moteur de templates déclaratifs (vesting, escrow, multisig, DAO) déployables via l'API, puis VM WASM déterministe.
- **Phase 5 — explorateur web + SDK** JS/Python sur l'API existante.
