# Politique de sécurité

## Statut

ChainGO est en phase **devnet** : logiciel expérimental, aucun réseau de valeur réelle
n'existe. Les protections de niveau production (votes BFT, slashing, audit externe)
sont planifiées en Phase 2 — voir [ROADMAP.md](ROADMAP.md).

## Signaler une vulnérabilité

- **N'ouvrez pas d'issue publique** pour une faille exploitable.
- Écrivez à : ghislain.ott@gmail.com avec le détail (impact, reproduction, version).
- Réponse sous 72 h ; divulgation coordonnée une fois le correctif publié.

## Périmètre particulièrement sensible

- `internal/crypto` — signatures ML-DSA-65, dérivation d'adresses
- `internal/state` — règles d'exécution, frais, supply (tout écart de déterminisme est critique)
- `internal/consensus` — sélection du proposeur, validation des blocs
- `internal/p2p` — parsing des messages réseau (entrées non fiables)
