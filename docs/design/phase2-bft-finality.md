# Design — Phase 2 : finalité BFT (issue #6)

## Problème

Aujourd'hui (Phase 1), un bloc est « commit » dès que le proposeur élu le produit.
Sécurité = confiance dans ce proposeur. Avec plusieurs validateurs, on veut qu'**un
bloc ne soit final que si une super-majorité du stake l'atteste** — tolérance aux
fautes byzantines (BFT) jusqu'à < 1/3 du stake malhonnête ou hors-ligne.

## Approche : un *finality gadget* par-dessus Aurora

On NE remplace PAS le mécanisme existant (proposeur déterministe + rounds de secours
pour la liveness). On ajoute une **couche de finalité** :

```
            production (Aurora, Phase 1)              finalité (Phase 2, ce design)
   proposeur élu ──► bloc commit localement ──► précommit signé par chaque validateur
                                                 │
                                  gossip des votes ▼
                       accumulation pondérée par le stake par (hauteur, hash)
                                                 │
                          ≥ 2/3 du stake total ? ▼
                                          hauteur FINALISÉE (irréversible)
```

- **Séparation liveness / sûreté.** La chaîne continue d'avancer (blocs produits) même
  si la finalité prend du retard. Un bloc passe par deux états : *committed* (appliqué,
  réversible en théorie) puis *finalized* (≥ 2/3 du stake l'a précommit, irréversible).
- **Vote = précommit.** `Vote{ChainID, Height, BlockHash, Voter, VoterPub, Signature}`,
  signé en **ML-DSA-65** (jamais d'autre schéma — invariant du projet).
- **Pouvoir de vote = poids du validateur** = `stake + délégations` (cohérent avec le
  tirage du proposeur).
- **Quorum** : `Σ pouvoir(voters pour (h, hash)) × 3 > 2 × pouvoir_total`. Strictement
  supérieur à 2/3.
- **Finalité = préfixe.** Un quorum à la hauteur `h` sur le hash de NOTRE bloc local
  finalise `h` et, transitivement (chaînage `prev_hash`), tous ses ancêtres. On suit
  `FinalizedHeight` (monotone croissant).

## Règles de comptage

1. Un vote n'est compté que si : signature valide, `voter` est un validateur actif
   (pouvoir > 0), `height > FinalizedHeight`.
2. Le quorum à `h` ne fait avancer `FinalizedHeight` que si le hash quorum == hash du
   bloc local à `h` (la finalité suit notre chaîne canonique).
3. Déduplication par hash de vote ; un validateur ne compte qu'une fois par (h, hash).

## Ce que cette tranche couvre / ne couvre pas

**Couvert (slice 1) :** type Vote signé, pool de votes pondéré, calcul du quorum 2/3,
émission automatique d'un précommit après chaque commit local, gossip P2P des votes,
exposition de `finalized_height` (API + statut).

**Hors périmètre (tranches suivantes, toujours #6/#7) :**
- Verrouillage Tendermint complet (prevote + precommit + POL, locking/unlocking) :
  ici un seul tour de précommit, pas de protection contre certains scénarios de
  partition. La double-proposition reste détectable et sera **slashée** (#7).
- Choix de fourche entre deux chaînes concurrentes toutes deux finalisées (impossible
  si < 1/3 byzantin — garantie BFT — mais à durcir).
- Ensemble de validateurs figé par hauteur (ici : ensemble courant ; OK tant que le
  set bouge lentement, à durcir).

Ces limites sont assumées et listées pour rester honnête : c'est un premier palier BFT
vérifiable, pas encore un consensus BFT complet audité.
