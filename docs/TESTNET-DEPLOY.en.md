# Running a ChainGO node

Step-by-step guide for operators who want to contribute to the ChainGO network by
running a node. Two paths:

- **A.** Join the ChainGO **public testnet** (`chaingo-testnet-1`) — the
  fastest path, ideal for testing, validating, or preparing to become a validator.
- **B.** Bootstrap a **new network** (private testnet or future mainnet) —
  for teams that want to launch their own instance.

> 💡 **Just want to use ChainGO?**
> No server needed. The web wallet runs in your browser:
> [`https://chaingo.org/wallet/`](https://chaingo.org/wallet/) (post-quantum
> keys generated locally, never sent). To explore the chain live:
> [`https://chaingo.org/explorer/`](https://chaingo.org/explorer/).

---

## Prerequisites

| Component | Recommendation |
|---|---|
| OS | Debian 12 (Bookworm) or a recent Ubuntu LTS |
| CPU / RAM | 2 vCPU / 4 GB RAM (more than enough for a testnet node) |
| Disk | 20 GB SSD (the bbolt database grows slowly) |
| Network | Public IPv4, ports `22 / 80 / 443 / 9000` openable |
| Access | SSH as root or via `sudo` |
| Optional | A domain name (recommended to expose the public API over HTTPS) |

---

## A. Join the ChainGO public testnet

The reference network is `chaingo-testnet-1`, whose public bootstrap node
exposes its API at `https://node.chaingo.org` and its P2P port at
`node.chaingo.org:9000`.

### A.1 Install Go and compile the binary

```bash
# On the server, as root
apt update && apt install -y curl git ufw
curl -fsSL https://go.dev/dl/go1.26.4.linux-amd64.tar.gz -o /tmp/go.tgz
rm -rf /usr/local/go
tar -C /usr/local -xzf /tmp/go.tgz
echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/go.sh
source /etc/profile.d/go.sh

git clone https://github.com/ghisdot/chaingo /opt/chaingo
cd /opt/chaingo
go build -trimpath -ldflags="-s -w" -o /usr/local/bin/chaingo ./cmd/chaingo
```

### A.2 Create a dedicated system user

```bash
useradd -r -m -d /var/lib/chaingo -s /usr/sbin/nologin chaingo
```

### A.3 systemd service that joins the public testnet

```bash
cat > /etc/systemd/system/chaingo-testnet.service <<'EOF'
[Unit]
Description=ChainGO node (testnet)
After=network-online.target
Wants=network-online.target

[Service]
User=chaingo
ExecStart=/usr/local/bin/chaingo node start \
  --genesis-url https://node.chaingo.org/v1/genesis \
  --peers node.chaingo.org:9000 \
  --datadir /var/lib/chaingo \
  --api 127.0.0.1:8545 \
  --p2p :9000
Restart=always
RestartSec=3
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now chaingo-testnet
```

The node downloads the genesis from the public seed, syncs in batches, and then
receives blocks via P2P gossip.

### A.4 Verify that the node is synced

```bash
systemctl status chaingo-testnet --no-pager
journalctl -u chaingo-testnet -n 30 --no-pager

# Local height vs network height:
curl -s http://127.0.0.1:8545/v1/status | grep -oE '"height":[0-9]+'
curl -s https://node.chaingo.org/v1/status | grep -oE '"height":[0-9]+'
```

When both heights are identical (within ±1), the node is synced. It can then be
used read-only (without a validator key) or — if the operator wants to become a
validator — they will need to stake from a funded wallet (see
[VALIDATOR.en.md](VALIDATOR.en.md)).

### A.5 (Optional) Expose the API over HTTPS via a domain

If the operator has a domain name (e.g. `node.example.org`), Caddy provides
HTTPS automatically via Let's Encrypt.

```bash
# DNS: create an A record pointing <your-subdomain> → IP of the VPS

# Firewall
ufw allow 22,80,443,9000/tcp
ufw --force enable

# Caddy
apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
  | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
  > /etc/apt/sources.list.d/caddy-stable.list
apt update && apt install -y caddy

cat > /etc/caddy/Caddyfile <<'EOF'
<your-subdomain.example.org> {
    reverse_proxy 127.0.0.1:8545
    encode gzip
}
EOF
systemctl reload caddy
```

> Replace `<your-subdomain.example.org>` with the real FQDN. The TLS certificate
> is obtained in 30-60 s on first startup.

---

## B. Bootstrap a new network (private testnet or mainnet)

For teams that want to **create their own chain** rather than join the public
testnet. The procedure is identical for a private testnet or to prepare the
future mainnet — only the genesis document changes.

### B.1 Generate the genesis validator key

```bash
chaingo keygen --out /var/lib/chaingo/validator.seed
chmod 600 /var/lib/chaingo/validator.seed
```

> ⚠️ This `validator.seed` is the validator's identity. **Back it up immediately
> and offline** (see the *Backing up seeds* section below).

### B.2 Prepare the genesis document

```bash
chaingo genesis template \
  --chain-id <your-chain-id> \
  --out /etc/chaingo/genesis.json \
  --seed-out /var/lib/chaingo/validator.seed

# Edit /etc/chaingo/genesis.json (alloc / stakes / vesting / params)
# according to the planned distribution. See docs/MAINNET.en.md for the ceremony.

chaingo genesis validate /etc/chaingo/genesis.json
```

`validate` prints a deterministic fingerprint (`block hash` + `state root`).
**All operators participating in the bootstrap must obtain the same
fingerprint** on the same genesis — this is the network's start-up guarantee.

### B.3 systemd service that bootstraps the new network

```bash
cat > /etc/systemd/system/chaingo.service <<'EOF'
[Unit]
Description=ChainGO bootstrap node
After=network-online.target

[Service]
User=chaingo
ExecStart=/usr/local/bin/chaingo node start \
  --genesis /etc/chaingo/genesis.json \
  --validator-seed /var/lib/chaingo/validator.seed \
  --datadir /var/lib/chaingo \
  --api 127.0.0.1:8545 \
  --p2p :9000
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now chaingo
```

The other initial validators and follower nodes then join via
`--genesis-url https://<first-node>/v1/genesis --peers <first-node>:9000`.

---

## Backing up seeds (mandatory)

The datadir `/var/lib/chaingo/` contains `validator.seed` (and, in `--dev` or
`--testnet` mode, `faucet.seed`). Without these files, the validator can no
longer sign blocks with its identity.

```bash
# On the server — encrypt with a strong passphrase (noted offline)
mkdir -p ~/.gnupg && echo "allow-loopback-pinentry" >> ~/.gnupg/gpg-agent.conf
gpgconf --kill gpg-agent
tar czf - /var/lib/chaingo/*.seed \
  | gpg --pinentry-mode loopback -c \
  > /tmp/chaingo-seeds.tar.gz.gpg
```

Then, from a trusted machine, download the encrypted archive and delete the
temporary copy from the server:

```bash
scp <user>@<server>:/tmp/chaingo-seeds.tar.gz.gpg ~/backups/
ssh <user>@<server> 'rm /tmp/chaingo-seeds.tar.gz.gpg'
```

Restoring on a new server:

```bash
gpg -d chaingo-seeds.tar.gz.gpg | sudo tar xzf - -C /
sudo chown chaingo:chaingo /var/lib/chaingo/*.seed
sudo chmod 600 /var/lib/chaingo/*.seed
```

---

## Maintenance

### Update the binary

```bash
cd /opt/chaingo
git pull
/usr/local/go/bin/go build -trimpath -ldflags="-s -w" -o /usr/local/bin/chaingo ./cmd/chaingo
systemctl restart chaingo-testnet   # or chaingo depending on the service
```

State is persisted in `/var/lib/chaingo/`; the chain resumes exactly where it
stopped.

### Follow the logs

```bash
journalctl -u chaingo-testnet -f
```

### Measure disk usage

```bash
du -sh /var/lib/chaingo
```

### Reload after editing the Caddyfile

```bash
systemctl reload caddy
```

---

## Server security

Minimal recommendations for a public node:

- **Non-root account + SSH key**: create a dedicated user, disable password
  authentication and `root` login by password
  (`PermitRootLogin prohibit-password`, `PasswordAuthentication no` in
  `/etc/ssh/sshd_config`).
- **UFW firewall**: only ports `22 / 80 / 443 / 9000` open.
- **OS updates**: `unattended-upgrades` for security patches.
- **Regular backup** of the encrypted seed archive.

---

## Troubleshooting

| Symptom | Diagnosis |
|---|---|
| The service won't start | `journalctl -u chaingo-testnet -n 100` — often a port already in use (`ss -tlnp | grep 8545`) |
| Caddy won't start | `caddy validate --config /etc/caddy/Caddyfile`. HTTPS blocked: check that port 80 is open (Let's Encrypt challenge). |
| The API returns HTTP 502 | Caddy is running but the node is not — `systemctl restart chaingo-testnet`. |
| Web wallet stuck on "offline" with a duplicated CORS error | An external layer (proxy/firewall) is adding an `Access-Control-Allow-Origin`. The ChainGO node already sends it — do not re-add it. |
| `gpg: Inappropriate ioctl for device` | Pinentry without a TTY. Follow the "loopback" procedure in the *Backing up seeds* section. |

---

## Going further

- [VALIDATOR.en.md](VALIDATOR.en.md) — becoming a validator, staking, delegation, slashing
- [MAINNET.en.md](MAINNET.en.md) — prerequisites and genesis ceremony for a mainnet
- [API.en.md](API.en.md) — full REST API reference
- [CONTRIBUTING.md](../CONTRIBUTING.md) — contributing to the source code
