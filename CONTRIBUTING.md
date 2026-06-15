# Contribuer à ChainGO

ChainGO est un projet open source (MIT) et accepte les contributions de la
communauté. *English speakers: contributions in English are welcome — please
open an issue or PR in either language.*

## Démarrer en local

```bash
git clone https://github.com/ghisdot/chaingo
cd chaingo
go build -o chaingo ./cmd/chaingo

./chaingo node start --dev        # nœud de développement local + faucet
./chaingo bench --txs 10000       # mesurer le débit local
go test ./...                     # suite de tests
```

Sur Windows, remplacer par `go build -o chaingo.exe ./cmd/chaingo` puis
`.\chaingo.exe ...`.

## Règles du projet (invariants à respecter)

1. **Aucune cryptographie pré-quantique.** Toute signature passe par
   `internal/crypto` (ML-DSA-65). Une PR introduisant ECDSA, Ed25519 ou
   tout autre schéma vulnérable au calcul quantique sera refusée — c'est
   l'exigence fondatrice du projet.
2. **Les règles économiques sont des `Params` de genèse**
   ([`internal/types/params.go`](internal/types/params.go)), jamais des
   constantes en dur. Toute évolution des paramètres se discute en issue
   avant la PR.
3. **Déterminisme absolu** dans le chemin d'exécution de l'état : même
   entrée → même racine d'état sur tous les nœuds. Pas d'horloge locale,
   pas d'aléatoire, pas d'itération de map non triée dans
   `internal/state/`, `internal/consensus/`, `internal/types/`.
4. **Sérialisation canonique** : `SigningBytes` (signature des
   transactions, votes, blocs) ne doit pas être affectée. Ne pas réordonner
   les champs des structs `Transaction`, `Block.BlockHeader`, `Vote` sans
   migration explicite.
5. **CI verte avant merge** : `go build ./...`, `go vet ./...` et
   `go test ./...` doivent passer. Le bench `chaingo bench` ne doit pas
   régresser sous l'objectif de 1 500 TPS.

## Processus de contribution

1. **Pour les petits changements** (correction de typo, doc, refactor
   local) : ouvrir directement une PR avec une description claire et un
   test si applicable.
2. **Pour les changements significatifs** (consensus, P2P, schéma d'état,
   économie) : ouvrir d'abord une **issue de design** pour discuter de
   l'approche avant de coder. Cela évite les PR qui partent dans la
   mauvaise direction.
3. **Décrire la vérification** dans le corps de la PR : tests ajoutés,
   benchs lancés, scénarios manuels couverts.
4. Les commits sont signés par leur auteur (utilisez votre identité GitHub
   habituelle).

Les chantiers prioritaires sont listés dans [ROADMAP.md](ROADMAP.md) et
les issues ouvertes sur GitHub.

## Conventions de code

- **Go** standard, `gofmt` appliqué.
- Commentaires utiles : le **pourquoi**, pas le **quoi** que le code dit déjà.
- Tests dans le même paquet (`_test.go`), nommés `TestFoo...`.
- Pour les changements de consensus / état : ajouter un test déterministe
  dans le paquet correspondant.

## Sécurité

Vulnérabilité de sécurité ? **Ne l'ouvrez pas en issue publique** —
suivez la procédure décrite dans [SECURITY.md](SECURITY.md).

## Documentation

- [API REST](docs/API.md) — référence des endpoints
- [Opérateur de nœud](docs/TESTNET-DEPLOY.md) — déploiement
- [Validateur](docs/VALIDATOR.md) — staking et délégation
- [Mainnet](docs/MAINNET.md) — préparation du lancement
- [Roadmap](ROADMAP.md) — état et priorités
