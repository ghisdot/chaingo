# Rapport de revue de sécurité — ChainGO

> **Réseau** : testnet public `chaingo-testnet-1`
> **Nature** : revue de sécurité **interne** (self-audit) + dossier de preuve.
> **Méthode** : tests adverses déterministes, fuzzing, tests multi-nœuds,
> analyse de soundness du système de preuve.

---

## 1. Ce qu'est — et n'est pas — ce document

Ce rapport est une **revue de sécurité interne** conduite par l'équipe du
projet, accompagnée d'un **dossier de preuve reproductible**. **Ce n'est pas un
audit réalisé par un cabinet tiers indépendant.**

La stratégie de sécurité de ChainGO est assumée et explicite :

- **Cryptographie standardisée là où elle existe** : signatures **ML-DSA-65**
  (FIPS 204, niveau NIST 3), hachage **SHA3-256**, scellement de keystore
  scrypt + AES-GCM. Aucune courbe elliptique classique nulle part.
- **Cryptographie « maison » pour l'anonymat fort** (pile zk-STARK) : elle est
  **ouverte, reproductible et durcie par une communauté** (bug bounty ouvert,
  cf. §6), pas par un cabinet. Ses limites sont documentées sans détour (§4.4,
  §5) et son activation sur le mainnet est **verrouillée par un gate** tant que
  le durcissement communautaire n'a pas eu lieu.

Tout claim de ce document est **étayé par un test exécutable** (cf. §7). Aucune
affirmation de sécurité n'est faite « sur parole ».

---

## 2. Périmètre et modèle de menace

| Domaine | Paquet | Menaces considérées |
|---|---|---|
| Crypto post-quantique | `internal/crypto` | forge de signature, malléabilité, dérivation d'adresse |
| Consensus BFT « Aurora » | `internal/consensus` | double-signe, partition réseau, reorg profond, perte de finalité, équivocation |
| Machine d'état | `internal/state` | non-déterminisme, non-atomicité, création de valeur, contournement des règles éco/token/contrat |
| Anonymat zk-STARK | `internal/stark` | forge de preuve, création de valeur, vol de note, extraction de montant, débordement de conservation |
| VM WASM | `internal/wasmvm` | non-déterminisme, non-terminaison (gas), opcodes non déterministes |
| Réseau P2P | `internal/p2p` | parsing d'entrées non fiables, DoS par tailles |
| Genèse | `internal/genesis` | empreinte non déterministe |

**Hypothèses** : adversaire byzantin contrôlant < 1/3 du stake pour la sûreté
BFT ; canaux réseau non fiables ; entrées (tx, blocs, messages P2P) hostiles.

---

## 3. Méthodologie

- **~390 tests** automatisés, dont **246 sur la seule pile zk-STARK** et **31
  tests de faute du consensus**. Déterministes (aucun `time`/`rand` non
  contrôlé ; PRNG à graine fixe).
- **Tests adverses dédiés** (fichiers `*_adverse_*`, `*_fault_*`,
  `*_forgerie_*`) : chaque test **tente une attaque** et **exige le rejet**.
- **Fuzzing** (6 cibles `Fuzz*`) sur le parsing et l'instrumentation.
- **Tests multi-nœuds** : 4 nœuds doivent converger sur la **même racine
  d'état** (déterminisme inter-nœuds).
- **Analyse de soundness** du système de preuve (budget de requêtes FRI +
  amplification OOD ; cf. §4.4 et `docs/PREUVE-PHASE3.md`).

Reproductible intégralement (§7).

---

## 4. Résultats par domaine

### 4.1 Cryptographie post-quantique — ✅ standard

- Toutes les signatures (tx, blocs, votes) passent par **ML-DSA-65**. Centralisé
  dans `internal/crypto` ; aucune réintroduction d'ECDSA/Ed25519 possible sans
  casser l'invariant testé.
- Adresses dérivées par hachage de la clé publique ; keystore chiffré
  (scrypt + AES-GCM), clé privée jamais transmise (wallet web : tout en local).
