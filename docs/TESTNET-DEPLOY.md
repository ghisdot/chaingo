# Faire tourner un nœud ChainGO

Guide pas-à-pas pour les opérateurs qui veulent contribuer au réseau ChainGO en
faisant tourner un nœud. Deux parcours :

- **A.** Rejoindre le **testnet public** ChainGO (`chaingo-testnet-1`) — le plus
  rapide, idéal pour tester, valider, ou se préparer à devenir validateur.
- **B.** Bootstrap d'un **nouveau réseau** (testnet privé ou futur mainnet) —
  pour les équipes qui veulent lancer leur propre instance.

> 💡 **Vous voulez juste utiliser ChainGO ?**
> Pas besoin de serveur. Le wallet web tourne dans votre navigateur :
> [`https://chaingo.org/wallet/`](https://chaingo.org/wallet/) (clés
> post-quantiques générées localement, jamais envoyées). Pour explorer la
> chaîne en direct : [`https://chaingo.org/explorer/`](https://chaingo.org/explorer/).

---

## Pré-requis

| Composant | Recommandation |
|---|---|
| OS | Debian 12 (Bookworm) ou Ubuntu LTS récent |
| CPU / RAM | 2 vCPU / 4 Go RAM (largement suffisant pour un nœud testnet) |
| Disque | 20 Go SSD (la base bbolt grossit lentement) |
| Réseau | IPv4 publique, ports `22 / 80 / 443 / 9000` ouvrables |
| Accès | SSH en root ou via `sudo` |
| Optionnel | Un nom de domaine (recommandé pour exposer l'API publique en HTTPS) |

---

## A. Rejoindre le testnet public ChainGO

Le réseau de référence est `chaingo-testnet-1`, dont le nœud d'amorçage public
expose son API sur `https://node.chaingo.org` et son port P2P sur
`node.chaingo.org:9000`.

### A.1 Installer Go et compiler le binaire

```bash
# Sur le serveur, en root
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

### A.2 Créer un utilisateur système dédié

```bash
useradd -r -m -d /var/lib/chaingo -s /usr/sbin/nologin chaingo
```

### A.3 Service systemd qui rejoint le testnet public

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

Le nœud télécharge la genèse depuis le seed public, se synchronise par lots et
reçoit ensuite les blocs en gossip P2P.

### A.4 Vérifier que le nœud est synchronisé

```bash
systemctl status chaingo-testnet --no-pager
journalctl -u chaingo-testnet -n 30 --no-pager

# Hauteur locale vs hauteur du réseau :
curl -s http://127.0.0.1:8545/v1/status | grep -oE '"height":[0-9]+'
curl -s https://node.chaingo.org/v1/status | grep -oE '"height":[0-9]+'
```

Quand les deux hauteurs sont identiques (à ±1), le nœud est synchronisé. Il
peut alors être utilisé en lecture (sans clé validateur) ou — si l'opérateur
veut devenir validateur — il faudra staker depuis un wallet financé (voir
[VALIDATOR.md](VALIDATOR.md)).

### A.5 (Optionnel) Exposer l'API en HTTPS via un domaine

Si l'opérateur dispose d'un nom de domaine (par ex. `node.exemple.org`),
Caddy fournit HTTPS automatiquement via Let's Encrypt.

```bash
# DNS : créer un A record pointant <votre-sous-domaine> → IP du VPS

# Pare-feu
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
<votre-sous-domaine.exemple.org> {
    reverse_proxy 127.0.0.1:8545
    encode gzip
}
EOF
systemctl reload caddy
```

> Remplacer `<votre-sous-domaine.exemple.org>` par le FQDN réel. Le certificat
> TLS est obtenu en 30-60 s au premier démarrage.

---

## B. Bootstrap d'un nouveau réseau (testnet privé ou mainnet)

Pour les équipes qui veulent **créer leur propre chaîne** plutôt que rejoindre
le testnet public. La procédure est identique pour un testnet privé ou pour
préparer le futur mainnet — seul le document de genèse change.

### B.1 Générer la clé du validateur de genèse

```bash
chaingo keygen --out /var/lib/chaingo/validator.seed
chmod 600 /var/lib/chaingo/validator.seed
```

> ⚠️ Cette `validator.seed` est l'identité du validateur. **Sauvegardez-la
> immédiatement et hors-ligne** (voir section *Sauvegarde des seeds* plus bas).

### B.2 Préparer le document de genèse

```bash
chaingo genesis template \
  --chain-id <id-de-votre-chaine> \
  --out /etc/chaingo/genesis.json \
  --seed-out /var/lib/chaingo/validator.seed

# Éditer /etc/chaingo/genesis.json (alloc / stakes / vesting / params)
# selon la distribution prévue. Voir docs/MAINNET.md pour la cérémonie.

chaingo genesis validate /etc/chaingo/genesis.json
```

`validate` imprime une empreinte déterministe (`block hash` + `state root`).
**Tous les opérateurs participant au bootstrap doivent obtenir la même
empreinte** sur la même genèse — c'est la garantie de démarrage du réseau.

### B.3 Service systemd qui amorce le nouveau réseau

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

Les autres validateurs initiaux et les nœuds-suiveurs rejoignent ensuite via
`--genesis-url https://<premier-nœud>/v1/genesis --peers <premier-nœud>:9000`.

---

## Sauvegarde des seeds (impératif)

Le datadir `/var/lib/chaingo/` contient `validator.seed` (et, en mode `--dev`
ou `--testnet`, `faucet.seed`). Sans ces fichiers, le validateur ne peut plus
signer de blocs avec son identité.

```bash
# Sur le serveur — chiffrer avec une passphrase forte (notée hors-ligne)
mkdir -p ~/.gnupg && echo "allow-loopback-pinentry" >> ~/.gnupg/gpg-agent.conf
gpgconf --kill gpg-agent
tar czf - /var/lib/chaingo/*.seed \
  | gpg --pinentry-mode loopback -c \
  > /tmp/chaingo-seeds.tar.gz.gpg
```

Ensuite, depuis un poste de confiance, télécharger l'archive chiffrée et
supprimer la copie temporaire du serveur :

```bash
scp <user>@<serveur>:/tmp/chaingo-seeds.tar.gz.gpg ~/backups/
ssh <user>@<serveur> 'rm /tmp/chaingo-seeds.tar.gz.gpg'
```

Restauration sur un nouveau serveur :

```bash
gpg -d chaingo-seeds.tar.gz.gpg | sudo tar xzf - -C /
sudo chown chaingo:chaingo /var/lib/chaingo/*.seed
sudo chmod 600 /var/lib/chaingo/*.seed
```

---

## Maintenance

### Mettre à jour le binaire

```bash
cd /opt/chaingo
git pull
/usr/local/go/bin/go build -trimpath -ldflags="-s -w" -o /usr/local/bin/chaingo ./cmd/chaingo
systemctl restart chaingo-testnet   # ou chaingo selon le service
```

L'état est persisté dans `/var/lib/chaingo/` ; la chaîne reprend exactement où
elle s'est arrêtée.

### Suivre les logs

```bash
journalctl -u chaingo-testnet -f
```

### Mesurer l'espace utilisé

```bash
du -sh /var/lib/chaingo
```

### Recharger après modification du Caddyfile

```bash
systemctl reload caddy
```

---

## Sécurité du serveur

Recommandations minimales pour un nœud public :

- **Compte non-root + clé SSH** : créer un utilisateur dédié, désactiver
  l'authentification par mot de passe et la connexion `root` par mot de passe
  (`PermitRootLogin prohibit-password`, `PasswordAuthentication no` dans
  `/etc/ssh/sshd_config`).
- **Pare-feu UFW** : seuls les ports `22 / 80 / 443 / 9000` ouverts.
- **Mises à jour OS** : `unattended-upgrades` pour les patches de sécurité.
- **Sauvegarde régulière** de l'archive chiffrée des seeds.

---

## En cas de pépin

| Symptôme | Diagnostic |
|---|---|
| Le service ne démarre pas | `journalctl -u chaingo-testnet -n 100` — souvent un port déjà utilisé (`ss -tlnp | grep 8545`) |
| Caddy ne démarre pas | `caddy validate --config /etc/caddy/Caddyfile`. HTTPS bloqué : vérifier que le port 80 est ouvert (challenge Let's Encrypt). |
| L'API renvoie HTTP 502 | Caddy tourne mais le nœud non — `systemctl restart chaingo-testnet`. |
| Wallet web bloqué sur « hors-ligne » avec erreur CORS dédoublée | Une couche externe (proxy/firewall) ajoute un `Access-Control-Allow-Origin`. Le nœud ChainGO le renvoie déjà — ne pas le réajouter. |
| `gpg: Inappropriate ioctl for device` | Pinentry sans TTY. Suivre la procédure « loopback » de la section *Sauvegarde des seeds*. |

---

## Pour aller plus loin

- [VALIDATOR.md](VALIDATOR.md) — devenir validateur, staking, délégation, slashing
- [MAINNET.md](MAINNET.md) — pré-requis et cérémonie de genèse pour un mainnet
- [API.md](API.md) — référence complète de l'API REST
- [CONTRIBUTING.md](../CONTRIBUTING.md) — contribuer au code source
