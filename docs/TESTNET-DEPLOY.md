# Déployer le testnet ChainGO sur Debian (chaingo.org)

Pas-à-pas concret pour mettre en ligne le 1er nœud testnet public sur ton VPS
Debian, accessible via `node.chaingo.org`. Une fois en place :
- le wallet web public (https://ghisdot.github.io/chaingo/wallet/) pourra s'y connecter,
- la CLI `chaingo` pourra l'interroger via `--api https://node.chaingo.org`,
- n'importe qui pourra lancer un nœud local qui rejoint ce testnet.

> ⏱️ Durée totale : ~15 min, dont ~5 min de compilation.

---

## 1. DNS — pointer `node.chaingo.org` sur le VPS

Dans la console DNS de `chaingo.org` (OVH ou autre), ajoute un enregistrement :

| Type | Sous-domaine | Cible            | TTL  |
|------|--------------|------------------|------|
| `A`  | `node`       | `IPv4 du VPS`    | 300  |

Optionnel : un `AAAA` pour l'IPv6 si ton VPS en a une.

> 💡 On garde `chaingo.org` (apex) pour un futur site officiel (ou pour pointer
> sur GitHub Pages). `node.chaingo.org` = l'API publique du testnet.

Vérification depuis ta machine :

```bash
dig +short node.chaingo.org   # doit retourner l'IP du VPS
# ou : nslookup node.chaingo.org
```

La propagation DNS prend quelques minutes (souvent < 2 min avec TTL 300).

---

## 2. Premier SSH + hardening minimal

```bash
ssh root@<ip-du-vps>
# (ou ssh debian@<ip-du-vps> si OVH t'a donné un user "debian")
```

Mets à jour et crée un user dédié :

```bash
apt update && apt full-upgrade -y
adduser ghislain                # mot de passe fort
usermod -aG sudo ghislain
```

**Depuis ta machine locale**, dans un autre terminal, dépose ta clé SSH publique :

```bash
ssh-copy-id ghislain@node.chaingo.org
ssh ghislain@node.chaingo.org   # vérifie que ça marche AVANT la suite
```

**Une fois la connexion par clé confirmée**, durcis le SSH sur le VPS :

```bash
sudo sed -i 's/^#*PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
sudo sed -i 's/^#*PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
sudo systemctl reload ssh
```

> ⚠️ Garde **une session SSH ouverte** pendant que tu testes la nouvelle config
> dans une seconde session. Si la 2e ne passe pas, tu corriges depuis la 1re.

---

## 3. Déploiement du nœud testnet (une commande)

Sur le VPS, en root (ou via sudo) :

```bash
curl -fsSL https://raw.githubusercontent.com/ghisdot/chaingo/main/scripts/deploy-node.sh -o deploy.sh
sudo bash deploy.sh --network testnet --domain node.chaingo.org
```

Le script ([scripts/deploy-node.sh](../scripts/deploy-node.sh)) :
- installe Go 1.26.4 dans `/usr/local/go`,
- clone le dépôt dans `/opt/chaingo` et compile `chaingo` dans `/usr/local/bin/`,
- crée l'utilisateur système `chaingo` et son datadir `/var/lib/chaingo`,
- ouvre les ports `22 / 80 / 443 / 9000` dans UFW,
- installe **Caddy** avec **HTTPS automatique** (Let's Encrypt) pour `node.chaingo.org`,
- installe le service `systemd` `chaingo` (redémarrage auto, survit aux reboots),
- démarre le testnet (`chain_id = chaingo-testnet-1`, faucet ouvert).

Compter 3–5 min selon la bande passante du VPS.

---

## 4. Vérifications

État du service et logs :

```bash
sudo systemctl status chaingo                # doit être active (running)
sudo journalctl -u chaingo -f                # blocs qui défilent
sudo journalctl -u chaingo | grep validator: # adresse du validateur de genèse
```

API publique (depuis ta machine) :

```bash
curl https://node.chaingo.org/v1/status
# -> {"chain_id":"chaingo-testnet-1","height":42,"finalized_height":41,...}

curl https://node.chaingo.org/v1/fees
# -> base_fee, tips, min_validator_stake, unbonding_seconds, etc.
```

Si l'HTTPS ne répond pas dans la minute, c'est Caddy qui est en train de chercher le certificat :

```bash
sudo journalctl -u caddy -n 50
```

---

## 5. Sauvegarde des seeds — CRITIQUE

Le datadir contient `validator.seed` et `faucet.seed` : ce sont les **clés** du
validateur de genèse et du faucet du testnet. Si tu les perds, ce nœud devient
muet (la chaîne continue si d'autres validateurs sont en ligne, mais ici tu es
seul à la genèse).

```bash
# Sur le VPS, chiffrer puis copier hors du serveur :
sudo tar czf - /var/lib/chaingo/*.seed | gpg -c > /tmp/chaingo-seeds.tar.gz.gpg
# (gpg te demande un mot de passe fort — note-le hors ligne)
```

Depuis ta machine :

```bash
scp ghislain@node.chaingo.org:/tmp/chaingo-seeds.tar.gz.gpg ~/secure/
# Puis effacer la copie temporaire du VPS :
ssh ghislain@node.chaingo.org 'sudo rm /tmp/chaingo-seeds.tar.gz.gpg'
```

Restauration plus tard, sur un nouveau VPS :

```bash
gpg -d chaingo-seeds.tar.gz.gpg | sudo tar xzf - -C /
sudo chown chaingo:chaingo /var/lib/chaingo/*.seed
```

---

## 6. Brancher le wallet web officiel

https://ghisdot.github.io/chaingo/wallet/ ouvre par défaut un champ "Nœud".
Saisis-y `https://node.chaingo.org` et sauve : toutes les opérations
(création de wallet, faucet, envoi, multisig…) passent désormais par ton
testnet, **sans qu'aucune clé privée ne quitte le navigateur** (signature
ML-DSA-65 via WASM).

---

## 7. Premières transactions de test (CLI)

Depuis ta machine, le binaire `chaingo` :

```powershell
# Créer un wallet local (clés ML-DSA-65, chiffrées avec mot de passe)
.\chaingo.exe wallet new alice

# Demander des CGO au faucet
.\chaingo.exe faucet --to alice --amount 100 --api https://node.chaingo.org

# Vérifier le solde
.\chaingo.exe balance alice --api https://node.chaingo.org

# Envoyer
.\chaingo.exe send --from alice --to <adresse> --amount 5 --api https://node.chaingo.org

# Créer un token no-code
.\chaingo.exe token create --from alice --symbol MONTOK --name "Mon Token" `
  --supply 1000000 --api https://node.chaingo.org

# Créer un coffre multisig 2-of-3
.\chaingo.exe contract multisig --from alice --signers alice,bob,carol --threshold 2 `
  --amount 100 --api https://node.chaingo.org
```

---

## 8. Faire tourner un nœud local qui REJOINT ce testnet

> ⚠️ Ne pas relancer avec `--testnet` en local — ça créerait un **autre**
> réseau. Pour rejoindre celui du VPS, on utilise `--genesis-url` :

```powershell
.\chaingo.exe node start `
  --genesis-url https://node.chaingo.org/v1/genesis `
  --peers node.chaingo.org:9000 `
  --datadir .testnet-local `
  --api 127.0.0.1:8545 `
  --p2p 127.0.0.1:9001
```

Ton nœud local :
- télécharge la genèse depuis le VPS,
- se synchronise par lots,
- reçoit les nouveaux blocs en gossip,
- expose `http://127.0.0.1:8545/v1/...` pour la CLI et le wallet web.

Pas besoin de port ouvert sur ta box : la connexion sortante TCP suffit.

---

## 9. Maintenance courante

### Mettre à jour le binaire (après un push sur `main`)

```bash
ssh ghislain@node.chaingo.org
cd /opt/chaingo
sudo git pull
sudo /usr/local/go/bin/go build -trimpath -ldflags="-s -w" -o /usr/local/bin/chaingo ./cmd/chaingo
sudo systemctl restart chaingo
sudo journalctl -u chaingo -f          # vérifier la reprise
```

L'état est persisté dans `/var/lib/chaingo` : la chaîne reprend exactement où elle s'était arrêtée.

### Voir l'espace disque utilisé par la chaîne

```bash
sudo du -sh /var/lib/chaingo
```

### Redémarrer / arrêter

```bash
sudo systemctl restart chaingo
sudo systemctl stop chaingo
sudo systemctl start chaingo
```

### Logs Caddy (HTTPS, certificat Let's Encrypt)

```bash
sudo journalctl -u caddy -n 100 -f
```

Le renouvellement du certificat est automatique (tous les ~60 jours).

---

## 10. Sécurité — points à connaître

- **Faucet ouvert** sur testnet (`POST /v1/dev/faucet`) : c'est intentionnel,
  c'est ce qui permet aux testeurs d'obtenir des CGO sans cérémonie. Si le
  faucet devient une cible de spam, on peut le rate-limit côté Caddy ou
  désactiver l'endpoint via un futur flag.
- L'endpoint `POST /v1/dev/wallet` (qui **révèle** une seed côté serveur) est
  **désactivé** sur testnet — il ne marche qu'en `--dev` local.
- Le port P2P (9000) parle en clair (TCP gossip JSON pour l'instant) : c'est
  attendu et nécessaire pour qu'on puisse y attaquer le codec binaire (Phase 2,
  [#8](https://github.com/ghisdot/chaingo/issues/8)).
- UFW est configuré : seuls `22 / 80 / 443 / 9000` sont ouverts.

---

## 11. Et le mainnet ?

Le même script avec `--network mainnet`, mais **seulement après** la checklist
de [docs/MAINNET.md](MAINNET.md) :
- Phase 2 BFT finalisée + audit externe passé,
- ≥ 4 validateurs indépendants engagés,
- testnet stable depuis plusieurs semaines,
- document de genèse mainnet validé par tous (mêmes empreintes).

Le sous-domaine sera probablement `mainnet.chaingo.org` (ou directement
`chaingo.org`), avec le **site officiel** côté `chaingo.org` (pointant sur
GitHub Pages via DNS).

---

## Récap des commandes essentielles

| Action                                 | Commande                                                     |
|----------------------------------------|--------------------------------------------------------------|
| Logs en direct                         | `sudo journalctl -u chaingo -f`                              |
| État du nœud                           | `curl https://node.chaingo.org/v1/status`                    |
| Mise à jour                            | `cd /opt/chaingo && sudo git pull && sudo go build ... && sudo systemctl restart chaingo` |
| Sauvegarde des seeds                   | `sudo tar czf - /var/lib/chaingo/*.seed \| gpg -c > seeds.tgz.gpg` |
| Désactiver le service                  | `sudo systemctl stop chaingo`                                |
| Réactiver                              | `sudo systemctl start chaingo`                               |
| Voir la taille de la chaîne            | `sudo du -sh /var/lib/chaingo`                               |
