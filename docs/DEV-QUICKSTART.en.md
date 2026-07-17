# Developer quickstart — build on ChainGO

Everything you need to read the chain and send signed (ML-DSA-65) transactions
from your own app, in a few minutes.

- **Ready-to-fork example dApp**: [`examples/dapp/pulse`](../examples/dapp/pulse)
  (network dashboard, 100% REST, zero dependencies).
- **Full API reference**: [docs/API.en.md](API.en.md) · <https://chaingo.org/api/>

---

## 1. A node to talk to

Run one locally, or point at a public node:

```bash
# Local (devnet: validator + faucet included)
docker run -d -p 8545:8545 ghcr.io/ghisdot/chaingo:latest node start --dev --api :8545
# → API on http://127.0.0.1:8545
```

Public testnet: `https://node.chaingo.org` (adjust to your node).

## 2. Read the chain (REST, no keys)

```bash
curl http://127.0.0.1:8545/v1/status                 # height, chain_id, supply, validators…
curl http://127.0.0.1:8545/v1/accounts/<cg-address>  # balances + nonce
curl http://127.0.0.1:8545/v1/blocks/latest          # latest block
curl http://127.0.0.1:8545/v1/fees                    # call BEFORE every tx
curl http://127.0.0.1:8545/v1/tokens                  # created tokens
curl http://127.0.0.1:8545/v1/contracts               # no-code contracts
```

In JavaScript it's a plain `fetch` — see the `pulse` example:

```js
const s = await (await fetch(node + "/v1/status")).json();
console.log(s.height, s.chain_id, s.pq_signature);
```

## 3. Write: sign & send a transaction

A transaction is a JSON object **signed with ML-DSA-65**. The signature covers
the **canonical JSON** of the transaction (field order = declaration order, the
`signature` field omitted) — see [docs/API.en.md](API.en.md) for the exact
schema. The private key never leaves the client.

Three ways to sign, easiest to lowest-level:

### a) Via the CLI (fastest)

```bash
chaingo wallet new alice                 # prints the 24-word recovery phrase
chaingo faucet --to alice --amount 500   # devnet
chaingo send --from alice --to <cg-address> --amount 42.5 --fast
```

### b) With an SDK (JS / Python)

ML-DSA-65 signing identical to the node, npm/PyPI packaging coming:

- **JavaScript**: <https://github.com/ghisdot/chaingo-sdk-js>
- **Python**: <https://github.com/ghisdot/chaingo-sdk-py>

### c) In the browser (wallet WASM module)

The web wallet loads `chaingo.wasm` (the SAME Go code as the node) and exposes:

```js
// Generate / restore a key
const w = chaingoNewWallet();                 // {address, seedHex, mnemonic}
const r = chaingoSeedFromMnemonic("word1 …");  // {seedHex, address}

// Sign a transaction (the key stays in the browser)
const tx = { chain_id, type: "transfer", to, amount, nonce, max_base_fee, tip };
const signed = chaingoSignTransaction(w.seedHex, JSON.stringify(tx)); // {signed, hash}

// Broadcast
await fetch(node + "/v1/tx", { method:"POST",
  headers:{ "Content-Type":"application/json" }, body: signed.signed });
```

> `nonce` = `GET /v1/accounts/{from}` → `nonce`; `max_base_fee`/`tip`: see
> `GET /v1/fees`. Amounts are in **ucgo** (1 CGO = 10⁹ ucgo).

## 4. Track a transaction

```bash
curl http://127.0.0.1:8545/v1/tx/<hash>   # {status: "confirmed", block_height: N}
```

---

## Going further

- **No-code contracts** (tokens, vesting, escrow, multisig, DAO, presale, timelock,
  airdrop, streaming): deployable via `create_token` / `contract_create` txs —
  schemas in [docs/API.en.md](API.en.md).
- **Arbitrary WASM contracts** (Rust, AssemblyScript…): see
  [docs/design/wasm-vm.en.md](design/wasm-vm.en.md) and the
  [examples/contracts/counter](../examples/contracts/counter) example.
- **Determinism & signing**: reference implementation in `internal/types/tx.go`
  (`SigningBytes`, `SignWith`).
