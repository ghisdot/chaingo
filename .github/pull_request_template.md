<!-- FR ou EN welcome -->

## Ce que fait cette PR / What this PR does



## Comment c'est vérifié / How it was verified
<!-- go build ./... && go vet ./... passent ; bench non régressé ; smoke test API/CLI -->
- [ ] `go build ./...` et `go vet ./...` passent
- [ ] `chaingo bench` ne régresse pas sous 1 500 TPS
- [ ] testé manuellement (commande / appel API) :

## Checklist invariants (voir [CONTRIBUTING.md](../CONTRIBUTING.md))
- [ ] Aucune crypto pré-quantique introduite (tout passe par `internal/crypto`)
- [ ] Pas de constante économique en dur (règles = `types.Params` de genèse)
- [ ] Déterminisme préservé (pas d'horloge locale / aléatoire / map non triée dans l'exécution)
- [ ] Ordre des champs de `Transaction`/`BlockHeader` inchangé (sinon migration documentée)
