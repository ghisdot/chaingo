# Référence API REST

Base : `http://<nœud>:8545/v1` — JSON partout, CORS ouvert, erreurs uniformes
`{"error": "message"}`. Aucune authentification : l'API est publique, la sécurité vient
des signatures ML-DSA-65 des transactions.

## Lecture

| Endpoint | Description |
|---|---|
| `GET /v1/status` | hauteur, chain_id, pairs, mempool, supply, base fee, **params de la chaîne** |
| `GET /v1/fees` | tout ce qu'il faut pour construire une tx (voir ci-dessous) |
| `GET /v1/supply` | `{total, minted, burned}` en ucgo + format lisible |
| `GET /v1/blocks?limit=20` | derniers blocs (max 200) |
| `GET /v1/blocks/{hauteur\|latest}` | un bloc avec ses transactions |
| `GET /v1/tx/{hash}` | `{tx, block_height, status}` — 404 si inconnue/en attente |
| `GET /v1/accounts/{adresse}` | `{balances, nonce, staked, unbonding, delegations}` |
| `GET /v1/validators` | set actif : stake, délégations, blocs proposés, récompenses |
| `GET /v1/tokens` · `GET /v1/tokens/{symbole}` | registre des tokens no-code |
| `GET /v1/contracts` · `GET /v1/contracts/{id}` | smart contracts no-code (vesting, escrow) : statut, montants verrouillés/libérés |
| `GET /v1/mempool` | taille de la file |
| `GET /v1/genesis` | document de genèse — sert à rejoindre le réseau |

### `GET /v1/fees` — à appeler avant chaque transaction

```json
{
  "base_fee": 100000,              // burn actuel par tx (ucgo) — dynamique EIP-1559
  "suggested_max_base": 200000,    // plafond conseillé (marge anti-pic)
  "suggested_tip": 50000,          // tip standard
  "fast_tip": 200000,              // tip prioritaire
  "private_extra_burn": 200000,    // surcoût du mode private
  "token_create_fee": 10000000000, // 10 CGO brûlés
  "min_validator_stake": 10000000000000,
  "min_delegation": 1000000000,
  "delegation_commission_bps": 1000,
  "unbonding_seconds": 1814400
}
```

## Écriture : `POST /v1/tx`

Corps = transaction **signée**. Tous les montants en ucgo (1 CGO = 10⁹ ucgo).

```json
{
  "chain_id": "chaingo-dev-1",        // GET /v1/status
  "type": "transfer",                 // transfer | create_token | mint | stake | unstake | delegate | undelegate
  "from": "cg…",                      // adresse dérivée de la clé publique
  "from_pub_key": "<base64>",         // clé publique ML-DSA-65 (1952 octets)
  "to": "cg…",                        // destinataire / validateur (selon le type)
  "token_id": "CGO",                  // transfer : token transféré
  "amount": 1500000000,               // 1.5 CGO
  "nonce": 0,                         // GET /v1/accounts/{from} → nonce
  "max_base_fee": 200000,             // plafond de base fee accepté
  "tip": 50000,                       // enchère au validateur (priorité)
  "private": false,                   // confidentialité accrue (surcoût brûlé)
  "memo": "facture #42",              // max 256 caractères
  "token": {                          // create_token uniquement :
    "symbol": "MONTOK", "name": "Mon Token",
    "decimals": 9, "supply": 1000000000000000, "mintable": true
  },
  "contract": {                       // contract_create uniquement :
    "template": "vesting",            // vesting | escrow
    "token_id": "CGO", "amount": 100000000000,
    "beneficiary": "cg…", "start_ms": 1781300000000, "end_ms": 1783900000000,
    "seller": "cg…", "arbiter": "cg…" // escrow
  },
  "contract_id": "9066d8ac…",         // contract_exec : hash de la tx de création
  "action": "claim",                  // contract_exec : claim | release | refund
  "timestamp": 1781234567890,
  "signature": "<base64>"             // ML-DSA-65 sur le JSON canonique sans `signature`
}
```

**Signature** : sérialiser la transaction en JSON (ordre des champs = ordre de déclaration,
champ `signature` omis), signer ces octets en ML-DSA-65, mettre la signature en base64.
Implémentation de référence : `SignWith` dans [internal/types/tx.go](../internal/types/tx.go)
et `signAndSubmit` dans [cmd/chaingo/main.go](../cmd/chaingo/main.go).

Réponse : `202 {"hash": "…", "status": "pending"}` puis suivre `GET /v1/tx/{hash}`.

## Devnet uniquement (`--dev`)

| Endpoint | Description |
|---|---|
| `POST /v1/dev/faucet` | `{"address": "cg…", "amount": 100000000000}` → CGO de test |
| `POST /v1/dev/wallet` | génère un wallet (renvoie la seed — dev seulement !) |

## Exemple complet (curl)

```bash
# État du réseau
curl http://localhost:8545/v1/status
# Financer un wallet de test
curl -X POST http://localhost:8545/v1/dev/faucet \
  -d '{"address":"cg982fbc54bcbe39cc078d1ed519a0cf228309f44a","amount":100000000000}'
# Suivre une transaction
curl http://localhost:8545/v1/tx/2fddadc5a01b…
```
