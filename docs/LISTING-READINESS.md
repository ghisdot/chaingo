# Checklist — « listing-readiness » (étalée dans le temps)

Honnête d'emblée : ChainGO est une **L1 souveraine** (pas un ERC-20). Se faire
« lister » ne se décrète pas — c'est un parcours **étagé** qui se compte en
**mois**, gated par le mainnet, la décentralisation et la liquidité. Voici l'ordre
réaliste.

---

## Étape 0 — Prérequis (avant toute idée de listing)

- [ ] **Mainnet en ligne et stable** (le testnet ne se liste pas).
- [ ] **Validateurs indépendants** ([VALIDATOR-CHECKLIST.md](VALIDATOR-CHECKLIST.md)) —
      une chaîne mono-opérateur n'est pas listable.
- [ ] **Distribution du token claire et publiée** (genèse, vesting on-chain,
      part équipe/communauté/trésorerie — déjà documentée dans
      [MAINNET.md](MAINNET.md)).
- [ ] **Explorateur public** + **API publique stable** (les agrégateurs et
      exchanges en ont besoin pour indexer supply, transactions, etc.).

## Étape 1 — Agrégateurs de données (le premier vrai « listing »)

CoinGecko / CoinMarketCap : le palier le plus accessible. Demandent typiquement :

- [ ] Mainnet live + **explorateur** + **API** publics.
- [ ] **Supply circulante vérifiable** (endpoint dédié, ex. `/v1/supply`).
- [ ] **Site web**, logo, descriptions, liens sociaux, dépôt open-source.
- [ ] Un **marché actif** (même petit) où le token s'échange — voir Étape 2.
- [ ] Formulaire de soumission rempli, contacts de l'équipe.

> Effet : prix/supply/volume visibles publiquement. C'est ce que les gens
> appellent souvent « être listé », sans exchange.

## Étape 2 — Liquidité (indispensable avant tout exchange)

Une L1 souveraine n'a pas de marché par défaut. Deux voies :

- [ ] **Pont (bridge) + CGO wrappé** vers une chaîne établie (ex. un wCGO) pour
      créer de la liquidité sur un **DEX** existant ; **ou**
- [ ] **Marché natif** (ton propre carnet d'ordres / AMM) avec de la liquidité
      d'amorçage.
- [ ] **Liquidité réelle** verrouillée (timelock/vesting — les contrats ChainGO
      le font), pour la crédibilité et l'anti-rug.

## Étape 3 — CEX (la barre haute)

Un exchange centralisé doit **intégrer ta chaîne** (faire tourner un nœud,
brancher dépôts/retraits). Attendus quasi systématiques :

- [ ] **Volume & liquidité** démontrés (issus des étapes 1-2).
- [ ] **Communauté active** et réelle (pas de chiffres gonflés).
- [ ] ⚠️ **Audit de sécurité par un cabinet tiers** — la plupart des CEX
      l'exigent pour leur due diligence. **C'est ici que la décision de zapper
      l'audit externe revient** : pas pour ta confiance, mais comme **exigence
      imposée**. À budgéter si le listing CEX est un objectif sérieux.
- [ ] **Clarté légale** : classification du token (utility vs security) selon ta
      juridiction, KYC/AML de l'équipe, parfois entité légale.
- [ ] **Intégration technique** : doc d'intégration nœud, finalité claire
      (profondeur de confirmation), stabilité prouvée.
- [ ] Souvent des **frais de listing** et/ou du market-making.

---

## Réalité & séquencement

```
Mainnet → Validateurs indépendants → Liquidité (bridge/DEX) → Agrégateurs (CG/CMC) → CEX (+ audit tiers)
```

- Ce qui est **gratuit et bientôt faisable** : agrégateurs (dès mainnet +
  liquidité minimale).
- Ce qui **coûte et prend des mois** : CEX (audit tiers, légal, liquidité).
- **Piège** : payer un « listing rapide » sur un exchange obscur ou un
  market-maker douteux. Ne fais pas ça — ça brûle la crédibilité durement gagnée
  par ton approche honnête.

## Ce qui aide, et que tu as déjà

- ✅ Code open-source, **rapport de sécurité** public, roadmap honnête → bon signal
  de due diligence.
- ✅ Token aux **règles transparentes** (genèse, pas de premine caché).
- ✅ Un **récit différenciant** (post-quantique natif) — rare, et vendeur auprès
  d'un public technique.

> Prochain pas concret côté listing : **rien**, tant que le mainnet et les
> validateurs indépendants ne sont pas là. Concentre l'énergie sur Étape 0.