- **Risque résiduel** : la maturité d'implémentation de ML-DSA reste plus jeune
  que celle des courbes classiques (propre à tout l'écosystème PQ).

### 4.2 Consensus BFT « Aurora » — ✅ couvert par tests de faute

- Proposeur **déterministe pondéré par le stake** ; rounds de secours pour la
  liveness.
- **Finalité BFT persistante** (`LastCommit` ≥ 2/3) vérifiable depuis la chaîne.
- Tests de faute : **double-signe → slashing**, **partition → pas de finalité
  sans quorum**, **reorg profond (enterré) atomique** (snapshot/restore,
  métadonnées effacées seulement après succès, **jamais sous la finalité**),
  **prevote-the-lock** (verrou POL).
- **Risque résiduel** : la décentralisation réelle dépend de **validateurs
  indépendants** (jalon opérationnel, pas du code).

### 4.3 Machine d'état — ✅ couvert

- **Atomicité** : `Execute(strict=true)` snapshot/restore — un bloc qui échoue ne
  laisse aucune trace.
- **Déterminisme** : sérialisation JSON canonique (ordre des champs figé, clés de
  map triées), horloge = timestamp du header (jamais l'horloge locale).
- **Règles économiques** = paramètres de genèse (jamais codées en dur) :
  EIP-1559 (base fee brûlé + tip), inflation sur le stake, unbonding.
- **Tokens** : symbole unique, plafond **max-supply** appliqué au mint (+ garde
  anti-débordement), **burn** borné au solde, métadonnées bornées.
- **Contrats no-code** (vesting, escrow, multisig, DAO, presale, timelock,
  airdrop, streaming) : autorisations **par rôle** testées, conservation des
  fonds, états terminaux.

### 4.4 Anonymat zk-STARK (maison) — 🟡 fonctionnel, durci, NON audité en externe

Pile **post-quantique hash-only** (corps Goldilocks, FRI, AIR multi-colonnes,
Poseidon) + circuit de transaction blindée M-entrées/N-sorties. Tests adverses :
forge de preuve, OOD falsifié/permuté/tronqué, non-conservation, vol de note,
extraction de montant — **tous rejetés**.

Durcissement livré et testé :

- **Soundness ≥ 128 bits (conjecturée)** : 40 requêtes FRI + grinding 16 bits
  (terme ≈ 136 b) **et amplification OOD multi-points** (3 points hors-domaine
  indépendants ⇒ erreur Schwartz-Zippel ~2⁻¹⁴⁴ sur le corps 64 bits).
- **Range-proofs** : valeurs de note bornées `< 2⁴⁸` ⇒ **création de valeur par
  débordement modulaire fermée** ; l'état refuse aussi les dépôts hors borne.
- **Dédup des nullifiers intra-tx** côté état (anti double-dépense).
- **Masquage ZK** des montants (testé non extractible).

**Limites assumées** (cf. §5) : Poseidon **non standardisé**, soundness
**conjecturée** (pas prouvée formellement), **ZK formel** non démontré,
**key-privacy ML-KEM** à établir. → **Gate `PrivacyEnabled` OFF sur mainnet**
jusqu'au durcissement communautaire. Détail : `docs/PREUVE-PHASE3.md`.

### 4.5 VM WASM — 🟡 fonctionnel, gate mainnet

- Déterminisme par **gas** (instrumentation **fuzzée 5,3 M exécutions**), jeu
  d'opcodes restreint validé au déploiement, interpréteur wazero.
- Vérifié par un test **multi-validateurs** (4 nœuds, même racine).
- **Gate `WasmEnabled` OFF sur mainnet** jusqu'au durcissement.

### 4.6 Réseau P2P & codecs — ✅ couvert

- Codec binaire à **longueurs bornées** (anti-DoS) ; décodage robuste, jamais de
  panique sur entrée malformée (testé, y compris tampons tronqués).

---

## 5. Limites connues (transparence)

1. **Pas d'audit par un tiers indépendant** — stratégie « self-audit +
   durcissement communautaire » assumée.
2. **Pile vie-privée maison** : Poseidon non standardisé, **soundness conjecturée
   et non prouvée** (la borne *prouvée* 128 b exigerait un corps d'extension pour
   l'aléa de pliage FRI), ZK formel non démontré, key-privacy ML-KEM à établir.
3. **Décentralisation** : validateurs indépendants pas encore en place.
4. **Mainnet non ouvert** : les gates `PrivacyEnabled` et `WasmEnabled` restent
   **OFF sur mainnet** jusqu'au durcissement de ces deux surfaces.

Ces limites sont **traçables** dans `ROADMAP.md` (section « À venir »).

---

## 6. Surface d'attaque ouverte (bug bounty)

ChainGO **invite** la communauté à attaquer le code. Cibles à fort intérêt :

- forger une preuve de dépense blindée pour une note inexistante ;
- **créer de la valeur** (débordement, doublon de nullifier, conservation) ;
- voler une note sans la clé `nk` ; extraire un montant d'une preuve ;
- casser la collision-résistance de Poseidon (paramètres maison) ;
- provoquer un **désaccord de racine d'état** entre nœuds (non-déterminisme) ;
- contourner une autorisation de contrat (rôle, seuil, échéance).

Signalement : voir [SECURITY.md](../SECURITY.md) (divulgation coordonnée,
crédit public). Tout est open-source (Apache 2.0) et reproductible.

---

## 7. Reproductibilité

```bash
# Suite complète (inclut les preuves zk — plusieurs minutes) :
go test ./... -count=1

# Rapide (saute les preuves zk lourdes) :
go test -short ./...

# Domaines ciblés :
go test ./internal/consensus/ -run 'Fault|Reorg|Finality|Lock' -v   # faute BFT
go test ./internal/stark/ -run 'Forgerie|Soundness|Adverse|SpendN'  # soundness zk
go test ./internal/state/ -run 'Token|Template|Shield'              # état/tokens/contrats

# Fuzzing (exemple) :
go test ./internal/wasmvm/ -run x -fuzz FuzzGasMetering -fuzztime 60s
```

**Dernière exécution complète** : voir le tableau ci-dessous (mis à jour à chaque
revue).

| Domaine | Tests | Verdict |
|---|---|---|
| `internal/crypto` | 3 | ✅ |
| `internal/consensus` | 31 | ✅ |
| `internal/state` | 46 | ✅ |
| `internal/stark` | 246 | ✅ |
| `internal/types` | 24 | ✅ |
| `internal/wasmvm` | 13 | ✅ |
| autres (`p2p`, `smt`, `store`, `genesis`, `shielded`…) | ~25 | ✅ |
| **Total** | **~390** | **✅ build + vet + tests au vert** |

---

*Document maintenu avec le code. Dernière revue alignée sur l'état courant du
dépôt. Pour le détail cryptographique de l'anonymat fort, voir
[docs/PREUVE-PHASE3.md](PREUVE-PHASE3.md).*
