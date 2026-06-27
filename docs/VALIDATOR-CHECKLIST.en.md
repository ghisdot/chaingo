# Checklist — launching independent validators (testnet)

Goal: move from "everything runs on the maintainer's machines" to **several
independent validators** — this is the **real decentralization milestone** before
mainnet. This checklist is operational; for the details (systemd, HTTPS,
backups) see [TESTNET-DEPLOY.en.md](TESTNET-DEPLOY.en.md) and
[VALIDATOR.en.md](VALIDATOR.en.md).

---

## Project side (you, once)

- [ ] **Stable testnet**: a public, reachable bootstrap node (seed/bootnode)
      (fixed IP/DNS, open P2P port), height advancing.
- [ ] **Genesis frozen and published**: the testnet's `genesis.json` downloadable
      (stable URL) — new nodes retrieve it via `--genesis-url`.
- [ ] **Genesis fingerprint** published (operators verify they are joining
      the same network).
- [ ] **Faucet** online (validator candidates need test CGO
      to stake) — aim wide: min stake = **10,000 CGO**.
- [ ] **Onboarding doc** (this file + VALIDATOR.en.md) linked from the site.
- [ ] **Support channel** (Discord/Telegram) for operators.
- [ ] **Bootnode list** documented (at least 1, ideally 2-3 distributed).

## Operator side (each independent validator)

### 1. Prepare the machine
- [ ] Dedicated Linux VPS/server, clock **synchronized (NTP)** — critical for a
      500 ms block network.
- [ ] Go installed, binary compiled: `go build -o chaingo ./cmd/chaingo`.
- [ ] **P2P** port open in the firewall (e.g. 9000); API bound locally
      (`127.0.0.1`) or over HTTPS via reverse proxy (cf. TESTNET-DEPLOY.en.md §A.5).

### 2. Join the testnet
- [ ] Start by joining the bootnodes:
      ```bash
      chaingo node start --testnet \
        --datadir /var/lib/chaingo \
        --genesis-url https://chaingo.org/testnet/genesis.json \
        --peers <bootnode1_host:port>,<bootnode2_host:port> \
        --api 127.0.0.1:8545 --p2p :9000
      ```
- [ ] **Verify sync**: local height == network height
      (`/v1/status`), and **same genesis fingerprint** as the one published.
- [ ] Run as a **systemd service** (auto-restart) — cf. TESTNET-DEPLOY.en.md §A.3.

### 3. Become a validator
- [ ] Create the operator's wallet: `chaingo wallet new <name>`.
- [ ] Obtain ≥ **10,000 CGO** of test funds (faucet).
- [ ] **Back up the seed** offline, encrypted (loss = loss of the validator).
- [ ] Stake: `chaingo stake --from <name> --amount 10000`.
- [ ] (Optional) Publish a profile: `chaingo validator-profile …` (name/site).
- [ ] Verify the entry in the **active set** (dashboard or `/v1/validators`).

### 4. Operation
- [ ] **Monitor block production**: your validator must propose when it is
      its turn (otherwise jail for inactivity → slashing 0.1 %).
- [ ] **Uptime**: an offline node misses its proposer slots.
- [ ] **Never run two nodes with the SAME validator seed** → double-sign
      → slashing 5 %. One seed = one machine.
- [ ] Monitor logs/disk space; plan a coordinated **upgrade** of the binary
      (protocol version governance).

---

## Milestone success criteria

- [ ] **≥ 4 validators** held by **≥ 4 distinct entities** (key to BFT
      safety: no entity > 1/3 of the stake).
- [ ] The network **keeps finalizing** when the maintainer's node stops
      (real liveness test).
- [ ] A test **reorg/partition** resolves cleanly between independent
      operators.
- [ ] Stake **distributed** (not 90 % on a single entity).

> When these criteria are met, the "decentralization" box on the roadmap turns
> green — it is a hard prerequisite for mainnet, independent of the code.
