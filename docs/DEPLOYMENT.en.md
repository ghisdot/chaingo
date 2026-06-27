# Deploying ChainGO 24/7 — alternatives to the main guide

This document complements [TESTNET-DEPLOY.en.md](TESTNET-DEPLOY.en.md), which details the
standard deployment on a Debian VPS. Gathered here are the
**alternatives**: express deployment via script, Docker, hosting providers with
a free tier, and adding secondary nodes.

---

## Express deployment — all-in-one script

For those who prefer a single command over a step-by-step guide, the repo
provides `scripts/deploy-node.sh`:

```bash
curl -fsSL https://raw.githubusercontent.com/ghisdot/chaingo/main/scripts/deploy-node.sh -o deploy.sh
sudo bash deploy.sh --network testnet --domain <your-domain.example.org>
```

This script installs Go, compiles ChainGO, creates the system user and the
systemd service, configures UFW, and — if `--domain` is provided — installs
Caddy with automatic HTTPS (Let's Encrypt).

Main options:

| Option | Description |
|---|---|
| `--network testnet \| mainnet` | Network to launch (default `testnet`) |
| `--domain <fqdn>` | Enables the automatic HTTPS reverse proxy on this domain |
| `--peers <host:port,…>` | List of peers to join |

> ℹ️ Mode `--network testnet` launched without `--peers` **creates a new
> local genesis** (chain_id `chaingo-testnet-1`) and becomes a bootstrap
> node. To join the public ChainGO testnet, use instead
> procedure A in [TESTNET-DEPLOY.en.md](TESTNET-DEPLOY.en.md#a-rejoindre-le-testnet-public-chaingo).

---

## Docker deployment

```bash
git clone https://github.com/ghisdot/chaingo && cd chaingo
docker build -t chaingo .
docker run -d --name chaingo --restart unless-stopped \
  -p 127.0.0.1:8545:8545 -p 9000:9000 \
  -v chaingo-data:/data \
  chaingo
```

By default, the image launches a public testnet node (see the Dockerfile `CMD`).
To join an existing chain, override the command:

```bash
docker run -d --name chaingo --restart unless-stopped \
  -p 127.0.0.1:8545:8545 -p 9000:9000 \
  -v chaingo-data:/data \
  chaingo node start \
    --genesis-url https://node.chaingo.org/v1/genesis \
    --peers node.chaingo.org:9000 \
    --datadir /data --api :8545 --p2p :9000 \
    --web /web
```

---

## Hosting providers

Requirements for a public node: a persistent VPS with an openable P2P port
(the "free tiers that go to sleep" like Render free are not suitable).

| Provider | Cost | Notes |
|---|---|---|
| Hetzner Cloud (CX22 and up) | ~€5/month | Good value, ample bandwidth |
| OVH VPS / Public Cloud | ~€5-10/month | Presence in France, IPv4 included |
| Scaleway DEV1-M | ~€7/month | Presence in Europe |
| Oracle Cloud — Always Free | Free, permanent | ARM VM (4 cores / 24 GB RAM). Sign-up with a verification card. |
| Fly.io | Limited free allocation | Compatible with the provided `Dockerfile`; check the monthly quota. |

Signing up with these providers (even on the free tier) is the
operator's responsibility — an identity or payment-method verification
may be requested.

---

## Adding a secondary node (resilience / decentralization)

On a second server, point to the first node to join the same
chain:

```bash
chaingo node start \
  --datadir /var/lib/chaingo \
  --genesis-url https://node1.example.org/v1/genesis \
  --peers <server-1-ip>:9000 \
  --api 127.0.0.1:8545 --p2p :9000
```

The new node downloads the genesis, syncs in batches, and then relays
blocks. With `--validator-seed <file>`, it can propose blocks
(provided it has staked at least `min_validator_stake` CGO).

---

## Critical backups

The `/var/lib/chaingo/` datadir contains:

| File | Contents | If lost… |
|---|---|---|
| `validator.seed` | validator key | impossible to sign new blocks with this identity |
| `faucet.seed` | faucet key (dev/testnet only) | faucet unusable |
| `genesis.json` | genesis document | re-syncable from any peer |
| `chain.db` | block and state database | re-syncable from peers |

Only the `*.seed` files are unrecoverable if lost. Encrypted backup
procedure in the *Seed backup* section of [TESTNET-DEPLOY.en.md](TESTNET-DEPLOY.en.md#sauvegarde-des-seeds-impératif).
