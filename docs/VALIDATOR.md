# Devenir validateur ou délégateur

ChainGO offre deux façons de sécuriser le réseau et de gagner des
récompenses (~3 %/an sur le stake total).

---

## Option 1 — Validateur (≥ 10 000 CGO + un nœud 24/24)

**Pré-requis** : un nœud ChainGO en service permanent. Voir
[TESTNET-DEPLOY.md](TESTNET-DEPLOY.md) pour la procédure d'installation.
La clé du validateur est la seed pointée par `--validator-seed` (ou
`validator.seed` dans le datadir en mode `--dev` / `--testnet`).

### Activer le rôle de validateur

Depuis le wallet correspondant à cette seed :

```bash
chaingo stake --from <wallet> --amount 10000
```

À partir de là, le consensus tire le validateur au sort à chaque bloc,
proportionnellement à son poids (`stake + délégations reçues`). À chaque
bloc proposé : récompense d'émission + tips des transactions.

### Risques

#### Hors-ligne : jail + slash léger

Les **rounds de secours** redonnent la main à un autre validateur quand le
proposeur élu ne répond pas. Chaque tour manqué incrémente un compteur. Au
seuil (`downtime_jail_threshold`), le validateur est **jailé** :

- exclu du tirage de proposeur,
- exclu du calcul de finalité (son pouvoir devient 0),
- **slashé de 0,1 %** (`slash_downtime_bps`), brûlé sur stake **et**
  délégations reçues.

Sortie : `chaingo unjail --from <wallet>` après écoulement de `jail_seconds`.
Un nœud qui produit régulièrement voit son compteur remis à 0 — ne jamais
être jailé est facile avec un bon uptime.

#### Double-signature : slash immédiat 5 %

Si un nœud précommit deux blocs différents à la même hauteur (équivocation —
le scénario typique : deux instances faisant tourner la même seed en
parallèle), la preuve est incluse on-chain par n'importe quel autre nœud
honnête. **5 % du stake propre ET des délégations sont brûlés**
(`slash_double_sign_bps`), de façon idempotente.

> ⚠️ **Ne jamais faire tourner deux nœuds avec la même `validator.seed`.**
> Pour la haute disponibilité, utiliser un seul nœud actif et un nœud
> passif de secours dont la seed reste **éteinte** sauf bascule manuelle.

### Sortir du rôle de validateur

```bash
chaingo unstake --from <wallet> --amount 10000
```

Les fonds passent en **unbonding** (21 jours sur mainnet, 24 h sur testnet),
puis redeviennent liquides automatiquement. Si tout le stake est retiré,
les délégations reçues sont automatiquement libérées (en unbonding aussi,
les délégateurs récupèrent leurs fonds).

---

## Option 2 — Délégateur (dès 1 CGO, aucun nœud à faire tourner)

Les holders qui ne veulent pas opérer un nœud peuvent **déléguer** leur
poids à un validateur existant et toucher leur part de ses récompenses, au
pro-rata, à chaque bloc qu'il propose, moins sa commission (10 %).

```bash
# Lister les validateurs actifs
curl https://node.chaingo.org/v1/validators

# Déléguer
chaingo delegate --from <wallet> --to cg<adresse-validateur> --amount 50

# Vérifier les récompenses (créditées au solde, bloc après bloc)
chaingo balance <wallet>

# Récupérer ses fonds (unbonding 21 j sur mainnet, 24 h sur testnet)
chaingo undelegate --from <wallet> --to cg<adresse-validateur> --amount 50
```

### Garanties et risques

- **Les CGO délégués ne quittent jamais le compte du délégateur** : la
  délégation est un poids comptable, le validateur ne peut pas les dépenser.
- **Le slashing s'applique aussi aux délégations.** Si le validateur choisi
  est slashé pour double-signature, les délégateurs perdent 5 % de leur
  délégation (brûlée). Sélectionner un validateur sérieux : un seul nœud
  actif, bon uptime, identité publique vérifiable.
- Si le validateur sort du set actif (unstake complet ou jail prolongé),
  les fonds délégués passent en unbonding et sont récupérés après le délai.

---

## Paramètres économiques

Tous les paramètres sont visibles en temps réel via l'endpoint
`GET /v1/fees` du nœud — exemple sur le testnet public :

```bash
curl https://node.chaingo.org/v1/fees
```

| Paramètre | Mainnet (prévu) | Testnet |
|---|---|---|
| Stake minimum validateur | 10 000 CGO | 10 000 CGO |
| Délégation minimum | 1 CGO | 1 CGO |
| Commission validateur sur récompenses des délégateurs | 10 % | 10 % |
| Émission annuelle sur le stake | ~3 % | ~3 % |
| Unbonding | 21 jours | 24 heures |
| Slash double-signature | 5 % | 5 % |
| Slash downtime | 0,1 % | 0,1 % |
