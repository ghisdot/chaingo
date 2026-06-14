# Héberger ChainGO 24/24 — site + premier nœud en ligne

Objectif : un serveur qui fait tourner **le nœud ChainGO en continu** et sert **le site
vitrine** (avec stats live) sur ton domaine, en HTTPS.

## Déploiement express (OVH / Ubuntu) — une commande

Sur un VPS Ubuntu fraîchement installé, en root :

```bash
curl -fsSL https://raw.githubusercontent.com/ghisdot/chaingo/main/scripts/deploy-node.sh -o deploy.sh
sudo bash deploy.sh --network testnet --domain node.exemple.com
```

Le script ([scripts/deploy-node.sh](../scripts/deploy-node.sh)) installe Go, compile ChainGO,
crée l'utilisateur + le service systemd (redémarrage auto), configure le pare-feu, et —
si `--domain` est fourni — installe **Caddy avec HTTPS automatique** (Let's Encrypt).
Ce premier nœud `--testnet` génère la **genèse canonique** du testnet (`chaingo-testnet-1`)
et l'expose sur `/v1/genesis` ; c'est le nœud d'amorçage que les autres rejoindront.

> 🔐 Après coup : **sauvegarde `/var/lib/chaingo/validator.seed` (et `faucet.seed`) hors du
> serveur** — c'est l'identité de ton validateur et de ton faucet.

### Faire tourner un nœud local qui REJOINT ce testnet

⚠️ N'utilise PAS `--testnet` en local (il créerait une genèse différente = un autre réseau).
Pour **rejoindre** le testnet du VPS, fais récupérer sa genèse :

```powershell
.\chaingo.exe node start --genesis-url https://node.exemple.com/v1/genesis `
  --peers <ip-du-vps>:9000 --datadir .testnet-local --api 127.0.0.1:8545
```

Ton nœud local télécharge la genèse, se synchronise et reçoit les blocs en gossip (même
derrière une box/NAT : la connexion sortante suffit). Il sert l'API en local sans clé
validateur (nœud complet : sync + API + relais).

### Premières transactions de test

```powershell
# 1. Un wallet (clés ML-DSA-65)
.\chaingo.exe wallet new test1
# 2. Le financer via le faucet du VPS (faucet ouvert sur testnet)
.\chaingo.exe faucet --to test1 --amount 100 --api https://node.exemple.com
# 3. Transférer (via ton nœud local OU directement le VPS)
.\chaingo.exe send --from test1 --to <adresse> --amount 5 --api http://127.0.0.1:8545
# 4. Wallet web : ouvre https://ghisdot.github.io/chaingo/wallet/ et mets l'URL « Nœud »
#    sur https://node.exemple.com
```

## Option 100 % gratuite (recommandée pour démarrer)

Deux pièces, deux hébergeurs gratuits :

1. **Le site + le wallet web → GitHub Pages (gratuit, illimité).** Le workflow
   `.github/workflows/pages.yml` reconstruit le wallet WASM et publie `web/` à chaque
   push sur `main`. Une fois Pages activé (Settings → Pages → Source : *GitHub Actions*),
   le site est en ligne sur `https://ghisdot.github.io/chaingo/`. Le wallet web y demande
   l'URL d'un nœud (champ « Nœud ») — pointe-le vers le nœud ci-dessous.

2. **Le nœud → un hébergeur avec offre gratuite persistante.** Un nœud blockchain doit
   tourner 24/24 avec stockage persistant et un port P2P ouvert — ce que les « free tiers
   qui s'endorment » (Render free, etc.) ne permettent pas. Options réellement viables :

   | Hébergeur | Gratuit ? | Notes |
   |---|---|---|
   | **Oracle Cloud — Always Free** | Oui, permanent | VM ARM 4 cœurs / 24 Go : large pour un nœud. Le meilleur choix gratuit 24/24. |
   | **Fly.io** | Allocation gratuite limitée | Simple avec le `Dockerfile` fourni ; surveille le quota. |
   | **Google Cloud / AWS free tier** | 12 mois | Petite VM e2-micro/t2.micro suffisante pour un nœud léger. |

   ⚠️ Ces hébergeurs demandent **ta propre inscription** (carte bancaire pour vérification
   même sur l'offre gratuite). Je ne peux pas créer ces comptes à ta place.

> 🔴 **Avant de parler de « mainnet » :** la v1 n'a pas encore la finalité BFT
> multi-validateurs, le slashing ni l'audit (Phase 2). Mettre en ligne un **réseau de test
> public (testnet)** est la bonne étape maintenant — même binaire, mais aucune valeur réelle
> promise. Le mainnet viendra après la Phase 2. La suite de ce guide marche pour les deux.

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
  chaingo
```
(L'image lance par défaut un nœud **testnet public**, voir le `CMD` du Dockerfile.)

## 3. Service systemd (option A) — redémarre tout seul, survit aux reboots

`/etc/systemd/system/chaingo.service` :

```ini
[Unit]
Description=ChainGO node
After=network-online.target
Wants=network-online.target

[Service]
User=chaingo
ExecStart=/usr/local/bin/chaingo node start --testnet \
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

> ℹ️ `--testnet` lance un **réseau de test public** (chain_id `chaingo-testnet-1`, faucet
> ouvert, unbonding 24 h, mais l'endpoint qui révèle une seed reste désactivé). C'est ce
> nœud-ci que le wallet web (sur GitHub Pages) interrogera : indique-lui son URL HTTPS dans
> le champ « Nœud ». Le mainnet viendra après la Phase 2.

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
