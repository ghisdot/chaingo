# ChainGO

[![CI](https://github.com/ghisdot/chaingo/actions/workflows/ci.yml/badge.svg)](https://github.com/ghisdot/chaingo/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)

🇫🇷 [Version française](README.md)

**Post-quantum blockchain** written in Go — every signature (transactions, blocks,
validators) uses **ML-DSA-65** (FIPS 204, NIST security level 3), the quantum-resistant
signature standard. SHA3-256 hashing. **~31,000 TPS measured end-to-end** — 20× the
1,500 TPS target.

## Chain rules (decided, not hardcoded)

Economic rules live in the **genesis document** (`params`) — every ChainGO network
chooses its own. Defaults (`types.DefaultParams()`):

| Rule | Value |
|---|---|
| Genesis supply | 1,000,000,000 CGO (9 decimals) — no hard cap, elastic supply |
| Mainnet distribution | 50% community · 20% treasury · 15% team (4-year vesting) · 10% ecosystem · 5% genesis/liquidity |
| Emission | ~3%/year on total stake, minted to block proposers |
| Fees | dynamic EIP-1559: **burned** base fee (0.0001 CGO floor) + free-market tip |
| Token creation | 10 CGO burned (anti-spam), fully no-code |
| Validators | 10,000 CGO minimum stake; fallback rounds keep liveness when validators go offline |
| Delegation | from 1 CGO, 10% validator commission, pro-rata rewards every block |
| Unbonding | 21 days (5 min on devnet) |

## Quickstart

Works on Windows, Linux and macOS (`go build ./cmd/chaingo`), Docker included.

```bash
chaingo node start --dev                  # devnet node + website on http://localhost:8545
chaingo wallet new alice                  # post-quantum wallet
chaingo faucet --to alice --amount 500
chaingo send --from alice --to cg… --amount 42.5 --fast
chaingo token create --from alice --symbol MYTOK --name "My Token" --supply 1000000
chaingo stake --from alice --amount 12000     # become a validator
chaingo delegate --from bob --to cg… --amount 50  # or delegate from 1 CGO
chaingo bench --txs 10000                 # measure TPS locally
```

Join an existing network:

```bash
chaingo node start --genesis-url http://<node>:8545/v1/genesis --peers <node>:9000
```

## Documentation

- [API reference](docs/API.md) — every endpoint + how to build and sign a transaction
- [24/7 deployment](docs/DEPLOYMENT.md) — VPS, systemd, Docker, HTTPS, backups
- [Validator & delegator guide](docs/VALIDATOR.md)
- [Roadmap](ROADMAP.md) · [Contributing](CONTRIBUTING.md) · [Security policy](SECURITY.md)

## Honest status

This is a **devnet**: feature-complete and fast, not yet ready for real value.
Multi-validator BFT finality votes and slashing are Phase 2; strong anonymity
(zk-STARK confidential transfers — the only quantum-resistant ZK family) is Phase 3;
no-code smart contracts (vesting, escrow, multisig, DAO templates) are Phase 4.
See [ROADMAP.md](ROADMAP.md).

License: [MIT](LICENSE).
