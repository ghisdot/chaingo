# Design — Mises à jour réseau & gouvernance des versions

> Comment pousser une mise à jour (feature ou correctif de sécurité) sans laisser
> traîner de nœuds vulnérables ni casser le réseau.

## Principe : 3 cas distincts

| Type de mise à jour | Le nœud pas à jour… | Mécanisme |
|---|---|---|
| **Consensus** (règle de validation, format signé) | décroche / fork tout seul (blocs « invalides » pour lui) | **Flag day** : activation par hauteur |
| **Hors-consensus** (ex : correctif DoS d'un décodeur) | reste compatible mais **vulnérable** | **Kick au handshake** (version de protocole) |
| **Réseau/P2P** (nouveau message, format de frame) | ne comprend pas → erreurs | **Version de protocole** |

Point clé : un changement de **consensus** est **auto-imposé** — un nœud pas à
jour ne peut pas valider les nouveaux blocs et sort de la chaîne tout seul. Le
risque n'est donc pas le retardataire qui « pollue », mais le **split** si
l'upgrade n'est pas coordonné → d'où le flag-day.

## Livré : version de protocole + kick + alerte

- `p2p.ProtocolVersion` (protocole binaire actuel = **v2**) est annoncée dans le
  `Hello`. `p2p.MinPeerProtocol` = version minimale acceptée.
- **KICK** : un pair sous `MinPeerProtocol` est **déconnecté** au handshake. Un
  retardataire ne peut pas rester connecté → forcé de se mettre à jour. C'est le
  « bloquer les nœuds pas à jour », au niveau réseau.
- **ALERTE bidirectionnelle** :
  - on logue chaque kick (`[p2p] KICK … protocole vN < minimum vM`) ;
  - si un pair annonce une version **supérieure** à la nôtre, c'est NOUS qui
    sommes en retard → log `⚠ NŒUD PAS À JOUR` + `Server.Outdated()` = true,
    exposé dans `/v1/status` (`outdated`), affiché en **bannière rouge** sur le
    dashboard validateur.
- Rétro-compat de transition : un ancien `Hello` (sans le champ version) se
  décode en v0 (legacy) → kické ; un ancien nœud échoue à lire notre `Hello`
  plus long → se déconnecte. La transition exige donc une **maj coordonnée**
  (acceptable au stade testnet, comme les autres changements de format).

## À venir : flag-day pour les changements de consensus

Pour qu'un changement de règle de consensus bascule **proprement et ensemble** :
ajouter une `activation_height` (dans les Params/genèse) — « à partir du bloc N,
la nouvelle règle s'applique ». Tout le monde met à jour AVANT N ; à N, les
nœuds à jour basculent ensemble, les retardataires forkent sur une chaîne morte.
Déterministe, pas de surprise (modèle Bitcoin/Ethereum).

## Politique d'auto-update

**Pas d'auto-update imposé aux validateurs tiers** : auto-updater un logiciel de
consensus est dangereux (un mauvais update = panne réseau globale ; vecteur
supply-chain). On **annonce**, on **kicke** les trop vieux, on **alerte** les
retardataires ; les opérateurs mettent à jour **manuellement avant le flag-day**.

Acceptable en revanche pour **nos propres** nœuds de testnet : un timer systemd
qui récupère une **release signée** (ML-DSA, cohérent avec le projet) puis
rebuild/restart — sans l'imposer aux autres.

## Procédure type d'une mise à jour

1. Développer + tester (CI verte sur les plateformes).
2. Si changement de consensus : fixer une `activation_height` future avec marge.
3. Bump `ProtocolVersion` (et `MinPeerProtocol` si on veut kicker les anciens).
4. Annoncer (canaux communautaires) la version + la hauteur d'activation.
5. Déployer les nœuds d'amorçage, puis les validateurs, AVANT la hauteur.
6. Les retardataires voient la bannière « EN RETARD » et/ou sont kickés.
