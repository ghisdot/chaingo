# Design — Machine virtuelle WASM (smart contracts arbitraires)

> Statut : **livré et câblé en consensus sur testnet/devnet** (`internal/wasmvm` +
> tx `wasm_deploy`/`wasm_call`, stockage par contrat dans la racine d'état).
> Activé par le Param de genèse **`WasmEnabled`** : **ON** en devnet/testnet,
> **OFF en mainnet** jusqu'au **durcissement communautaire** — invariant de sûreté :
> pas de code arbitraire exécuté en consensus mainnet avant ce durcissement. Le
> testnet sert précisément à éprouver ce moteur avant le mainnet.

## Objectif

Permettre aux développeurs de déployer des **contrats arbitraires** (pas
seulement les templates no-code), comme l'EVM sur Ethereum/BNB — mais en
**WebAssembly**, plus moderne et multi-langage (Rust, AssemblyScript, TinyGo, C).

## Le moteur d'exécution (`internal/wasmvm`)

`internal/wasmvm` charge et **exécute réellement** du WASM via **wazero** (runtime
Go pur, **sans CGO** — préserve la compilation native Windows/Linux/macOS du
projet) :
- `Run(wasm, fn, timeout, args…)` : instancie un module, API hôte minimale
  (`env.log`), mémoire bornée (16 Mio), timeout wall-clock (sandbox).
- **`RunMetered(wasm, fn, gasLimit, args…)` : GAS DÉTERMINISTE** — voir ci-dessous.
- Tests : exécute un `add(i32,i32)` réel, interrompt une boucle infinie, échoue
  proprement sur module invalide / fonction absente.

## ✅ Tranche 1 LIVRÉE : gas déterministe par instrumentation (`meter.go`)

Le vrai verrou est levé. `instrument(wasm, gasLimit, cost)` **réécrit le
bytecode** : ajoute un global i64 de gas (init = gasLimit, **aucun renumérotage**
car ajouté en fin d'espace d'index) et injecte une **charge de gas** (décrément +
test ; `unreachable` si < 0) en tête de **chaque corps de fonction** et de **chaque
`loop`**. Comme boucle et récursion sont les seuls moyens de répéter du travail
en WASM, ces deux points **garantissent l'arrêt** : aucun module ne tourne
au-delà de son gas, et le point d'arrêt est **identique sur tous les nœuds**
(out-of-gas déterministe, pas un timeout).

Sûreté : jeu d'opcodes restreint au sous-ensemble MVP ; tout opcode inconnu
(SIMD/atomics/bulk-mem 0xfc-0xfe…) fait **rejeter** le module (jamais de saut
deviné). Encodage **LEB128 signé** correct (≠ zig-zag de Go). **Fuzzé** : 5,3 M
exécutions sur bytecode arbitraire sans panique. Tests : la sémantique est
préservée (`add` instrumenté = 42), une boucle infinie s'arrête par gas en < 1 s.

CLI : `chaingo wasm run --gas N <fichier.wasm> <fn> [args]`.

**Périmètre v1** : garantie d'**arrêt**. La tarification fine (gas proportionnel
au travail, par bloc de base) est un raffinement (charge fixe par point pour
l'instant).

## Comment le déterminisme est tenu (en consensus)

Exécuter du code arbitraire sur tous les nœuds exige un résultat **bit-à-bit
identique**, sinon les racines d'état divergent et la chaîne se scinde. Les
garanties en place :
- **Arrêt déterministe** par instrumentation de gas (cf tranche 1) — pas de
  timeout wall-clock dans le chemin de consensus.
- **Jeu d'opcodes restreint**, *validé au déploiement* (`wasmvm.Validate`) : tout
  opcode non maîtrisé (SIMD, atomics…) fait rejeter le bytecode → seul du code
  instrumentable atterrit on-chain.
- **Interpréteur wazero forcé** (`RunDeterministic`) : même chemin d'exécution
  sur toutes les architectures CPU.
- **Imports déterministes uniquement** : module `env` (storage, caller, value,
  transfer, log) — ni horloge, ni aléa, ni threads.
- **Revert atomique** : trap/out-of-gas → effets du contrat ignorés, frais brûlés
  (anti-DoS) ; succès → storage/balance/transferts commités atomiquement.

Vérifié par un **test d'intégration multi-validateurs** : déploiement puis appel
via le vrai chemin de consensus, 4 nœuds d'accord sur la racine d'état à chaque
étape.

## Ce qu'il MANQUE encore (avant activation mainnet)

1. **Durcissement communautaire** (exécution de bytecode hostile, bug bounty) — le **bloquant** mainnet.
2. **Revue des flottants** : canonicalisation des NaN ou interdiction au
   déploiement (aujourd'hui : autorisés, l'interpréteur wazero est déterministe ;
   à confirmer par l'audit).
3. **Tarification fine du gas** : coût proportionnel au travail par bloc de base
   (aujourd'hui : charge fixe par point — garantit l'arrêt, pas un marché du gas).
4. **Limites anti-DoS plus fines** : profondeur de pile, nombre d'exports
   (déjà en place : taille max du bytecode, mémoire 16 Mio, gas plafonné).

## Plan d'implémentation (tranches)

| Tranche | Contenu | État |
|---|---|---|
| 1 | Instrumentation de gas (injection bytecode) + fuzzing | ✅ **livré** |
| 2 | Tarification fine (coût par bloc de base) + revue déterminisme | ⬜ (gas = arrêt seul ; durcissement en cours) |
| 3 | **API hôte d'état en SANDBOX** (`host.go` : storage_read/write, caller, value, transfer, log) | ✅ **livré** |
| 3b | Câblage de l'API hôte sur la VRAIE machine d'état (storage par contrat dans la racine) | ✅ **livré** |
| 4 | Tx `wasm_deploy` / `wasm_call` + frais + déterminisme (interpréteur) | ✅ **livré** (testnet/devnet) |
| 5 | Limites anti-DoS (taille bytecode ✅, mémoire ✅, gas ✅ ; pile ⬜) + **durcissement communautaire** ⬜ | 🟡 partiel |

## API hôte (module `env`)

Un contrat peut appeler : `storage_read/write` (KV par contrat), `caller`,
`value`, `transfer`, `log`. ABI mémoire par (pointeur, longueur). En consensus,
ces hôtes sont adossés à la VRAIE machine d'état : le storage du contrat est
chargé depuis la racine, recommité atomiquement en cas de succès. Exemple de
contrat Rust : [examples/contracts/counter](../../examples/contracts/counter).

## Déployer / appeler

- **Studio** (no-code, web) : onglet « Contrats WASM » → téléverser un `.wasm`
  compilé → tx signée par le wallet post-quantique. Formulaire d'appel + liste.
- **CLI** : `chaingo wasm deploy --from <wallet> <fichier.wasm>` puis
  `chaingo wasm call --from <wallet> [--gas N] [--value CGO] <adresse> <fn> [args]`.
  `chaingo wasm list` liste les contrats. `chaingo wasm run` reste la sandbox locale.
- **API** : `GET /v1/wasm/contracts`, `GET /v1/wasm/contracts/{addr}` (+ storage).
  Le bytecode est soumis dans le champ `code` (base64) d'une tx `wasm_deploy`.

L'adresse d'un contrat = hash de sa tx de déploiement. Frais : `WasmDeployFee` /
`WasmCallFee` brûlés (Params de genèse).

## Décision

Le moteur WASM est un **différenciateur fort** (contrats arbitraires +
post-quantique). Il est **livré et câblé** sur testnet/devnet — c'est là qu'on
l'éprouve avec de vrais contrats et du trafic. Il reste **désactivé sur mainnet
(`WasmEnabled=false`)** jusqu'au **durcissement communautaire** : il exécute du
code potentiellement hostile, et l'invariant du projet est de ne jamais faire
tourner ça en consensus mainnet sans cette revue. Les **8 templates no-code**
(vesting, escrow, multisig, DAO, presale, timelock, airdrop, streaming) restent
l'option recommandée pour la majorité des usages, sans surface d'attaque de VM.
