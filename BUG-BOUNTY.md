# Programme de bug bounty ChainGO

ChainGO assume une stratégie **« crypto maison + durcissement communautaire »**.
Ce programme **récompense** la communauté qui attaque le code et trouve de vraies
failles. Tout est open-source (Apache 2.0) et reproductible.

> ⚠️ **Statut & honnêteté.** ChainGO est en **testnet**. Le token CGO n'a pas
> encore de valeur de marché. Les récompenses sont donc en **CGO** (testnet, et
> **allocation mainnet** à la genèse pour les failles sérieuses trouvées avant le
> lancement) **+ crédit public**. Nous le disons franchement : ce programme vise
> d'abord les passionnés de sécurité et de cryptographie post-quantique, pas à
> remplacer un audit rémunéré en fiat.

---

## Comment signaler

- **N'ouvrez pas d'issue publique** pour une faille exploitable.
- Écrivez à **ghisdot@proton.me** (idéalement chiffré) avec :
  - l'impact et la classe de sévérité estimée,
  - une **procédure de reproduction** (PoC très apprécié),
  - le **commit** concerné.
- **Première réponse sous 72 h.** Divulgation **coordonnée** : la faille reste
  embargoée jusqu'au correctif. Crédit public au correctif (sauf demande
  contraire).

Voir aussi [SECURITY.md](SECURITY.md) et le
[rapport de revue de sécurité](docs/SECURITY-REVIEW.md).

---

## Classes de sévérité & récompenses

Les montants sont indicatifs (CGO testnet + allocation mainnet équivalente).

| Sévérité | Exemples | Récompense |
|---|---|---|
| **Critique** | Forger une preuve de dépense blindée pour une note inexistante · **créer de la valeur** (débordement, doublon de nullifier, conservation cassée) · minter/voler des fonds sans autorité · **casser la sûreté BFT** (deux blocs finalisés conflictuels) · forger une signature ML-DSA · **désaccord de racine d'état** entre nœuds honnêtes (split de chaîne) | **50 000 – 250 000 CGO** + hall of fame |
| **Haute** | Arrêt de liveness exploitable · contournement du slashing · action de contrat non autorisée (rôle/seuil/échéance) · erreur de comptabilité de supply (sans création de valeur) · double-dépense d'une note | **10 000 – 50 000 CGO** |
| **Moyenne** | DoS par épuisement de ressources · panique sur entrée malformée (codec/API/P2P) · erreur de frais (mineure, non exploitable en valeur) | **2 000 – 10 000 CGO** |
| **Basse** | Fuite d'info non critique · nit de déterminisme non exploitable · durcissement défensif | **crédit + 200 – 2 000 CGO** |

La sévérité finale est tranchée par l'équipe selon l'**impact réel** et la
**qualité du rapport** (repro fiable, analyse claire → palier supérieur).

---

## Périmètre

**Dans le périmètre** (code du dépôt `ghisdot/chaingo`) :

| Paquet | Pourquoi |
|---|---|
| `internal/crypto` | signatures ML-DSA-65, dérivation d'adresse, keystore |
| `internal/consensus` | sélection du proposeur, finalité BFT, slashing, reorg |
| `internal/state` | déterminisme, atomicité, règles éco, tokens, contrats |
| `internal/stark` | pile zk-STARK, circuit blindé, soundness, range-proofs |
| `internal/wasmvm` | déterminisme par gas, opcodes |
| `internal/p2p`, `internal/codec` | parsing d'entrées non fiables |
| `internal/genesis` | empreinte déterministe de la genèse |
| Wallet/Studio web (`web/`) | clé privée côté navigateur, signature locale |

**Hors périmètre** (limites **déjà connues et documentées**, cf.
[SECURITY-REVIEW.md §5](docs/SECURITY-REVIEW.md)) :

- Le fait que **Poseidon ne soit pas standardisé**, que la **soundness soit
  conjecturée** (et non prouvée), ou l'**absence d'audit tiers** — ce sont des
  choix connus, pas des bugs. En revanche, une **attaque concrète** exploitant
  l'un d'eux **est** dans le périmètre (et probablement critique).
- DoS du **faucet** public, spam applicatif, ingénierie sociale, accès physique,
  infra tierce (hébergeur, DNS), dépendances en dehors de notre contrôle.
- Tout test qui **dégrade le testnet public pour les autres** (faites tourner un
  **devnet local** : `chaingo node start --dev`).

---

## Règles

1. **Pas de mainnet** : il n'existe pas encore. Testez en local/devnet.
2. **Ne nuisez pas** aux autres utilisateurs du testnet public.
3. **Un rapport = une faille**, avec repro. Les rapports en double : le premier
   horodaté gagne.
4. **Pas d'exfiltration de données** d'autrui, pas de chantage.
5. Récompense versée après **correctif vérifié**, depuis une **trésorerie gérée
   on-chain** (multisig/DAO ChainGO — cf. ci-dessous), éventuellement en
   **vesting** pour les gros montants.

---

## Trésorerie du bounty (dogfooding)

Le pot de récompenses est détenu et versé via **les propres contrats no-code de
ChainGO**, ce qui le rend **public et auditable on-chain** :

- un **multisig M-of-N** (ou une **DAO**) détient les CGO du programme ;
- chaque paiement est une **proposition** votée par les signataires ;
- les gros montants peuvent être versés en **vesting**/**timelock** au chercheur.

> Créez-les depuis le studio (<https://chaingo.org/studio/>) ou en CLI
> (`chaingo contract multisig|dao …`). C'est aussi une démonstration vivante de
> la chaîne.

---

*Merci de rendre ChainGO plus solide. La sécurité post-quantique est un bien
commun.*
