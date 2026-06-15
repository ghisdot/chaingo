# Déployer ChainGO 24/24 — alternatives au guide principal

Ce document complète [TESTNET-DEPLOY.md](TESTNET-DEPLOY.md) qui détaille le
déploiement standard sur un VPS Debian. Sont rassemblées ici les
**alternatives** : déploiement express via script, Docker, hébergeurs avec
offre gratuite, ajout de nœuds secondaires.

---

## Déploiement express — script tout-en-un

Pour ceux qui préfèrent une seule commande à un guide pas-à-pas, le dépôt
fournit `scripts/deploy-node.sh` :

```bash
curl -fsSL https://raw.githubusercontent.com/ghisdot/chaingo/main/scripts/deploy-node.sh -o deploy.sh
sudo bash deploy.sh --network testnet --domain <votre-domaine.example.org>
```

Ce script installe Go, compile ChainGO, crée l'utilisateur système et le
service systemd, configure UFW, et — si `--domain` est fourni — installe
Caddy avec HTTPS automatique (Let's Encrypt).

Options principales :

| Option | Description |
|---|---|
| `--network testnet \| mainnet` | Réseau à lancer (par défaut `testnet`) |
| `--domain <fqdn>` | Active le reverse proxy HTTPS automatique sur ce domaine |
| `--peers <host:port,…>` | Liste de pairs à rejoindre |

> ℹ️ Le mode `--network testnet` lancé sans `--peers` **crée une nouvelle
> genèse locale** (chain_id `chaingo-testnet-1`) et devient un nœud
> d'amorçage. Pour rejoindre le testnet public ChainGO, utiliser plutôt la
> procédure A de [TESTNET-DEPLOY.md](TESTNET-DEPLOY.md#a-rejoindre-le-testnet-public-chaingo).

---

## Déploiement Docker

```bash
git clone https://github.com/ghisdot/chaingo && cd chaingo
docker build -t chaingo .
docker run -d --name chaingo --restart unless-stopped \
  -p 127.0.0.1:8545:8545 -p 9000:9000 \
  -v chaingo-data:/data \
  chaingo
```

Par défaut, l'image lance un nœud testnet public (voir `CMD` du Dockerfile).
Pour rejoindre une chaîne existante, surcharger la commande :

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

## Hébergeurs

Pré-requis pour un nœud public : VPS persistant avec un port P2P ouvrable
(les « free tiers qui s'endorment » type Render free ne conviennent pas).

| Hébergeur | Coût | Notes |
|---|---|---|
| Hetzner Cloud (CX22 et plus) | ~5 €/mois | Bon rapport qualité/prix, large bande passante |
| OVH VPS / Public Cloud | ~5-10 €/mois | Présence en France, IPv4 incluse |
| Scaleway DEV1-M | ~7 €/mois | Présence en Europe |
| Oracle Cloud — Always Free | Gratuit, permanent | VM ARM (4 cœurs / 24 Go RAM). Inscription avec carte de vérification. |
| Fly.io | Allocation gratuite limitée | Compatible avec le `Dockerfile` fourni ; vérifier le quota mensuel. |

L'inscription chez ces hébergeurs (même sur l'offre gratuite) est à la
charge de l'opérateur — une vérification d'identité ou de moyen de paiement
peut être demandée.

---

## Ajouter un nœud secondaire (résilience / décentralisation)

Sur un second serveur, pointer vers le premier nœud pour rejoindre la même
chaîne :

```bash
chaingo node start \
  --datadir /var/lib/chaingo \
  --genesis-url https://node1.example.org/v1/genesis \
  --peers <ip-serveur-1>:9000 \
  --api 127.0.0.1:8545 --p2p :9000
```

Le nouveau nœud télécharge la genèse, se synchronise par lots et relaie
ensuite les blocs. Avec `--validator-seed <fichier>`, il peut proposer des
blocs (à condition d'avoir staké au moins `min_validator_stake` CGO).

---

## Sauvegardes critiques

Le datadir `/var/lib/chaingo/` contient :

| Fichier | Contenu | Si perdu… |
|---|---|---|
| `validator.seed` | clé du validateur | impossible de signer de nouveaux blocs avec cette identité |
| `faucet.seed` | clé du faucet (dev/testnet uniquement) | faucet inutilisable |
| `genesis.json` | document de genèse | resynchronisable depuis n'importe quel pair |
| `chain.db` | base de données des blocs et de l'état | resynchronisable depuis les pairs |

Seules les `*.seed` sont irrécupérables si perdues. Procédure de sauvegarde
chiffrée en section *Sauvegarde des seeds* de [TESTNET-DEPLOY.md](TESTNET-DEPLOY.md#sauvegarde-des-seeds-impératif).
