#!/usr/bin/env bash
# become-validator.sh — onboarding "1 commande" pour devenir validateur sur
# le testnet public ChainGO. Idempotent : peut être relancé pour reprendre
# une installation interrompue (re-stake si déjà initialisé, etc.).
#
# Usage :
#   sudo bash scripts/become-validator.sh                 # interactif
#   sudo bash scripts/become-validator.sh --stake 10000   # montant explicite
#   sudo bash scripts/become-validator.sh --skip-faucet   # déjà financé
#
# Pré-requis : un nœud ChainGO déjà installé en service systemd
# (chaingo-testnet.service), suivant le guide docs/TESTNET-DEPLOY.md.

set -euo pipefail

# ---- Configuration par défaut ----
SERVICE="chaingo-testnet.service"
SEED_PATH="/var/lib/chaingo/validator.seed"
WALLET_NAME="validateur"
API="https://node.chaingo.org"
PEERS_HOST="node.chaingo.org:9000"
STAKE_AMOUNT_CGO=10000
FAUCET_AMOUNT_CGO=15000   # stake + marge pour les frais
SKIP_FAUCET=0
ASSUME_YES=0

while [ $# -gt 0 ]; do
  case "$1" in
    --stake)        STAKE_AMOUNT_CGO="$2"; shift 2;;
    --faucet)       FAUCET_AMOUNT_CGO="$2"; shift 2;;
    --skip-faucet)  SKIP_FAUCET=1; shift;;
    --service)      SERVICE="$2"; shift 2;;
    --api)          API="$2"; shift 2;;
    --wallet)       WALLET_NAME="$2"; shift 2;;
    -y|--yes)       ASSUME_YES=1; shift;;
    -h|--help)
      grep '^# ' "$0" | sed 's/^# //'
      exit 0;;
    *)
      echo "Option inconnue : $1"; exit 1;;
  esac
done

[ "$(id -u)" = 0 ] || { echo "Lancez en root : sudo bash $0"; exit 1; }
command -v chaingo >/dev/null || { echo "Binaire 'chaingo' introuvable. Suivez d'abord docs/TESTNET-DEPLOY.md."; exit 1; }
command -v curl >/dev/null || { echo "curl requis."; exit 1; }

# ---- Helpers ----
log()  { printf "\n\033[1;36m▸ %s\033[0m\n" "$*"; }
ok()   { printf "  \033[1;32m✓\033[0m %s\n" "$*"; }
warn() { printf "  \033[1;33m!\033[0m %s\n" "$*"; }
die()  { printf "\n\033[1;31m✗ %s\033[0m\n" "$*"; exit 1; }

confirm() {
  [ "$ASSUME_YES" = 1 ] && return 0
  read -r -p "  $1 [y/N] " r
  [[ "$r" =~ ^[Yy]$ ]]
}

# Récupère un nombre depuis un JSON style {"chain_id":"...","height":42}
json_get() {
  local key="$1"
  grep -oE "\"$key\":\"?[^\",}]+" | head -1 | sed -E "s/\"$key\":\"?//"
}

# ---- 1. Vérifier le service ----
log "Vérification du service systemd ($SERVICE)"
if ! systemctl status "$SERVICE" >/dev/null 2>&1; then
  die "Service $SERVICE introuvable. Installer d'abord un nœud — voir docs/TESTNET-DEPLOY.md."
fi
SERVICE_FILE="/etc/systemd/system/$SERVICE"
[ -f "$SERVICE_FILE" ] || die "$SERVICE_FILE introuvable."
ok "Service présent et chargé"

# ---- 2. Seed de validateur ----
log "Seed du validateur"
if [ -f "$SEED_PATH" ]; then
  ok "Seed déjà présente : $SEED_PATH"
else
  chaingo keygen --out "$SEED_PATH" >/dev/null
  chmod 600 "$SEED_PATH"
  # Le user système 'chaingo' doit pouvoir la lire — sinon le service plante au démarrage.
  if id chaingo >/dev/null 2>&1; then
    chown chaingo:chaingo "$SEED_PATH"
  fi
  ok "Seed générée et sécurisée"
fi

# Récupère l'adresse à partir de la seed (deterministe via keygen --import-only ?
# pas dispo — on relit via wallet import dans un répertoire tmp et on prend
# l'adresse, puis on retire).
TMP_HOME="$(mktemp -d)"
trap 'rm -rf "$TMP_HOME"' EXIT
ADDR=$(HOME="$TMP_HOME" chaingo wallet import _probe --seed "$SEED_PATH" --pass '' 2>/dev/null \
  | awk '/Adresse/ {print $NF}')
[ -n "$ADDR" ] || die "Impossible de dériver l'adresse depuis la seed."
ok "Adresse du validateur : $ADDR"

# ---- 3. Patch du service pour ajouter --validator-seed ----
log "Service systemd — validator-seed"
if grep -q "validator-seed $SEED_PATH" "$SERVICE_FILE"; then
  ok "Service déjà configuré comme validateur"
else
  # Insertion juste avant --datadir (présent dans la plupart des configs).
  # Si --datadir absent, on ajoute en fin de l'ExecStart sur sa propre ligne.
  if grep -q -- "--datadir" "$SERVICE_FILE"; then
    sed -i "/^ExecStart=/,/^[^ \\\\]/ { /--datadir/i\\
  --validator-seed $SEED_PATH \\\\
}" "$SERVICE_FILE"
  else
    sed -i "/^ExecStart=/ s|$| --validator-seed $SEED_PATH|" "$SERVICE_FILE"
  fi
  systemctl daemon-reload
  systemctl restart "$SERVICE"
  ok "Service patché et redémarré"
  sleep 2
