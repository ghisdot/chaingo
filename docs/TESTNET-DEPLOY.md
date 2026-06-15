# Déployer ChainGO sur ton VPS Debian (chaingo.org) — guide complet

Ce guide te fait passer d'un **VPS Debian vide** à :

- **`https://chaingo.org`** → ton site officiel (vitrine FR/EN + wallet web post-quantique), 100 % chez toi
- **`https://node.chaingo.org`** → l'API publique de **ton nœud testnet**, HTTPS auto
- **Plus tard `https://mainnet.chaingo.org`** → l'API d'un nœud mainnet, **sur le même VPS** ou un autre

Tout passe par **Caddy** en frontal (reverse proxy + Let's Encrypt), avec des nœuds **séparés** (process, datadir, ports) pour pouvoir faire cohabiter testnet et mainnet plus tard.

> 📌 GitHub ne sert que de **dépôt de code** dans ce setup. Le site et le wallet
> sont servis par TON serveur, pas par GitHub Pages.

> ⏱️ Compte 20-30 min la première fois. Tout est en commandes copy-paste.

---

## 0. Pré-requis (à avoir AVANT de commencer)

- Un VPS **Debian 12 (Bookworm)**, accès SSH (en root ou user avec sudo)
- Le domaine `chaingo.org` chez ton registrar
- **DNS configuré** (étape 1 ci-dessous, si ce n'est pas déjà fait)
- ~2 Go de RAM libres, ~5 Go d'espace disque

Vérifier qu'on est bien sur Debian :

```bash
cat /etc/os-release | head -2
```

---

## 1. DNS — pointer les domaines sur le VPS

Dans la console DNS de `chaingo.org` (OVH ou autre registrar), tu dois avoir
**au moins ces 3 enregistrements** :

| Type | Sous-domaine | Cible (= IP du VPS) | TTL |
|------|--------------|---------------------|-----|
| `A` | `@` (= chaingo.org) | `IPv4 du VPS` | 300 |
| `A` | `www` | `IPv4 du VPS` | 300 |
| `A` | `node` | `IPv4 du VPS` | 300 |

(Garde `node` pour le testnet ; on ajoutera `mainnet` plus tard pour le mainnet.)

**Vérification depuis ta machine** (pas le VPS) :

```bash
dig +short chaingo.org
dig +short www.chaingo.org
dig +short node.chaingo.org
# Les trois doivent renvoyer EXACTEMENT l'IP du VPS.
```

Si ça ne renvoie rien ou la mauvaise IP, attends quelques minutes (propagation
DNS). Ne passe pas à la suite tant que les 3 résolvent vers ton VPS.

---

## 2. Premier SSH et hardening minimal

```bash
ssh root@<ip-du-vps>
# (ou ssh debian@<ip-du-vps> si OVH t'a donné un user "debian")
```

### 2.1 Mise à jour + outils de base

```bash
apt update && apt full-upgrade -y
apt install -y curl git ufw gnupg debian-keyring debian-archive-keyring apt-transport-https
```

### 2.2 Créer un user dédié avec sudo

```bash
adduser ghislain                       # choisis un mot de passe FORT
usermod -aG sudo ghislain
```

### 2.3 Activer la connexion par clé SSH

**Sur ta machine locale** (pas le VPS), dans un autre terminal :

```bash
ssh-copy-id ghislain@chaingo.org
ssh ghislain@chaingo.org               # vérifie que ça passe sans mot de passe
```

### 2.4 Durcir SSH (sur le VPS)

**Une fois la connexion par clé confirmée** (très important : ne te bloque pas dehors) :

```bash
sudo sed -i 's/^#*PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
sudo sed -i 's/^#*PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
sudo systemctl reload ssh
```

Garde une session SSH ouverte pendant que tu testes la nouvelle config dans une
seconde session — si la 2e ne passe pas, tu corriges depuis la 1re.

---

## 3. Installer Go (le compilateur)

```bash
curl -fsSL https://go.dev/dl/go1.26.4.linux-amd64.tar.gz -o /tmp/go.tgz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf /tmp/go.tgz
echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh
source /etc/profile.d/go.sh
go version            # doit afficher : go version go1.26.4 linux/amd64
```

---

## 4. Cloner ChainGO et compiler

```bash
# User système dédié pour le nœud (pas root, pas ghislain)
sudo useradd -r -m -d /var/lib/chaingo -s /usr/sbin/nologin chaingo

# Cloner les sources dans /opt/chaingo
sudo git clone https://github.com/ghisdot/chaingo /opt/chaingo
cd /opt/chaingo

# Compiler le binaire (~1 min)
sudo /usr/local/go/bin/go build -trimpath -ldflags="-s -w" -o /usr/local/bin/chaingo ./cmd/chaingo

# Sanity check
chaingo help | head -3
```

---

## 5. Le service systemd du nœud testnet

```bash
sudo tee /etc/systemd/system/chaingo-testnet.service >/dev/null <<'EOF'
[Unit]
Description=ChainGO testnet node
After=network-online.target
Wants=network-online.target

[Service]
User=chaingo
ExecStart=/usr/local/bin/chaingo node start --testnet \
  --datadir /var/lib/chaingo \
  --api 127.0.0.1:8545 \
  --p2p :9000
Restart=always
RestartSec=3
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now chaingo-testnet
```

Vérifier que ça tourne :

```bash
sudo systemctl status chaingo-testnet --no-pager
sudo journalctl -u chaingo-testnet -n 30 --no-pager
```

Tu devrais voir des lignes du style :

```
[node] chain chaingo-testnet-1 initialized, genesis abc123…
[node] validator: cg...
[node] faucet:    cg... (1,000,000,000 CGO)
[consensus] block #1 produced: 0 tx(s), hash ... 
```

**Note bien l'adresse du validateur** (ligne `validator:`) — c'est l'identité de ton nœud.

Test rapide en local sur le VPS :

```bash
curl -s http://127.0.0.1:8545/v1/status | head -c 200
```

---

## 6. Pare-feu (UFW)

```bash
sudo ufw allow 22/tcp        # SSH
sudo ufw allow 80/tcp        # HTTP (challenge Let's Encrypt)
sudo ufw allow 443/tcp       # HTTPS (site + API)
sudo ufw allow 9000/tcp      # P2P testnet
sudo ufw --force enable
sudo ufw status numbered
```

---

## 7. Installer Caddy (reverse proxy + HTTPS automatique)

```bash
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null
sudo apt update
sudo apt install -y caddy
```

---

## 8. Configurer Caddy : site + API

C'est ici que la magie opère : un seul `Caddyfile` gère le site statique
ET le reverse proxy vers le nœud, avec HTTPS auto pour les deux.

```bash
sudo tee /etc/caddy/Caddyfile >/dev/null <<'EOF'
# --- Site officiel : vitrine FR/EN + wallet web ---
chaingo.org, www.chaingo.org {
    root * /opt/chaingo/web
    file_server
    encode gzip

    # WASM doit être servi avec le bon Content-Type pour le wallet
    @wasm path *.wasm
    header @wasm Content-Type application/wasm

    # En-têtes de sécurité
    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains"
        X-Content-Type-Options "nosniff"
        X-Frame-Options "DENY"
        Referrer-Policy "no-referrer-when-downgrade"
    }
}

# --- API publique du nœud testnet ---
node.chaingo.org {
    reverse_proxy 127.0.0.1:8545
    encode gzip

    # CORS ouvert (le nœud renvoie déjà les bons headers, c'est une ceinture)
    header Access-Control-Allow-Origin "*"
}
EOF

sudo systemctl reload caddy
sudo journalctl -u caddy -n 20 --no-pager
```

Caddy obtient les certificats Let's Encrypt automatiquement (compte 30-60 sec
au premier démarrage). Tu peux suivre :

```bash
sudo journalctl -u caddy -f
# Ctrl+C quand tu vois "certificate obtained successfully" pour les 3 domaines
```

---

## 9. Vérifications

Depuis n'importe où :

```bash
# Le site
curl -sI https://chaingo.org | head -3
# HTTP/2 200

# Le wallet (statique)
curl -sI https://chaingo.org/wallet/ | head -3

# L'API du nœud testnet
curl -s https://node.chaingo.org/v1/status
# {"chain_id":"chaingo-testnet-1","height":42,...}

curl -s https://node.chaingo.org/v1/fees
```

Ouvre dans ton navigateur :
- **https://chaingo.org** → ton site
- **https://chaingo.org/wallet/** → le wallet web (il pointe **automatiquement** sur `https://node.chaingo.org` puisque c'est servi depuis `chaingo.org`)

---

## 10. Sauvegarde des seeds — CRITIQUE

Le datadir contient `validator.seed` et `faucet.seed` : ce sont les **clés** du
nœud et du faucet. Si tu les perds, ce nœud précis devient muet.

```bash
# Sur le VPS : chiffrer
sudo tar czf - /var/lib/chaingo/*.seed | gpg -c > /tmp/chaingo-testnet-seeds.tar.gz.gpg
# (gpg te demande un mot de passe FORT — note-le hors ligne)
```

Depuis ta machine locale :

```bash
scp ghislain@chaingo.org:/tmp/chaingo-testnet-seeds.tar.gz.gpg ~/backups/
ssh ghislain@chaingo.org 'sudo rm /tmp/chaingo-testnet-seeds.tar.gz.gpg'
```

Pour restaurer plus tard (sur un nouveau serveur) :

```bash
gpg -d chaingo-testnet-seeds.tar.gz.gpg | sudo tar xzf - -C /
sudo chown chaingo:chaingo /var/lib/chaingo/*.seed
```

---

## 11. Première transaction de test

Depuis ta machine, avec le binaire `chaingo` local :

```powershell
.\chaingo.exe wallet new alice
.\chaingo.exe faucet --to alice --amount 100 --api https://node.chaingo.org
.\chaingo.exe balance alice --api https://node.chaingo.org
.\chaingo.exe send --from alice --to <adresse> --amount 5 --api https://node.chaingo.org
```

Ou tout simplement : ouvre **https://chaingo.org/wallet/** dans un navigateur,
crée un wallet, demande des CGO au faucet — tout marche sans ligne de commande.

---

## 12. Lancer un nœud LOCAL qui rejoint ton testnet

> ⚠️ N'utilise PAS `--testnet` en local : ça créerait un AUTRE réseau. Pour rejoindre le tien, on télécharge la genèse depuis le VPS :

```powershell
.\chaingo.exe node start `
  --genesis-url https://node.chaingo.org/v1/genesis `
  --peers node.chaingo.org:9000 `
  --datadir .testnet-local `
  --api 127.0.0.1:8545 `
  --p2p 127.0.0.1:9001
```

Ton nœud local télécharge la genèse, se synchronise, reçoit les nouveaux blocs.
Pas besoin de port ouvert sur ta box : la connexion sortante TCP suffit.

---

## 13. Maintenance

### Mettre à jour le code (site **et** binaire)

```bash
ssh ghislain@chaingo.org
cd /opt/chaingo
sudo git pull
sudo /usr/local/go/bin/go build -trimpath -ldflags="-s -w" -o /usr/local/bin/chaingo ./cmd/chaingo
sudo systemctl restart chaingo-testnet
# Le site /opt/chaingo/web est servi tel quel par Caddy : git pull suffit pour le mettre à jour
```

### Logs

```bash
sudo journalctl -u chaingo-testnet -f       # nœud
sudo journalctl -u caddy -f                  # site + HTTPS
```

### Espace disque

```bash
sudo du -sh /var/lib/chaingo
df -h /
```

### Redémarrage

```bash
sudo systemctl restart chaingo-testnet
sudo systemctl restart caddy
```

---

## 14. PLUS TARD — ajouter un nœud MAINNET sur le même VPS

> 🔴 **Ne fais ça que quand la checklist mainnet est cochée** :
> Phase 2 BFT finalisée + audit externe + ≥ 4 validateurs indépendants + testnet
> public stable depuis plusieurs semaines + document de genèse mainnet validé.
> Voir [docs/MAINNET.md](MAINNET.md).

Le principe : un **2e nœud** sur le **même VPS**, **ports différents**, **datadir différent**, **user système différent**, **sous-domaine différent**.

### 14.1 DNS

Ajouter un enregistrement `A` : `mainnet.chaingo.org` → IP du VPS.

### 14.2 User système et datadir

```bash
sudo useradd -r -m -d /var/lib/chaingo-mainnet -s /usr/sbin/nologin chaingo-mainnet
sudo install -d -o chaingo-mainnet -g chaingo-mainnet /var/lib/chaingo-mainnet
```

### 14.3 Mettre la genèse mainnet en place

Le document `mainnet.json` (généré par `chaingo genesis template/validate`, voir
[MAINNET.md](MAINNET.md)) :

```bash
sudo install -d /etc/chaingo
sudo cp mainnet.json /etc/chaingo/mainnet.json
sudo cp validator.seed /var/lib/chaingo-mainnet/validator.seed
sudo chown chaingo-mainnet:chaingo-mainnet /var/lib/chaingo-mainnet/validator.seed
sudo chmod 600 /var/lib/chaingo-mainnet/validator.seed
```

### 14.4 Service systemd dédié

```bash
sudo tee /etc/systemd/system/chaingo-mainnet.service >/dev/null <<'EOF'
[Unit]
Description=ChainGO mainnet node
After=network-online.target
Wants=network-online.target

[Service]
User=chaingo-mainnet
ExecStart=/usr/local/bin/chaingo node start \
  --genesis /etc/chaingo/mainnet.json \
  --validator-seed /var/lib/chaingo-mainnet/validator.seed \
  --datadir /var/lib/chaingo-mainnet \
  --api 127.0.0.1:8546 \
  --p2p :9001 \
  --peers <ip-bootstrap-1>:9001,<ip-bootstrap-2>:9001
Restart=always
RestartSec=3
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now chaingo-mainnet
```

### 14.5 Pare-feu

```bash
sudo ufw allow 9001/tcp          # P2P mainnet
```

### 14.6 Ajouter le routage Caddy

Ajouter ce bloc à `/etc/caddy/Caddyfile` :

```caddy
mainnet.chaingo.org {
    reverse_proxy 127.0.0.1:8546
    encode gzip
    header Access-Control-Allow-Origin "*"
}
```

Puis :

```bash
sudo systemctl reload caddy
```

### 14.7 Vérification

```bash
curl -s https://mainnet.chaingo.org/v1/status
# {"chain_id":"chaingo-1",...}
```

**Le site `chaingo.org` reste le même** — tu peux ajouter une bascule
testnet/mainnet dans le wallet (à venir : sélecteur de réseau), ou héberger un
wallet dédié sur `mainnet.chaingo.org/wallet/`.

---

## 15. Devrais-je garder le testnet quand le mainnet est en ligne ?

**Oui — sur le même serveur, sans souci.** Le testnet reste utile en permanence pour :
- Les nouveaux utilisateurs qui veulent tester avant de bouger des fonds réels,
- Les développeurs qui intègrent l'API,
- Les futures versions à valider avant de toucher au mainnet.

Le VPS fait tourner les deux nœuds en parallèle (~150 Mo de RAM pour les deux,
~100 Mo de disque par jour de blocs).

---

## 16. Et si je veux séparer testnet et mainnet sur 2 serveurs ?

Recommandé à terme (sécurité + isolation des charges). Procédure :

1. Sur le **nouveau** VPS mainnet, suit ce guide en remplaçant `node` par `mainnet` et `--testnet` par `--genesis /etc/chaingo/mainnet.json --validator-seed ...`.
2. Sur l'ancien (testnet), supprime juste le bloc `mainnet.chaingo.org` du Caddyfile et désactive le service `chaingo-mainnet`.

---

## Récap des commandes utiles

| Action | Commande |
|--------|----------|
| Statut nœud testnet | `sudo systemctl status chaingo-testnet` |
| Logs nœud | `sudo journalctl -u chaingo-testnet -f` |
| Logs Caddy | `sudo journalctl -u caddy -f` |
| Statut chaîne | `curl https://node.chaingo.org/v1/status` |
| Mise à jour | `cd /opt/chaingo && sudo git pull && sudo /usr/local/go/bin/go build -trimpath -ldflags="-s -w" -o /usr/local/bin/chaingo ./cmd/chaingo && sudo systemctl restart chaingo-testnet` |
| Backup seeds | `sudo tar czf - /var/lib/chaingo/*.seed \| gpg -c > seeds.tgz.gpg` |
| Recharger Caddy | `sudo systemctl reload caddy` |
| Taille de la db | `sudo du -sh /var/lib/chaingo` |

---

## En cas de pépin

- **Caddy ne démarre pas** : `sudo journalctl -u caddy -n 50` — souvent un Caddyfile mal formé. `sudo caddy validate --config /etc/caddy/Caddyfile`.
- **HTTPS ne marche pas** : vérifier que le port 80 est ouvert (challenge Let's Encrypt), et que le DNS pointe bien vers le VPS.
- **Le nœud ne démarre pas** : `sudo journalctl -u chaingo-testnet -n 50` — souvent un port déjà pris (`netstat -tlnp | grep 8545`).
- **L'API renvoie 502** : Caddy tourne mais le nœud non. `sudo systemctl restart chaingo-testnet`.
