# Design — Sûreté BFT : invariants, état actuel, chemin vers le verrouillage complet

Ce document cadre le sujet le plus sensible du projet — la **sûreté du consensus** —
et le distingue honnêtement de ce qui est livré.

## Modèle actuel (livré)

- Proposeur déterministe pondéré par le stake + rounds de secours (liveness).
- Un seul tour de vote : **précommit** signé ML-DSA-65.
- **Finalité = commit ≥ 2/3 porté par le bloc suivant** (`LastCommit`), persistée et
  vérifiable depuis la chaîne (décale d'un bloc).
- Slashing de l'équivocation (double-signature) et de l'inactivité.

## Propriété de sûreté actuelle : « fail-safe », pas « toujours vivant »

Le système **ne corrompt jamais l'état silencieusement** :
- Un bloc en conflit (même hauteur déjà committée, ou `prev_hash` qui ne correspond pas)
  est **rejeté** par `ApplyExternalBlock` — jamais écrasé.
- Une hauteur finalisée n'est jamais révertie.
- **Auto-équivocation impossible** : un nœud n'émet jamais deux précommits à la même
  hauteur (garde-fou `castVote` + carte `voted`). Sans ce garde-fou, un nœud qui
  reprocesserait une hauteur signerait un 2ᵉ vote et se ferait slasher lui-même.

**Limite assumée :** sous asynchronie/partition, deux blocs valides peuvent coexister au
*sommet* (round 0 du proposeur élu vs round de secours). Les nœuds qui ont committé la
branche minoritaire **se bloquent** (rejettent la branche majoritaire faute de
fork-choice) — perte de **liveness** sur ces nœuds, **pas** de perte de sûreté. La
finalité (≥ 2/3) ne peut désigner qu'un seul bloc par hauteur (impossible d'avoir deux
quorums 2/3 avec < 1/3 byzantin), donc jamais deux blocs finalisés en conflit.

## Pourquoi on ne « bricole » pas un reorg naïf

Faire basculer un nœud d'une branche à l'autre impose de **changer son précommit** pour
la hauteur contestée. Or re-signer un autre hash à la même hauteur = **auto-équivocation**
→ auto-slash. C'est précisément le problème que le **verrouillage (locking) de Tendermint**
résout. Un reorg naïf serait donc *moins* sûr que l'état actuel.

## Chemin vers la sûreté complète (Tendermint-like)

Deux tours de vote par hauteur/round, avec verrouillage :

1. **Propose** : le proposeur du round propose un bloc.
2. **Prevote** : chaque validateur prévote le bloc (ou nil).
3. **Lock** : à ≥ 2/3 de prévotes pour un bloc B, le validateur se **verrouille** sur B et
   précommit B. Il ne précommettra un autre bloc que s'il voit ≥ 2/3 de prévotes pour cet
   autre bloc à un **round strictement supérieur** (preuve de verrou plus récente, *POL*).
4. **Precommit / Commit** : à ≥ 2/3 de précommits pour B, B est committé et final.

Le verrouillage garantit qu'on ne change de précommit que sur preuve d'un quorum plus
récent — d'où : **jamais deux blocs finalisés en conflit, et reorg sûr** vers la branche
légitime (sans auto-équivocation).

### Tranches d'implémentation prévues
1. Type `Prevote` (distinct du précommit) + gossip.
2. Suivi par hauteur/round : prévotes reçues, verrou courant (round + hash).
3. Règle de verrouillage dans la production et le vote.
4. Fork-choice piloté par les verrous (POL) + reorg borné (jusqu'au dernier finalisé,
   immuable).
5. Set de validateurs figé par hauteur (vérification des quorums contre le set d'époque).
6. Tests d'intégration des scénarios de partition/fork dans le harnais `simNet`.

> Le set figé par hauteur et le fork-choice n'ont de sens qu'avec le verrouillage : ils
> seront livrés ensemble, après les tranches 1-3.
