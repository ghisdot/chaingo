# Politique de sécurité

## Statut

ChainGO opère actuellement un **testnet public** (`chaingo-testnet-1`).
Aucun réseau de valeur réelle (mainnet) n'est encore en service.

La sécurité repose sur une **revue interne (self-audit) + un durcissement
communautaire** (bug bounty ouvert), et non sur un audit par un cabinet tiers.
Le détail des surfaces couvertes, des limites assumées et de la méthode est dans
le **[Rapport de revue de sécurité](docs/SECURITY-REVIEW.md)** (reproductible).
Les surfaces « maison » non encore durcies par la communauté — anonymat
zk-STARK (`PrivacyEnabled`), VM WASM (`WasmEnabled`) — restent **désactivées sur
mainnet** par un gate.

Le testnet sert précisément à éprouver le code en conditions publiques
avant le mainnet. Toute découverte de vulnérabilité est précieuse — merci
de la signaler selon la procédure ci-dessous.

## Signaler une vulnérabilité

- **N'ouvrez pas d'issue publique** pour une faille exploitable.
- Écrivez à **ghisdot@proton.me** avec :
  - une description de l'impact,
  - une procédure de reproduction (PoC bienvenue),
  - la version concernée (commit hash si possible).
- Première réponse sous 72 h.
- Divulgation coordonnée : la vulnérabilité reste embargoée jusqu'à la
  publication du correctif.

Les contributeurs qui signalent une faille sérieuse seront crédités
publiquement (sauf demande contraire) lors de la publication du correctif.

## Périmètre particulièrement sensible

| Paquet | Pourquoi |
|---|---|
| `internal/crypto` | signatures ML-DSA-65, dérivation d'adresses |
| `internal/state` | règles d'exécution, frais, supply — tout écart de déterminisme est critique |
| `internal/consensus` | sélection du proposeur, validation des blocs, slashing |
| `internal/p2p` | parsing de messages réseau (entrées non fiables) |
| `internal/genesis` | empreinte déterministe de la genèse |
| `cmd/wallet-wasm` | wallet web (clé privée côté navigateur) |

## Hors périmètre

- Problèmes purement esthétiques du site ou du wallet web (à signaler en
  issue publique standard).
- Reports génériques de scan automatique (Nessus / OWASP ZAP / etc.) sans
  preuve d'exploitation.
- Vulnérabilités d'hébergeurs tiers ou de dépendances déjà publiquement
  divulguées (relayer vers l'upstream concerné).

## Pour aller plus loin

- [CONTRIBUTING.md](CONTRIBUTING.md) — règles du projet pour contribuer.
- [ROADMAP.md](ROADMAP.md) — avancement de la Phase 2 (sécurité de
  production) et de l'audit externe planifié.
