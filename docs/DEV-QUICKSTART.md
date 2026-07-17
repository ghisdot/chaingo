# Quickstart développeur — construire sur ChainGO

Tout ce qu'il faut pour lire la chaîne et envoyer des transactions signées
(ML-DSA-65) depuis votre propre app, en quelques minutes.

- **Exemple de dApp prêt à forker** : [`examples/dapp/pulse`](../examples/dapp/pulse)
  (tableau de bord réseau, 100 % REST, sans dépendance).
- **Référence API complète** : [docs/API.md](API.md) · <https://chaingo.org/api/>

---

## 1. Un nœud à interroger

Lancez-en un en local, ou visez un nœud public :

```bash
# Local (devnet : validateur + faucet inclus)
docker run -d -p 8545:8545 ghcr.io/ghisdot/chaingo:latest node start --dev --api :8545
# → API sur http://127.0.0.1:8545
```

Testnet public : `https://node.chaingo.org` (adaptez à votre nœud).

## 2. Lire la chaîne (REST, aucune clé)

```bash
curl http://127.0.0.1:8545/v1/status                 # hauteur, chain_id, supply, validateurs…
curl http://127.0.0.1:8545/v1/accounts/<cg-adresse>  # soldes + nonce
curl http://127.0.0.1:8545/v1/blocks/latest          # dernier bloc
curl http://127.0.0.1:8545/v1/fees                    # à appeler AVANT chaque tx
curl http://127.0.0.1:8545/v1/tokens                  # tokens créés
curl http://127.0.0.1:8545/v1/contracts               # contrats no-code
```

En JavaScript, c'est un simple `fetch` — voir l'exemple `pulse` :

```js
const s = await (await fetch(node + "/v1/status")).json();
console.log(s.height, s.chain_id, s.pq_signature);
```

## 3. Écrire : signer & envoyer une transaction

Une transaction est un objet JSON **signé en ML-DSA-65**. La signature porte sur
le **JSON canonique** de la transaction (ordre des champs = ordre de déclaration,
champ `signature` omis) — voir [docs/API.md](API.md#écriture--post-v1tx) pour le
schéma exact. La clé privée ne quitte jamais le client.

Trois façons de signer, du plus simple au plus bas niveau :

### a) En CLI (le plus rapide)

```bash
chaingo wallet new alice                 # affiche la phrase de récupération 24 mots
chaingo faucet --to alice --amount 500   # devnet
chaingo send --from alice --to <cg-adresse> --amount 42.5 --fast
```

### b) Avec un SDK (JS / Python)

Signature ML-DSA-65 identique au nœud, packaging à venir sur npm/PyPI :

- **JavaScript** : <https://github.com/ghisdot/chaingo-sdk-js>
- **Python** : <https://github.com/ghisdot/chaingo-sdk-py>

### c) Dans le navigateur (module WASM du wallet)

Le wallet web charge `chaingo.wasm` (le MÊME code Go que le nœud) et expose :

```js
// Générer / restaurer une clé
const w = chaingoNewWallet();                 // {address, seedHex, mnemonic}
const r = chaingoSeedFromMnemonic("mot1 …");   // {seedHex, address}

// Signer une transaction (la clé reste dans le navigateur)
const tx = { chain_id, type: "transfer", to, amount, nonce, max_base_fee, tip };
const signed = chaingoSignTransaction(w.seedHex, JSON.stringify(tx)); // {signed, hash}

// Diffuser
await fetch(node + "/v1/tx", { method:"POST",
  headers:{ "Content-Type":"application/json" }, body: signed.signed });
```

> `nonce` = `GET /v1/accounts/{from}` → `nonce` ; `max_base_fee`/`tip` : voir
> `GET /v1/fees`. Les montants sont en **ucgo** (1 CGO = 10⁹ ucgo).

## 4. Suivre une transaction

```bash
curl http://127.0.0.1:8545/v1/tx/<hash>   # {status: "confirmed", block_height: N}
```

---

## Aller plus loin

- **Contrats no-code** (tokens, vesting, escrow, multisig, DAO, presale, timelock,
  airdrop, streaming) : déployables via des tx `create_token` / `contract_create`
  — schémas dans [docs/API.md](API.md).
- **Contrats WASM arbitraires** (Rust, AssemblyScript…) : voir
  [docs/design/wasm-vm.md](design/wasm-vm.md) et l'exemple
  [examples/contracts/counter](../examples/contracts/counter).
- **Déterminisme & signature** : implémentation de référence dans
  `internal/types/tx.go` (`SigningBytes`, `SignWith`).
