# Héberger ChainGO 24/24 — site + premier nœud en ligne

Objectif : un serveur qui fait tourner **le nœud ChainGO en continu** et sert **le site
vitrine** (avec stats live) sur ton domaine, en HTTPS.

## 1. Le serveur

Un petit VPS suffit largement (le nœud est léger) :

| Spec minimum | Recommandé | Exemples (~5-10 €/mois) |
|---|---|---|
| 2 vCPU, 4 Go RAM, 40 Go SSD | 4 vCPU, 8 Go RAM, 80 Go SSD | Hetzner CX22/CX32, OVH VPS, Scaleway DEV1-M |

Prends **Ubuntu 24.04 LTS**. Ouvre les ports : `22` (SSH), `80/443` (web), `9000` (P2P).
Le port `8545` (API) reste **interne** — c'est le reverse proxy qui l'expose en HTTPS.

```bash
sudo ufw allow 22,80,443,9000/tcp && sudo ufw enable
```

## 2. Installer le nœud

### Option A — binaire (le plus simple)

```bash
# Depuis la page GitHub Releases (générée automatiquement à chaque tag v*) :
wget https://github.com/<ton-compte>/chaingo/releases/latest/download/chaingo-linux-amd64
sudo install chaingo-linux-amd64 /usr/local/bin/chaingo

# Le site est dans le dépôt :
git clone https://github.com/<ton-compte>/chaingo /opt/chaingo
```

### Option B — Docker

```bash
git clone https://github.com/<ton-compte>/chaingo && cd chaingo
docker build -t chaingo .
docker run -d --name chaingo --restart unless-stopped \
  -p 127.0.0.1:8545:8545 -p 9000:9000 -v chaingo-data:/data \
  chaingo node start --dev --datadir /data --web /web
```

## 3. Service systemd (option A) — redémarre tout seul, survit aux reboots

`/etc/systemd/system/chaingo.service` :

```ini
[Unit]
Description=ChainGO node
After=network-online.target
Wants=network-online.target

[Service]
User=chaingo
ExecStart=/usr/local/bin/chaingo node start --dev \
  --datadir /var/lib/chaingo \
  --api 127.0.0.1:8545 --p2p :9000 \
  --web /opt/chaingo/web
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

```bash
sudo useradd -r -m -d /var/lib/chaingo chaingo
sudo systemctl enable --now chaingo
journalctl -u chaingo -f        # suivre les logs
```

> ⚠️ `--dev` lance la chaîne de développement (faucet ouvert). Pour le futur testnet
> public on remplacera par `--genesis testnet.json --validator-seed …` — même service.

## 4. Domaine + HTTPS : Caddy (2 lignes, certificat automatique)

```bash
sudo apt install caddy
```

`/etc/caddy/Caddyfile` :

```
chaingo.example.com {
    reverse_proxy 127.0.0.1:8545
}
```

```bash
sudo systemctl reload caddy
```

C'est tout : `https://chaingo.example.com` sert le site (stats live incluses) et l'API
`https://chaingo.example.com/v1/...` — certificat TLS renouvelé automatiquement.

## 5. Mettre à jour le site et le nœud

- **Site seul** : le nœud sert `web/` directement depuis le disque →
  `cd /opt/chaingo && git pull` et c'est en ligne au prochain rafraîchissement. Zéro coupure.
- **Nœud** : `git pull`, retélécharger/recompiler le binaire, `sudo systemctl restart chaingo`
  (la chaîne reprend où elle s'était arrêtée — état persisté). En Docker :
  `docker build -t chaingo . && docker restart chaingo`.

## 6. Sauvegardes — CRITIQUE

Dans le datadir (`/var/lib/chaingo`) :

| Fichier | Contenu | Si perdu… |
|---|---|---|
| `validator.seed` | la clé de TON validateur | tu ne produis plus de blocs (et stake bloqué) |
| `faucet.seed` | la clé du faucet devnet | faucet mort |
| `genesis.json` | les règles de la chaîne | les autres nœuds l'ont aussi |
| `chain.db` | blocs + état | resynchronisable depuis les pairs |

```bash
# Sauvegarde chiffrée des seeds, à copier HORS du serveur :
tar czf - /var/lib/chaingo/*.seed | gpg -c > chaingo-seeds-backup.tar.gz.gpg
```

## 7. Ajouter d'autres nœuds (résilience)

Sur un second serveur, il suffit de pointer vers le premier :

```bash
chaingo node start --datadir /var/lib/chaingo \
  --genesis-url https://chaingo.example.com/v1/genesis \
  --peers <ip-serveur-1>:9000 --api 127.0.0.1:8545 --p2p :9000
```

Il télécharge la genèse, se synchronise et relaie — un nœud complet de plus sur le réseau.
