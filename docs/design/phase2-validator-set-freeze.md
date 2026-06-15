# Design — Set de validateurs figé par hauteur (#5)

> Tranche du durcissement BFT. Pré-requis du verrouillage POL (#6) et du fork-choice (#7).

## Problème

Les vérifications de quorum BFT (un commit doit réunir > 2/3 du pouvoir actif)
lisaient le pouvoir des validateurs depuis l'état **vivant** :

- `verifyCommit` : `state.PowerOf(voter)` et `state.TotalPower()`
- `AddVote` : `state.PowerOf(voter)` pour accepter/refuser un vote
- `buildLastCommit` : `state.TotalPower()` comme dénominateur du 2/3

Or l'état change en continu (stake, unstake, jail, délégations). Quand on
vérifie le commit du bloc H — porté par le bloc H+1, donc évalué *après* que
l'état a déjà avancé — le dénominateur `TotalPower()` et le pouvoir de chaque
votant peuvent **différer de ce qu'ils étaient à la hauteur H**.

Conséquences possibles : un commit légitimement ≥ 2/3 à H rejeté plus tard
(perte de finalité/liveness), ou un quorum mal évalué si le set a grossi/réduit.
Pour un consensus qui protège de l'argent réel, **le 2/3 doit se mesurer contre
un set figé, identique pour tous les nœuds**.

## Définition

> **Set votant de la hauteur H = ensemble des validateurs actifs tel qu'il est
> juste après l'application du bloc H-1** (état « post-(H-1) »).

C'est cohérent avec la sélection du proposeur : `SelectProposer(H, …)` est déjà
appelé quand l'état est à H-1, donc le proposeur de H est tiré du set post-(H-1).
Les précommits de H doivent réunir 2/3 de **ce même** set.

## Mécanique

- `state.SnapshotActiveSet()` renvoie une **photo immuable** `{powers, total}`
  des validateurs de pouvoir > 0.
- Le moteur garde `setByHeight : hauteur → *ValidatorSet`.
- À l'**entrée** du traitement d'un bloc H (production ou réception), avant
  `Execute(H)`, l'état est exactement post-(H-1) : on fige `setByHeight[H]`.
- Toutes les vérifications de quorum pour la hauteur H utilisent
  `setForHeight(H)` au lieu de l'état vivant.
- `setForHeight(H)` retombe sur une photo de l'état courant **si la hauteur
  n'est pas encore figée**. Ce cas n'arrive que (a) pour un vote reçu sur la
  hauteur suivante avant qu'on ait traité son bloc — où l'état courant EST le
  bon set — ou (b) juste après un redémarrage pour l'unique hauteur non encore
  finalisée. Le repli n'est donc jamais pire que le comportement actuel.
- On purge `setByHeight[h]` pour `h ≤ finalized` (fenêtre minuscule conservée :
  de `finalized+1` à `current+1`).

## Non-objectifs (tranches suivantes)

- Le **verrouillage** (lock/unlock sur preuve de quorum plus récent) : #6.
- Le **fork-choice** piloté par les verrous : #7.

Ce changement ne fait que rendre le dénominateur du quorum déterministe et
stable par hauteur — fondation nécessaire avant de raisonner sur les verrous.
