# REST API Reference

Base: `http://<node>:8545/v1` — JSON everywhere, open CORS, uniform errors
`{"error": "message"}`. No authentication: the API is public, security comes
from the ML-DSA-65 signatures on transactions.

## Reads

| Endpoint | Description |
|---|---|
| `GET /v1/status` | height, chain_id, peers, mempool, supply, base fee, **chain params** |
| `GET /v1/fees` | everything needed to build a tx (see below) |
| `GET /v1/supply` | `{total, minted, burned}` in ucgo + human-readable format |
| `GET /v1/blocks?limit=20` | latest blocks (max 200) |
| `GET /v1/blocks/{height\|latest}` | a block with its transactions |
| `GET /v1/tx/{hash}` | `{tx, block_height, status}` — 404 if unknown/pending |
| `GET /v1/accounts/{address}` | `{balances, nonce, staked, unbonding, delegations}` |
| `GET /v1/accounts/{address}/txs?limit=50&before=<height>` | history of txs involving the address (paginated, most recent first) — for the explorer |
| `GET /v1/blocks/by-hash/{hash}` | block looked up by its hash (rather than by its height) |
| `GET /v1/search?q=<term>` | universal search: detects height / block hash / tx hash / address / token symbol → returns `{type, ref, url}` |
| `GET /v1/validators` | active set: stake, delegations, blocks proposed, rewards |
| `GET /v1/tokens` · `GET /v1/tokens/{symbol}` | no-code token registry |
| `GET /v1/contracts` · `GET /v1/contracts/{id}` | no-code smart contracts (vesting, escrow, multisig, dao, presale, timelock, airdrop, streaming): status, locked/released amounts, proposals |
| `GET /v1/mempool` | queue size |
| `GET /v1/genesis` | genesis document — used to join the network |

### `GET /v1/fees` — call before every transaction

```json
{
  "base_fee": 100000,              // current burn per tx (ucgo) — dynamic EIP-1559
  "suggested_max_base": 200000,    // recommended cap (anti-spike margin)
  "suggested_tip": 50000,          // standard tip
  "fast_tip": 200000,              // priority tip
  "private_extra_burn": 200000,    // surcharge for private mode
  "token_create_fee": 10000000000, // 10 CGO burned
  "min_validator_stake": 10000000000000,
  "min_delegation": 1000000000,
  "delegation_commission_bps": 1000,
  "unbonding_seconds": 1814400
}
```

## Writes: `POST /v1/tx`

Body = **signed** transaction. All amounts in ucgo (1 CGO = 10⁹ ucgo).

```json
{
  "chain_id": "chaingo-dev-1",        // GET /v1/status
  "type": "transfer",                 // transfer | create_token | mint | burn | stake | unstake |
                                      // delegate | undelegate | unjail | validator_profile |
                                      // contract_create | contract_exec | wasm_deploy | wasm_call |
                                      // shield | shielded_transfer | unshield
  "from": "cg…",                      // address derived from the public key
  "from_pub_key": "<base64>",         // ML-DSA-65 public key (1952 bytes)
  "to": "cg…",                        // recipient / validator (depending on type)
  "token_id": "CGO",                  // transfer/mint/burn: token concerned
  "amount": 1500000000,               // 1.5 CGO
  "nonce": 0,                         // GET /v1/accounts/{from} → nonce
  "max_base_fee": 200000,             // accepted base fee cap
  "tip": 50000,                       // bid to the validator (priority)
  "private": false,                   // increased privacy (burned surcharge)
  "memo": "invoice #42",              // max 256 characters
  "token": {                          // create_token only:
    "symbol": "MONTOK", "name": "Mon Token",
    "decimals": 9, "supply": 1000000000000000, "mintable": true,
    "max_supply": 0,                  // hard cap (0 = unlimited; requires mintable)
    "burnable": true,                 // any holder can burn their tokens (burn type)
    "logo_uri": "https://…/logo.png", "description": "…", "website": "https://…"  // metadata (optional)
  },
  "contract": {                       // contract_create only:
    "template": "vesting",            // vesting | escrow | multisig | dao | presale | timelock | airdrop | streaming
    "token_id": "CGO", "amount": 100000000000,
    "beneficiary": "cg…", "start_ms": 1781300000000, "end_ms": 1783900000000, // vesting/timelock/streaming
    "seller": "cg…", "arbiter": "cg…",// escrow
    "signers": ["cg…"], "threshold": 2,// multisig/dao: signers/members + quorum; airdrop: recipients (signers)
    "price": 500000000                // presale: ucgo per base unit of the token sold
  },
  "contract_id": "9066d8ac…",         // contract_exec: hash of the creation tx
  "action": "claim",                  // contract_exec: claim | release | refund | propose | approve | reject | buy | cancel
  "proposal": 0,                      // multisig/dao approve|reject: proposal index
  "timestamp": 1781234567890,
  "signature": "<base64>"             // ML-DSA-65 over the canonical JSON without `signature`
}
```

**Signature**: serialize the transaction to JSON (field order = declaration order,
`signature` field omitted), sign those bytes with ML-DSA-65, encode the signature in base64.
Reference implementation: `SignWith` in [internal/types/tx.go](../internal/types/tx.go)
and `signAndSubmit` in [cmd/chaingo/main.go](../cmd/chaingo/main.go).

Response: `202 {"hash": "…", "status": "pending"}` then poll `GET /v1/tx/{hash}`.

## Devnet only (`--dev`)

| Endpoint | Description |
|---|---|
| `POST /v1/dev/faucet` | `{"address": "cg…", "amount": 100000000000}` → test CGO |
| `POST /v1/dev/wallet` | generates a wallet (returns the seed — dev only!) |

## Full example (curl)

```bash
# Network status
curl http://localhost:8545/v1/status
# Fund a test wallet
curl -X POST http://localhost:8545/v1/dev/faucet \
  -d '{"address":"cg982fbc54bcbe39cc078d1ed519a0cf228309f44a","amount":100000000000}'
# Track a transaction
curl http://localhost:8545/v1/tx/2fddadc5a01b…
```
