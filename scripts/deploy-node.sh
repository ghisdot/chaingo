#!/usr/bin/env bash
# Déploiement clé en main d'un nœud ChainGO sur Ubuntu/Debian (OVH, etc.).
# À lancer EN ROOT sur le serveur fraîchement installé :
#
#   curl -fsSL https://raw.githubusercontent.com/ghisdot/chaingo/main/scripts/deploy-node.sh -o deploy.sh
#   sudo bash deploy.sh --network testnet --domain node.exemple.com
#
# Options :
#   --network testnet|mainnet   (défaut: testnet)
#   --domain  <fqdn>            active HTTPS auto via Caddy (recommandé : le wallet
#                               web sur GitHub Pages exige une API en HTTPS)
#   --peers   host:9000,...     pairs à rejoindre (réseau existant)
#   --branch  <branche|tag>     code à compiler (défaut: main)
set -euo pipefail

NETWORK=testnet
DOMAIN=""
PEERS=""
BRANCH=main
GO_VERSION=1.26.4
REPO=https://github.com/ghisdot/chaingo

while [ $# -gt 0 ]; do
  case "$1" in
    --network) NETWORK="$2"; shift 2;;
    --domain)  DOMAIN="$2";  shift 2;;
    --peers)   PEERS="$2";   shift 2;;
    --branch)  BRANCH="$2";  shift 2;;
    *) echo "option inconnue : $1"; exit 1;;
  esac
done

[ "$(id -u)" = 0 ] || { echo "À lancer en root (sudo bash deploy.sh ...)"; exit 1; }
case "$NETWORK" in
  testnet) ;;
  mainnet)
    echo "!!! --network mainnet : assure-toi d'avoir coché TOUTE la checklist de docs/MAINNET.md"
    echo "    (Phase 2 finie, audit, >=4 validateurs, genèse validée). Ctrl-C pour annuler."
    sleep 5 ;;
  *) echo "--network doit être testnet ou mainnet"; exit 1;;
esac

echo "==> Dépendances système"
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y git curl ufw

echo "==> Go ${GO_VERSION}"
if ! /usr/local/go/bin/go version 2>/dev/null | grep -q "go${GO_VERSION}"; then
  curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tgz
  rm -rf /usr/local/go
  tar -C /usr/local -xzf /tmp/go.tgz
fi

echo "==> Compilation de ChainGO (branche ${BRANCH})"
id chaingo >/dev/null 2>&1 || useradd -r -m -d /var/lib/chaingo chaingo
install -d -o chaingo -g chaingo /var/lib/chaingo
rm -rf /opt/chaingo
git clone --depth 1 --branch "$BRANCH" "$REPO" /opt/chaingo
( cd /opt/chaingo && /usr/local/go/bin/go build -trimpath -ldflags="-s -w" -o /usr/local/bin/chaingo ./cmd/chaingo )

echo "==> Compilation du wallet web (WASM)"
GOROOT="$(/usr/local/go/bin/go env GOROOT)"
( cd /opt/chaingo && GOOS=js GOARCH=wasm /usr/local/go/bin/go build -trimpath -ldflags="-s -w" -o web/wallet/chaingo.wasm ./cmd/wallet-wasm )
cp "$GOROOT/lib/wasm/wasm_exec.js" /opt/chaingo/web/wallet/wasm_exec.js

echo "==> Pare-feu (SSH, web, P2P)"
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw allow 9000/tcp
ufw --force enable

echo "==> Service systemd (redémarrage auto, survit aux reboots)"
PEERSFLAG=""
[ -n "$PEERS" ] && PEERSFLAG="--peers $PEERS"
cat >/etc/systemd/system/chaingo.service <<EOF
[Unit]
Description=ChainGO node (${NETWORK})
After=network-online.target
Wants=network-online.target

[Service]
User=chaingo
ExecStart=/usr/local/bin/chaingo node start --${NETWORK} --datadir /var/lib/chaingo --api 127.0.0.1:8545 --p2p :9000 ${PEERSFLAG}
Restart=always
RestartSec=3
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now chaingo

if [ -n "$DOMAIN" ]; then
  echo "==> HTTPS via Caddy pour ${DOMAIN}"
  apt-get install -y debian-keyring debian-archive-keyring apt-transport-https
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null
  apt-get update -y
  apt-get install -y caddy
  cat >/etc/caddy/Caddyfile <<EOF
${DOMAIN} {
    reverse_proxy 127.0.0.1:8545
}
EOF
  systemctl reload caddy
fi

echo "==> Attente du premier bloc..."
sleep 4
curl -s http://127.0.0.1:8545/v1/status || true
echo
echo "============================================================"
echo " Nœud ChainGO ${NETWORK} démarré."
echo " Logs        : journalctl -u chaingo -f"
echo " Adresse validateur : grep 'validator:' dans les logs ci-dessus"
echo " ⚠ SAUVEGARDE /var/lib/chaingo/validator.seed (et faucet.seed) HORS du serveur."
if [ -n "$DOMAIN" ]; then
  echo " API publique : https://${DOMAIN}/v1/status"
  echo " Pointe le wallet web (https://ghisdot.github.io/chaingo/wallet/) sur https://${DOMAIN}"
else
  echo " API locale uniquement (127.0.0.1:8545). Ajoute --domain pour l'exposer en HTTPS."
fi
echo "============================================================"
