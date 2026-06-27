# Becoming a validator or delegator

ChainGO offers two ways to secure the network and earn
rewards (~3 %/year on the total stake).

---

## Option 1 — Validator (≥ 10,000 CGO + a 24/7 node)

**Prerequisites**: a ChainGO node running permanently. See
[TESTNET-DEPLOY.en.md](TESTNET-DEPLOY.en.md) for the installation procedure.
The validator's key is the seed pointed to by `--validator-seed` (or
`validator.seed` in the datadir in `--dev` / `--testnet` mode).

### Activating the validator role

From the wallet corresponding to that seed:

```bash
chaingo stake --from <wallet> --amount 10000
```

From then on, consensus draws the validator at random for each block,
in proportion to its weight (`stake + received delegations`). For each
block proposed: emission reward + transaction tips.

### Risks

#### Offline: jail + light slash

The **fallback rounds** hand control to another validator when the
elected proposer does not respond. Each missed round increments a counter. At
the threshold (`downtime_jail_threshold`), the validator is **jailed**:

- excluded from the proposer draw,
- excluded from the finality computation (its power becomes 0),
- **slashed by 0.1 %** (`slash_downtime_bps`), burned on both stake **and**
  received delegations.

Exit: `chaingo unjail --from <wallet>` after `jail_seconds` has elapsed.
A node that produces regularly sees its counter reset to 0 — never
being jailed is easy with good uptime.

#### Double-signing: immediate 5 % slash

If a node precommits two different blocks at the same height (equivocation —
the typical scenario: two instances running the same seed in
parallel), the proof is included on-chain by any other honest
node. **5 % of the validator's own stake AND of the delegations are burned**
(`slash_double_sign_bps`), idempotently.

> ⚠️ **Never run two nodes with the same `validator.seed`.**
> For high availability, use a single active node and a passive
> backup node whose seed stays **off** except for a manual switchover.

### Exiting the validator role

```bash
chaingo unstake --from <wallet> --amount 10000
```

The funds go into **unbonding** (21 days on mainnet, 24 h on testnet),
then automatically become liquid again. If the entire stake is withdrawn,
the received delegations are automatically released (also in unbonding,
the delegators recover their funds).

---

## Option 2 — Delegator (from 1 CGO, no node to run)

Holders who do not want to operate a node can **delegate** their
weight to an existing validator and earn their share of its rewards, pro
rata, for each block it proposes, minus its commission (10 %).

```bash
# List active validators
curl https://node.chaingo.org/v1/validators

# Delegate
chaingo delegate --from <wallet> --to cg<validator-address> --amount 50

# Check rewards (credited to the balance, block after block)
chaingo balance <wallet>

# Recover your funds (unbonding 21 d on mainnet, 24 h on testnet)
chaingo undelegate --from <wallet> --to cg<validator-address> --amount 50
```

### Guarantees and risks

- **Delegated CGO never leaves the delegator's account**: the
  delegation is an accounting weight, the validator cannot spend it.
- **Slashing also applies to delegations.** If the chosen validator
  is slashed for double-signing, the delegators lose 5 % of their
  delegation (burned). Pick a serious validator: a single active
  node, good uptime, verifiable public identity.
- If the validator leaves the active set (full unstake or prolonged jail),
  the delegated funds go into unbonding and are recovered after the delay.

---

## Economic parameters

All parameters are visible in real time via the node's
`GET /v1/fees` endpoint — example on the public testnet:

```bash
curl https://node.chaingo.org/v1/fees
```

| Parameter | Mainnet (planned) | Testnet |
|---|---|---|
| Minimum validator stake | 10,000 CGO | 10,000 CGO |
| Minimum delegation | 1 CGO | 1 CGO |
| Validator commission on delegator rewards | 10 % | 10 % |
| Annual emission on the stake | ~3 % | ~3 % |
| Unbonding | 21 days | 24 hours |
| Double-signing slash | 5 % | 5 % |
| Downtime slash | 0.1 % | 0.1 % |
