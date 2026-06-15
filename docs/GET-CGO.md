# Obtenir des CGO

## Sur le testnet public (`chaingo-testnet-1`)

Le testnet est un réseau de test : les CGO obtenus ici n'ont **aucune valeur
monétaire**. Ils servent à expérimenter les transferts, créer des tokens, des
contrats, déléguer à des validateurs, et stresser le réseau.

### Méthode 1 — depuis le wallet web (recommandée, sans installation)

1. Ouvrir [chaingo.org/wallet](https://chaingo.org/wallet/).
2. Créer un wallet (un mot de passe chiffre la seed dans le navigateur, rien
   n'est envoyé à un serveur).
3. Cliquer sur le bouton **Faucet** (ou démarrer directement depuis le site)
   — le solde est crédité en quelques secondes.

### Méthode 2 — depuis la CLI

```bash
chaingo wallet new <nom>
chaingo faucet --to <nom> --amount 100 --api https://node.chaingo.org
chaingo balance <nom> --api https://node.chaingo.org
```

### Méthode 3 — appel API direct (curl)

```bash
curl -X POST https://node.chaingo.org/v1/dev/faucet \
  -H 'Content-Type: application/json' \
  -d '{"address":"<cg-adresse>","amount":100000000000}'
```

Le montant est exprimé en **ucgo** (1 CGO = 10⁹ ucgo). L'exemple ci-dessus
envoie 100 CGO.

### Limites du faucet testnet

Le faucet est ouvert sur le testnet (à dessein, pour faciliter les tests),
mais des limites peuvent être appliquées à tout moment pour prévenir l'abus.
Si la requête échoue, réessayer plus tard ou rejoindre le canal de support
de la communauté.

---

## Sur le mainnet (pas encore lancé)

Le mainnet ChainGO **n'est pas encore en service**. Son lancement est
conditionné par :

1. la finalisation de la Phase 2 de la roadmap (sécurité de production),
2. la réussite d'un audit de sécurité externe,
3. l'engagement d'au moins 4 validateurs indépendants,
4. la finalisation du document de genèse.

Voir [ROADMAP.md](../ROADMAP.md) pour l'avancement, et [MAINNET.md](MAINNET.md)
pour la procédure complète.

### Distribution prévue au lancement

| Part | CGO | Allocation |
|---|---|---|
| 50 % | 500 M | **Communauté** — airdrops aux testeurs du testnet, programmes d'adoption, récompenses early holders |
| 20 % | 200 M | **Trésorerie** — financement du développement, audits, infrastructure |
| 15 % | 150 M | **Équipe** — vesting 4 ans on-chain (verrouillé dès le bloc 0) |
| 10 % | 100 M | **Écosystème** — partenariats, grants aux développeurs |
| 5 %  | 50 M  | **Genèse et liquidité** — stakes des validateurs initiaux et amorce de marché |

### Comment participer aux airdrops communauté

La méthode la plus simple pour être éligible aux distributions
post-lancement : **utiliser activement le testnet**.

- Créer un wallet et faire des transferts.
- Tester les contrats no-code (vesting, escrow, multisig).
- Faire tourner un nœud, voire devenir validateur testnet (voir
  [VALIDATOR.md](VALIDATOR.md)).
- Contribuer au code source (voir [CONTRIBUTING.md](../CONTRIBUTING.md)).

Les modalités exactes des airdrops seront publiées avant le lancement du
mainnet, sur le site officiel.

### Listings d'échanges

Aucun listing n'est encore en place. Les démarches commenceront après le
lancement du mainnet et la stabilisation du réseau. Toute annonce passera
par les canaux officiels du projet — méfiance des intermédiaires qui
prétendraient vendre du CGO en pré-mainnet.

---

## Ressources

- [Wallet web](https://chaingo.org/wallet/) · [Explorateur](https://chaingo.org/explorer/) · [API](https://chaingo.org/api/)
- [Guide validateur & délégateur](VALIDATOR.md)
- [Préparation du mainnet](MAINNET.md)
- [Feuille de route](../ROADMAP.md)
