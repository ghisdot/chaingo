# Design — Verrouillage POL (locking) — #6

> Le morceau le plus sensible de la Phase 2. Ce document pose la décision
> d'architecture AVANT d'écrire la moindre ligne, parce qu'une erreur ici =
> double-dépense. Pré-requis livré : set de validateurs figé par hauteur (#5).

## Rappel du modèle actuel

ChainGO fait aujourd'hui de la **finalité par attestation** :

1. Le proposeur élu produit le bloc H et le commit localement.
2. Chaque validateur, en voyant H, émet **un** prevote **et un** précommit pour H.
3. Quand le bloc H+1 embarque ≥ 2/3 de précommits sur H (`LastCommit`), H est finalisé.

Il n'y a **pas de rounds de vote internes à une hauteur**. Le champ `Round` de
l'en-tête sert aux **proposeurs de secours** (liveness : si l'élu du round 0 est
hors-ligne, l'élu du round 1 propose après un intervalle) — ce n'est PAS un round
de vote Tendermint.

Garde-fou actuel (`castVoteKind`) : un nœud **refuse de signer un 2ᵉ vote du même
kind à la même hauteur**. Sans round dans le vote, changer de précommit = produire
sa propre preuve d'équivocation → auto-slash. C'est sûr (fail-safe) mais **bloque
tout reorg** : un nœud qui a committé une branche minoritaire ne peut pas rallier
la majoritaire (perte de liveness sur ce nœud, jamais de perte de sûreté).

## Ce que le verrouillage doit garantir

**Jamais deux blocs finalisés en conflit à la même hauteur**, ET la possibilité de
**rallier la branche légitime** sans s'auto-slasher. Mécanique Tendermint :

- À ≥ 2/3 de **prevotes** pour un bloc B à un round r → c'est une *polka* (Proof-of-
  Lock, POL). Le validateur se **verrouille** sur (r, B) et précommit B.
- Il ne **déverrouille** (et ne précommit un autre bloc B′) que s'il voit une polka
  pour B′ à un round **strictement supérieur**. La preuve d'un quorum plus récent
  justifie le changement — donc pas d'équivocation punissable.

## La décision d'architecture

Le verrouillage suppose des **rounds de vote**. ChainGO n'en a pas. Deux chemins :

### Chemin A — réécriture Tendermint complète
State machine par hauteur : `propose → prevote → precommit → commit`, avec timeouts
par round, incrément de round sur timeout, etc. C'est le manuel. **Coût** : remplace
le modèle « produire-puis-attester » actuel (élégant et qui tourne en prod) par une
boucle de consensus synchrone à timeouts. Risque élevé, plusieurs semaines, et on
jette du code qui marche.

### Chemin B — greffer le POL sur les rounds de secours existants ✅ recommandé
Les rounds de secours **produisent déjà des blocs candidats concurrents à la même
hauteur** (proposeur du round 0 vs round 1…). On réutilise ce numéro de round comme
round de vote :

- On ajoute `Round` au `Vote` (= round de l'en-tête du bloc voté).
- On suit les prevotes par `(hauteur, round, hash)` et on détecte les polkas.
- Verrou par hauteur : `(lockedRound, lockedHash)`. Un validateur ne précommit un
  hash concurrent que sur polka à un round **> lockedRound**.
- L'**équivocation devient consciente du round** : deux précommits à la même hauteur
  ET au même round pour des hash différents = faute (slash). À des rounds différents
  AVEC justification POL = changement légitime, pas de slash. (Rejoint #8.)

**Pourquoi B** : il épouse l'architecture existante (les rounds de secours SONT déjà
notre mécanisme de blocs concurrents), garde la séparation liveness/sûreté, et
transforme le garde-fou « anti-2ᵉ-vote » binaire en règle de verrouillage nuancée,
sans réécrire la production de blocs.

## Changement de format `Vote` (cassant)

`Vote` gagne un champ `Round uint32`. Comme `SigningBytes` est du JSON canonique et
que l'ordre des champs EST le format signé, ajouter `Round` **invalide les anciennes
signatures de vote** → mise à niveau coordonnée de tous les nœuds (acceptable au
stade testnet, comme le passage P2P binaire). Le codec binaire du Vote (tranche 2)
gagne aussi le champ.

## État : LIVRÉ (chemin B, reorg du sommet)

Tranches 1-5 livrées et testées. Le fork-choice bascule vers un bloc concurrent
**au sommet** s'il porte une polka à un round supérieur ; validé par un **test de
simulation de partition** (`TestForkChoiceConvergesViaHigherRoundPolka`) : un
nœud minoritaire sur B0 (round 0) reorg vers B1 (round 1) dès qu'il voit la
polka, et les 4 nœuds convergent sans double-finalité.

**Scope assumé** : reorg du **sommet** (1 bloc). C'est le cas dominant car la
finalité ne traîne que d'un bloc. Un fork **enterré** (plusieurs blocs non
finalisés divergents) est hors périmètre — il est ignoré, et le système reste
fail-safe (jamais de double-finalité). Le reorg multi-blocs est un durcissement
ultérieur (rejouer une branche de N blocs depuis le point de fork).

## Tranches d'implémentation (chacune testée, commitée séparément)

1. **`Vote.Round`** : champ + signing bytes + codec binaire + maj des appels. Tests :
   round-trip signature, sérialisation. *(cassant — annoncé.)*
2. **Suivi des prevotes par (hauteur, round, hash) + détection de polka** dans le
   `votePool`. Tests : polka détectée à ≥ 2/3, pas en-dessous, par round.
3. **État de verrou par hauteur** `(lockedRound, lockedHash)` + règle de lock/unlock
   dans l'émission de vote (remplace le garde-fou binaire actuel). Tests : on ne
   change de précommit que sur polka de round supérieur ; sinon on reste verrouillé.
4. **Équivocation consciente du round** (même hauteur+round, hash ≠ ⇒ faute ;
   round ≠ ⇒ légitime). Rejoint #8 (slash de prevote équivoque). Tests : faute
   détectée intra-round, changement inter-round non puni.
5. **Fork-choice piloté par les verrous** (#7) : choisir la branche couverte par la
   polka de plus haut round ; reorg borné au dernier finalisé (immuable). Tests :
   scénario de partition dans le harnais `simNet` → convergence sans double-finalité.

> #7 (fork-choice) est listé ici comme tranche 5 car il est indissociable du verrou :
> les verrous SONT l'entrée du fork-choice. #5 (set figé) est déjà livré et fournit le
> dénominateur stable des quorums utilisés par les polkas.

## Invariants préservés

- Toute signature en ML-DSA-65 (jamais d'autre schéma).
- `Execute(strict=true)` reste atomique (snapshot/restore).
- Le timestamp du bloc reste l'horloge des règles.
- Une hauteur finalisée n'est jamais révertie (le reorg est borné au-dessus du
  dernier finalisé).
