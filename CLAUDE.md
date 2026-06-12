# ChainGO — notes pour Claude

Blockchain post-quantique en Go. Voir README.md pour l'architecture complète et la feuille de route (Phases 2-5).

## Commandes

- Go est installé via winget : `C:\Program Files\Go\bin\go.exe` (peut ne pas être dans le PATH de la session).
- Build : `go build -o chaingo.exe ./cmd/chaingo` (⚠ Windows verrouille le binaire : arrêter le nœud avant de rebuilder)
- Devnet : `.\chaingo.exe node start --dev --datadir .devnet\node1 --api 127.0.0.1:8545 --p2p 127.0.0.1:9000`
- Bench TPS : `.\chaingo.exe bench --txs 10000` (~31 000 TPS mesurés, objectif 1 500)
- Pas de tests unitaires encore — la vérification se fait par bench + smoke test API/CLI.

## Invariants à respecter

- **Toute signature** passe par `internal/crypto` (ML-DSA-65). Ne jamais introduire ECDSA/Ed25519 : la résistance quantique est l'exigence n°1 du projet.
- La sérialisation de signature (`SigningBytes`) est du JSON canonique : l'ordre des champs des structs EST le format — ne pas réordonner les champs de `Transaction`/`BlockHeader` sans migration.
- La racine d'état dépend de `encoding/json` qui trie les clés de map — déterminisme requis entre nœuds.
- **Les règles économiques sont des Params de genèse** (`types.Params`, stockées dans l'état), décidées avec l'utilisateur le 2026-06-12 : inflation 300 bps/an sur le stake, frais EIP-1559 (base fee dynamique brûlé + tip marché), stake min 10 000 CGO, unbonding 21 j (5 min en devnet). Ne jamais réintroduire de constante économique codée en dur.
- `state.Execute(strict=true)` (blocs reçus) doit rester atomique : snapshot/restore en cas d'échec. Le timestamp du bloc (header) est l'horloge des règles (unbonding) — jamais l'horloge locale.
- Le proposeur est déterministe : `SelectProposer(height, prevHash, round)` pondéré par le stake. Les rounds > 0 sont les proposeurs de secours (liveness si un validateur staké est hors-ligne) ; un bloc est valide si son proposeur correspond à un round < consensus.MaxRounds. Toute modification casse le consensus entre versions.

## Données locales

- `.devnet/` : datadirs de test (db bbolt, seeds validateur/faucet, genesis.json) — jetable.
- Keystore wallets : `~/.chaingo/keystore/*.json` (scrypt + AES-GCM).
