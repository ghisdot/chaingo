# Contribuer à ChainGO

Merci de votre intérêt ! ChainGO est ouvert à toutes les contributions. *(English speakers: contributions in English are welcome — open an issue or PR.)*

## Démarrer

```powershell
go build -o chaingo.exe ./cmd/chaingo
.\chaingo.exe node start --dev      # devnet local + site sur http://localhost:8545
.\chaingo.exe bench --txs 10000     # vérifier les performances
```

## Règles du projet

1. **Jamais de cryptographie pré-quantique.** Toute signature passe par `internal/crypto`
   (ML-DSA-65). Une PR introduisant ECDSA/Ed25519 sera refusée — c'est l'exigence n°1 du projet.
2. **Les règles économiques sont des `Params` de genèse** (`internal/types/params.go`),
   jamais des constantes en dur, et leur changement se discute en issue avant toute PR.
3. **Déterminisme absolu** dans tout ce qui touche l'état : même entrée → même racine
   d'état sur tous les nœuds. Pas d'horloge locale, pas d'aléatoire, pas d'itération de
   map non triée dans le chemin d'exécution.
4. La sérialisation signée (`SigningBytes`) est du JSON canonique : ne réordonnez pas
   les champs des structs `Transaction` / `BlockHeader`.
5. `go build ./... ; go vet ./...` doivent passer, et le bench ne doit pas régresser
   sous l'objectif de 1 500 TPS.

## Processus

- Ouvrez une **issue** pour discuter avant les gros changements (consensus, état, p2p).
- Les PR ciblées et testées sont fusionnées vite ; décrivez comment vous avez vérifié.
- Voir [ROADMAP.md](ROADMAP.md) pour les chantiers prioritaires.

## Sécurité

Vulnérabilité ? Ne l'exposez pas en issue publique — voir [SECURITY.md](SECURITY.md).
