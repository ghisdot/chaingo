# Checklist — lancer des validateurs indépendants (testnet)

Objectif : passer de « tout tourne sur les machines du mainteneur » à **plusieurs
validateurs indépendants** — c'est le **vrai jalon de décentralisation** avant le
mainnet. Cette checklist est opérationnelle ; pour les détails (systemd, HTTPS,
sauvegardes) voir [TESTNET-DEPLOY.md](TESTNET-DEPLOY.md) et
[VALIDATOR.md](VALIDATOR.md).

---

## Côté projet (toi, une fois)

- [ ] **Testnet stable** : un nœud d'amorçage (seed/bootnode) public et joignable
      (IP/DNS fixe, port P2P ouvert), hauteur qui avance.
- [ ] **Genesis figé et publié** : `genesis.json` du testnet téléchargeable
      (URL stable) — les nouveaux nœuds le récupèrent via `--genesis-url`.
- [ ] **Empreinte de genèse** publiée (les opérateurs vérifient qu'ils rejoignent
      bien le même réseau).
- [ ] **Faucet** en ligne (les candidats validateurs ont besoin de CGO de test
      pour staker) — viser large : stake mini = **10 000 CGO**.
- [ ] **Doc d'onboarding** (ce fichier + VALIDATOR.md) liée depuis le site.
- [ ] **Canal de support** (Discord/Telegram) pour les opérateurs.
- [ ] **Liste de bootnodes** documentée (au moins 1, idéalement 2-3 répartis).

## Côté opérateur (chaque validateur indépendant)

### 1. Préparer la machine
- [ ] VPS/serveur Linux dédié, horloge **synchronisée (NTP)** — critique pour un
      réseau à blocs 500 ms.
- [ ] Go installé, binaire compilé : `go build -o chaingo ./cmd/chaingo`.
- [ ] Port **P2P** ouvert au pare-feu (ex. 9000) ; API liée en local
      (`127.0.0.1`) ou en HTTPS via reverse-proxy (cf. TESTNET-DEPLOY.md §A.5).

### 2. Rejoindre le testnet
- [ ] Lancer en rejoignant les bootnodes :
      ```bash
      chaingo node start --testnet \
        --datadir /var/lib/chaingo \
        --genesis-url https://chaingo.org/testnet/genesis.json \
        --peers <bootnode1_host:port>,<bootnode2_host:port> \
        --api 127.0.0.1:8545 --p2p :9000
      ```
- [ ] **Vérifier la synchro** : hauteur locale == hauteur réseau
      (`/v1/status`), et **même empreinte de genèse** que celle publiée.
- [ ] Tourner en **service systemd** (redémarrage auto) — cf. TESTNET-DEPLOY.md §A.3.

### 3. Devenir validateur
- [ ] Créer le wallet de l'opérateur : `chaingo wallet new <nom>`.
- [ ] Obtenir ≥ **10 000 CGO** de test (faucet).
- [ ] **Sauvegarder la seed** hors-ligne, chiffrée (perte = perte du validateur).
- [ ] Staker : `chaingo stake --from <nom> --amount 10000`.
- [ ] (Optionnel) Publier un profil : `chaingo validator-profile …` (nom/site).
- [ ] Vérifier l'entrée dans le **set actif** (dashboard ou `/v1/validators`).

### 4. Exploitation
- [ ] **Surveiller la production de blocs** : ton validateur doit proposer à son
      tour (sinon jail pour inactivité → slashing 0,1 %).
- [ ] **Uptime** : un nœud hors-ligne manque ses slots de proposeur.
- [ ] **Ne jamais lancer deux nœuds avec la MÊME seed validateur** → double-signe
      → slashing 5 %. Une seed = une machine.
- [ ] Surveiller logs/espace disque ; plan de **mise à jour** coordonnée du binaire
      (gouvernance de version du protocole).

---

## Critères de réussite du jalon

- [ ] **≥ 4 validateurs** tenus par **≥ 4 entités distinctes** (clé de la sûreté
      BFT : aucune entité > 1/3 du stake).
- [ ] Le réseau **continue de finaliser** quand le nœud du mainteneur s'arrête
      (test de liveness réel).
- [ ] Un **reorg/partition** de test se résout proprement entre opérateurs
      indépendants.
- [ ] Stake **réparti** (pas 90 % sur une seule entité).

> Quand ces critères sont tenus, la case « décentralisation » de la roadmap passe
> au vert — c'est un prérequis dur du mainnet, indépendant du code.
