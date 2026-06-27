# ChainGO mainnet preparation

> 🔴 **The mainnet is NOT launched.** The network remains on the **testnet**
> (`chaingo-testnet-1`) until all of the prerequisites below are
> met. This document is the roadmap and the operating procedure — not a
> trigger.

## 1. Prerequisites before launch (blocking checklist)

No mainnet until **everything** is checked:

- [ ] **Phase 2 complete**: full BFT finality (Tendermint-style locking), double-signing
      **and** downtime slashing, fork-choice, validator set frozen by height.
- [ ] **Network performance**: compact binary codec, sparse Merkle tree for state.
- [ ] **Tests**: broad unit/integration coverage, fuzzing of network inputs.
- [ ] **External security audit** passed, findings fixed.
- [ ] **≥ 4 independent validators** committed (n ≥ 3f+1; 4 tolerates 1 fault). Ideally more,
      operated by distinct entities.
- [ ] **Stable public testnet** for several weeks (uptime, real load testing).
- [ ] **Genesis distribution finalized and validated** (section 3), signed/multi-sig document.
- [ ] **Incident response plan** (compromised keys, emergency halt, communication).

## 2. Mainnet parameters (recap of agreed decisions)

| Rule | Mainnet value |
|---|---|
| chain_id | `chaingo-1` |
| Genesis supply | 1,000,000,000 CGO |
| Max supply | none (elastic: ~3%/year issuance vs burn) |
| Unbonding | **21 days** (`unbonding_seconds = 1814400`) — NOT the testnet value of 24 h |
| Min validator stake | 10,000 CGO |
| Double-signing slashing | 5% (`slash_double_sign_bps = 500`) |
| Faucet | **disabled** (neither `--dev` nor `--testnet`: launched with `--genesis mainnet.json`) |

All these settings live in the `params` of the genesis document — see
[internal/types/params.go](../internal/types/params.go).

## 3. Build the mainnet genesis

The tool: `chaingo genesis`.

```bash
# 1. Skeleton + 1st validator key
chaingo genesis template --chain-id chaingo-1 --out mainnet.json --seed-out v0.seed
# 2. Edit mainnet.json for the distribution (below), 21-day unbonding, etc.
# 3. Verify — the FINGERPRINT must be identical for ALL operators
chaingo genesis validate mainnet.json
```

### "Community first" distribution (1 B CGO) → genesis fields

| Share | CGO | Where, in the genesis |
|---|---|---|
| 50% Community | 500 M | `alloc` to a distribution address (airdrops/post-launch programs) |
| 20% Treasury | 200 M | `vesting` (gradual release) or `alloc` to a multisig vault |
| 15% Team | 150 M | **`vesting` 4 years** (linear on-chain release) |
| 10% Ecosystem | 100 M | `alloc` to the ecosystem fund |
| 5% Genesis / liquidity | 50 M | `stakes` (genesis validators) + `alloc` (liquidity) |

- **Vesting is enforced on-chain**: the team/treasury share is locked in
  vesting contracts created at block 0 (`vesting` in the JSON), released linearly
  between `start_ms` and `end_ms`. No one can bypass the schedule.
- ⚠️ **Provide a small liquid balance** (`alloc`) for each vesting beneficiary:
  claiming (`contract claim`) costs fees, so 0 liquid CGO = impossible to claim.

Example `vesting` block (team, 4 years) in `mainnet.json`:

```json
"vesting": [
  { "beneficiary": "cg<team>", "amount": 150000000000000000,
    "start_ms": 1750000000000, "end_ms": 1876000000000 }
],
"alloc": { "cg<team>": 1000000000 }
```

## 4. Genesis ceremony (multi-validator)

1. Each validator generates its key: `chaingo keygen --out vN.seed` and
   **publicly shares its cg… address** (never the seed).
2. A coordinator assembles `mainnet.json` (distribution + `stakes` of the N
   validators, parameters).
3. **Each participant runs `chaingo genesis validate mainnet.json` and
   compares the returned `block hash`.** The fingerprint must be strictly
   identical everywhere — this is the guarantee of starting on the same chain.
4. At the agreed time T, each operator launches its node:
   ```bash
   chaingo node start --genesis mainnet.json --validator-seed vN.seed \
     --datadir /var/lib/chaingo --api 127.0.0.1:8545 --p2p :9000 \
     --peers <seed-1-ip>:9000,<seed-2-ip>:9000
   ```
5. Verify that `finalized_height` progresses (≥ 2/3 of the stake online):
   the chain is live and finalizing.

## 5. Decisions to finalize before the ceremony

- **Vault addresses** (community, treasury, team, ecosystem).
  The M-of-N multisig template is available (`chaingo contract multisig`)
  and is recommended for these vaults rather than single-key addresses.
  At genesis, you first allocate to an address, then move into a
  multisig vault created on-chain. A multisig directly at genesis can
  be added later.
- **Precise vesting schedule** (initial cliff? treasury duration?).
- **List of genesis validators** and their respective stakes.
- **Launch date** and communication plan.

See the [roadmap](../ROADMAP.md) for progress on the technical prerequisites.