fi

# Vérifie que les logs récents contiennent bien la ligne validator: <addr>
if ! journalctl -u "$SERVICE" --since "5 minutes ago" --no-pager | grep -q "validator: $ADDR"; then
  warn "La ligne 'validator: $ADDR' n'est pas (encore) visible dans les logs."
  warn "Vérifier : sudo journalctl -u $SERVICE -n 50 --no-pager | grep validator"
fi

# ---- 4. Faucet ----
log "Faucet — financement de l'adresse du validateur"
if [ "$SKIP_FAUCET" = 1 ]; then
  ok "Faucet sauté (--skip-faucet)"
else
  AMOUNT_UCGO=$((FAUCET_AMOUNT_CGO * 1000000000))
  HTTP_CODE=$(curl -s -o /tmp/faucet.resp -w "%{http_code}" \
    -X POST "$API/v1/dev/faucet" \
    -H 'Content-Type: application/json' \
    -d "{\"address\":\"$ADDR\",\"amount\":$AMOUNT_UCGO}")
  if [ "$HTTP_CODE" = "202" ]; then
    ok "$FAUCET_AMOUNT_CGO CGO demandés au faucet (réponse $(cat /tmp/faucet.resp))"
  else
    warn "Faucet a renvoyé HTTP $HTTP_CODE — peut-être déjà financé ou faucet temporairement limité."
    warn "Réponse : $(cat /tmp/faucet.resp)"
  fi
  log "Attente confirmation du faucet (4 s)…"
  sleep 4
fi

# ---- 5. Importer la seed dans le keystore CLI root ----
log "Import dans le keystore CLI"
if HOME="$HOME" chaingo balance "$WALLET_NAME" --api "$API" >/dev/null 2>&1; then
  ok "Wallet '$WALLET_NAME' déjà présent dans le keystore"
else
  chaingo wallet import "$WALLET_NAME" --seed "$SEED_PATH" --pass '' >/dev/null
  ok "Wallet '$WALLET_NAME' importé (mot de passe vide — convient pour un nœud serveur dédié)"
fi

# ---- 6. Vérifier le solde ----
log "Vérification du solde"
BAL=$(chaingo balance "$WALLET_NAME" --api "$API" 2>/dev/null | awk '/CGO/ {print $2; exit}')
BAL=${BAL:-0}
echo "  Solde actuel : $BAL CGO"
# Comparer sans la partie décimale
BAL_INT=$(echo "$BAL" | cut -d. -f1)
if [ "${BAL_INT:-0}" -lt "$STAKE_AMOUNT_CGO" ]; then
  die "Solde insuffisant pour staker $STAKE_AMOUNT_CGO CGO. Relancez avec --faucet $((STAKE_AMOUNT_CGO + 1000))."
fi
ok "Solde suffisant pour staker"

# ---- 7. Vérifier si déjà validateur ----
log "État du validateur"
VALIDATORS_JSON=$(curl -s "$API/v1/validators")
if echo "$VALIDATORS_JSON" | grep -q "\"address\":\"$ADDR\""; then
  ok "Déjà validateur actif sur le réseau"
  echo "$VALIDATORS_JSON" | python3 -m json.tool 2>/dev/null | grep -A4 "$ADDR" | head -7 || true
else
  log "Staking $STAKE_AMOUNT_CGO CGO…"
  if ! confirm "Confirmer le stake de $STAKE_AMOUNT_CGO CGO (irréversible avant unbonding 24 h) ?"; then
    warn "Stake annulé. Relancez le script quand vous êtes prêt."
    exit 0
  fi
  chaingo stake --from "$WALLET_NAME" --amount "$STAKE_AMOUNT_CGO" --api "$API" --pass '' || \
    die "Échec du stake. Vérifier les logs et le solde."
  ok "Transaction de stake envoyée"
  log "Attente d'inclusion dans un bloc…"
  sleep 6
  VALIDATORS_JSON=$(curl -s "$API/v1/validators")
  if echo "$VALIDATORS_JSON" | grep -q "\"address\":\"$ADDR\""; then
    ok "Validateur actif !"
  else
    warn "Pas encore visible dans /v1/validators — la tx est peut-être encore en mempool."
    warn "Vérifier dans quelques secondes : curl $API/v1/validators"
  fi
fi

# ---- Résumé ----
cat <<EOF

================================================================
 ChainGO — Validateur configuré
================================================================
  Adresse           : $ADDR
  Seed              : $SEED_PATH  (sauvegardez ce fichier hors ligne)
  Service           : $SERVICE (redémarrage auto activé)
  Stake             : $STAKE_AMOUNT_CGO CGO
  Wallet CLI        : $WALLET_NAME

À surveiller :
  • Logs en direct  : sudo journalctl -u $SERVICE -f
  • Blocs proposés  : curl -s $API/v1/validators | python3 -m json.tool
  • Explorateur     : https://chaingo.org/explorer/#/validator/$ADDR

Ce que vous gagnez :
  • Récompenses    : ~3 %/an sur votre stake actif
  • Tips           : 100 % des tips des tx incluses dans vos blocs

Ce qu'il faut éviter :
  • Ne JAMAIS faire tourner ce nœud sur plusieurs serveurs en même temps
    (double-signature = slash 5 % automatique).
  • Garder le nœud en ligne (downtime prolongé = jail + slash 0,1 %).

EOF
EOF
