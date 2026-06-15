# Préparation du mainnet ChainGO

> 🔴 **Le mainnet n'est PAS lancé.** Le réseau reste sur le **testnet**
> (`chaingo-testnet-1`) tant que les pré-requis ci-dessous ne sont pas tous
> remplis. Ce document est le plan de route et le mode opératoire — pas un
> déclencheur.

## 1. Pré-requis avant lancement (checklist bloquante)

Aucun mainnet tant que **tout** n'est pas coché :

- [ ] **Phase 2 terminée** : finalité BFT complète (verrouillage type Tendermint), slashing
      double-signature **et** d'inactivité, fork-choice, set de validateurs figé par hauteur.
- [ ] **Performance réseau** : codec binaire compact, arbre de Merkle creux pour l'état.
- [ ] **Tests** : couverture unitaire/intégration large, fuzzing des entrées réseau.
- [ ] **Audit de sécurité externe** passé, findings corrigés.
- [ ] **≥ 4 validateurs indépendants** engagés (n ≥ 3f+1 ; 4 tolère 1 faute). Idéalement plus,
      opérés par des entités distinctes.
- [ ] **Testnet public stable** depuis plusieurs semaines (uptime, montée en charge réelle).
- [ ] **Distribution de genèse finalisée et validée** (section 3), document signé/multi-sig.
- [ ] **Plan de réponse incident** (clés compromises, halt d'urgence, communication).

## 2. Paramètres mainnet (rappel des décisions actées)

| Règle | Valeur mainnet |
|---|---|
| chain_id | `chaingo-1` |
| Supply de genèse | 1 000 000 000 CGO |
| Max supply | aucun (élastique : émission ~3 %/an vs burn) |
| Unbonding | **21 jours** (`unbonding_seconds = 1814400`) — PAS la valeur testnet de 24 h |
| Stake min validateur | 10 000 CGO |
| Slashing double-signature | 5 % (`slash_double_sign_bps = 500`) |
| Faucet | **désactivé** (ni `--dev` ni `--testnet` : on lance avec `--genesis mainnet.json`) |

Tous ces réglages vivent dans `params` du document de genèse — voir
[internal/types/params.go](../internal/types/params.go).

## 3. Construire la genèse mainnet

L'outil : `chaingo genesis`.

```bash
# 1. Squelette + clé du 1er validateur
chaingo genesis template --chain-id chaingo-1 --out mainnet.json --seed-out v0.seed
# 2. Éditer mainnet.json pour la distribution (ci-dessous), unbonding 21 j, etc.
# 3. Vérifier — l'EMPREINTE doit être identique pour TOUS les opérateurs
chaingo genesis validate mainnet.json
```

### Distribution « communauté d'abord » (1 Md CGO) → champs de genèse

| Part | CGO | Où, dans la genèse |
|---|---|---|
| 50 % Communauté | 500 M | `alloc` vers une adresse de distribution (airdrops/programmes post-lancement) |
| 20 % Trésorerie | 200 M | `vesting` (déblocage progressif) ou `alloc` vers un coffre multisig |
| 15 % Équipe | 150 M | **`vesting` 4 ans** (déblocage linéaire on-chain) |
| 10 % Écosystème | 100 M | `alloc` vers le fonds écosystème |
| 5 % Genèse / liquidité | 50 M | `stakes` (validateurs de genèse) + `alloc` (liquidité) |

- **Le vesting est enforcé on-chain** : la part équipe/trésorerie est verrouillée dans des
  contrats de vesting créés dès le bloc 0 (`vesting` dans le JSON), débloqués linéairement
  entre `start_ms` et `end_ms`. Personne ne peut contourner le calendrier.
- ⚠️ **Prévoir un petit solde liquide** (`alloc`) pour chaque bénéficiaire de vesting :
  réclamer (`contract claim`) coûte des frais, donc 0 CGO liquide = impossible de réclamer.

Exemple de bloc `vesting` (équipe, 4 ans) dans `mainnet.json` :

```json
"vesting": [
  { "beneficiary": "cg<equipe>", "amount": 150000000000000000,
    "start_ms": 1750000000000, "end_ms": 1876000000000 }
],
"alloc": { "cg<equipe>": 1000000000 }
```

## 4. Cérémonie de genèse (multi-validateurs)

1. Chaque validateur génère sa clé : `chaingo keygen --out vN.seed` et
   **partage publiquement son adresse cg…** (jamais la seed).
2. Un coordinateur assemble `mainnet.json` (distribution + `stakes` des N
   validateurs, paramètres).
3. **Chaque participant exécute `chaingo genesis validate mainnet.json` et
   compare le `block hash` retourné.** L'empreinte doit être strictement
   identique partout — c'est la garantie de démarrage de la même chaîne.
4. À l'instant T convenu, chaque opérateur lance son nœud :
   ```bash
   chaingo node start --genesis mainnet.json --validator-seed vN.seed \
     --datadir /var/lib/chaingo --api 127.0.0.1:8545 --p2p :9000 \
     --peers <ip-seed-1>:9000,<ip-seed-2>:9000
   ```
5. Vérifier que `finalized_height` progresse (≥ 2/3 du stake en ligne) :
   la chaîne est vivante et finalise.

## 5. Décisions à finaliser avant la cérémonie

- **Adresses des coffres** (communauté, trésorerie, équipe, écosystème).
  Le template multisig M-of-N est disponible (`chaingo contract multisig`)
  et est recommandé pour ces coffres plutôt que des adresses à clé unique.
  À la genèse, on alloue d'abord vers une adresse, puis on déplace dans un
  coffre multisig créé on-chain. Un multisig directement à la genèse pourra
  être ajouté ultérieurement.
- **Calendrier précis des vestings** (cliff initial ? durée trésorerie ?).
- **Liste des validateurs de genèse** et leurs stakes respectifs.
- **Date du lancement** et plan de communication.

Voir la [feuille de route](../ROADMAP.md) pour l'avancement des pré-requis techniques.
