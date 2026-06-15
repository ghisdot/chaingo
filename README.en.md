# ChainGO

[![CI](https://github.com/ghisdot/chaingo/actions/workflows/ci.yml/badge.svg)](https://github.com/ghisdot/chaingo/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)

🇫🇷 [Version française](README.md) · 🌐 [chaingo.org](https://chaingo.org)

**Post-quantum blockchain** written in Go. Every signature (transactions,
blocks, votes) uses **ML-DSA-65** (FIPS 204, NIST security level 3), the
quantum-resistant signature standard. SHA3-256 hashing.

**Public testnet is live.** Mainnet ships after external security audit and
finalization of the BFT consensus (Phase 2).

- 🔐 **Native post-quantum security**, end-to-end.
- ⚡ **~31,000 TPS** end-to-end (parallel PQ verification + execution).
- 🔥 **Deflationary economics**: burned EIP-1559 base fees, elastic supply.
- 🪙 **No-code**: tokens, vesting, escrow, multisig, DAO — deploy **from the browser** (studio), without writing a smart contract.
- 🌐 **P2P** network, anyone can join.

---

## Who is this for?

### 👤 You want to use ChainGO (transfers, wallet, tokens)

No install needed. Everything runs in the browser:

- **Post-quantum web wallet**: <https://chaingo.org/wallet/>
  Create a wallet, request testnet CGO, send transactions, manage tokens
  and no-code contracts. Keys are generated and stored **in your browser**
  (never sent to a server).
- **Block explorer**: <https://chaingo.org/explorer/>
  Browse blocks, transactions, accounts, validators and tokens live.
- **API reference**: <https://chaingo.org/api/>

### 🛡️ You want to run a node or become a validator

- [Node operator guide](docs/TESTNET-DEPLOY.md) — join the public testnet in
  15 minutes, or bootstrap your own chain.
- [Validator & delegator guide](docs/VALIDATOR.md) — staking, delegation,
  slashing, yield.

### 💻 You want to contribute to the code

- [Contributing guide](CONTRIBUTING.md) — project rules, process, invariants
  to respect (post-quantum crypto, determinism).
- [Roadmap](ROADMAP.md) — what's shipped, what's left.
- [API reference](docs/API.md) — for building clients or integrating.
- Security policy: [SECURITY.md](SECURITY.md).

---

## Chain rules

Economic rules live in the **genesis document** — every ChainGO network
picks its own. Defaults:

| Rule | Value |
|---|---|
| Genesis supply | 1,000,000,000 CGO (9 decimals) — no hard cap, elastic supply |
| Mainnet distribution | 50 % community · 20 % treasury · 15 % team (4-year vesting) · 10 % ecosystem · 5 % genesis/liquidity |
| Emission | ~3 %/year on total stake, minted to block proposers |
| Fees | dynamic EIP-1559: **burned** base fee + free-market tip |
| Token creation | 10 CGO burned (anti-spam), fully no-code |
| No-code smart contracts | vesting, escrow, multisig, DAO — 1 CGO burned per contract |
| Validators | 10,000 CGO minimum stake; fallback rounds keep liveness when validators go offline |
| Delegation | from 1 CGO, 10 % validator commission, pro-rata rewards every block |
| Unbonding | 21 days (mainnet), 24 h (testnet) |
| Blocks | 500 ms, max 2000 tx |

## "Aurora" consensus (PoS + BFT)

- Deterministic block proposer **weighted by stake**, seeded by `(prev hash,
  height, round)`.
- **Fallback rounds** for liveness when validators go offline.
- **Persistent BFT finality**: every block embeds `LastCommit` (≥ 2/3
  precommits on the parent) — finality is chain-verifiable and survives
  restarts.
- **Slashing**: 5 % on double-signing, 0.1 % and jail on extended downtime.
  Applied to stake **and** delegations.

## Quickstart (local development)

```bash
git clone https://github.com/ghisdot/chaingo
cd chaingo
go build -o chaingo ./cmd/chaingo

./chaingo node start --dev                    # local devnet (validator + faucet)
./chaingo wallet new alice
./chaingo faucet --to alice --amount 500
./chaingo send --from alice --to <cg-addr> --amount 42.5 --fast
./chaingo token create --from alice --symbol MYTOK --name "My Token" --supply 1000000
./chaingo contract multisig --from alice --signers a,b,c --threshold 2 --amount 100
./chaingo bench --txs 10000                   # measure TPS locally
```

Joining an existing network:

```bash
./chaingo node start \
  --genesis-url https://node.chaingo.org/v1/genesis \
  --peers node.chaingo.org:9000
```

Full documentation:

- [API reference](docs/API.md) — every endpoint + how to sign a transaction
- [Node operator guide](docs/TESTNET-DEPLOY.md) — install, HTTPS, backups
- [24/7 hosting](docs/DEPLOYMENT.md) — express deployment, Docker, systemd
- [Validator & delegator guide](docs/VALIDATOR.md)
- [Mainnet preparation](docs/MAINNET.md) — distribution, on-chain vesting, genesis ceremony
- [Roadmap](ROADMAP.md) · [Contributing](CONTRIBUTING.md) · [Security](SECURITY.md)

## Project status

- **Phase 1 — Foundations**: ✅ complete.
- **Phase 2 — Production security**: 🟢 hardened BFT (height-frozen validator set, POL
  locking, full slashing, fork-choice + reorg with a partition test), binary codec, fuzzing,
  network-upgrade governance. **Remaining**: external audit and — above all — **independent
  validators** (today on the maintainer's machines; this is the real decentralization milestone
  before mainnet).
- **Phase 4 — No-code smart contracts**: 🟢 vesting / escrow / multisig / **DAO** templates
  shipped and deployable from the studio. A WASM engine (arbitrary contracts) **preview** is
  shipped but **out-of-consensus**; the consensus-grade engine still has to be built and audited.
- **Phase 5 — Ecosystem**: 🟢 web wallet, explorer, studio, validator dashboard, load tester.
  **Remaining**: JS/Python SDKs, full EN docs.
- **Phase 3 — Strong anonymity (zk-STARK)**: ⬜ post-mainnet.

See [ROADMAP.md](ROADMAP.md) for the full, honest breakdown.

## License

MIT — see [LICENSE](LICENSE).
