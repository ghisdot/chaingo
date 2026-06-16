# Design — Machine virtuelle WASM (smart contracts arbitraires)

> Statut : **preview expérimentale livrée** (`internal/wasmvm`, hors-consensus).
> Le moteur consensus-grade (contrats déployables on-chain) est un chantier XXL
> détaillé ci-dessous. À NE PAS câbler dans les blocs avant que tous les points
> « consensus-grade » soient faits + audités.

## Objectif

Permettre aux développeurs de déployer des **contrats arbitraires** (pas
seulement les templates no-code), comme l'EVM sur Ethereum/BNB — mais en
**WebAssembly**, plus moderne et multi-langage (Rust, AssemblyScript, TinyGo, C).

## Ce qui est livré (preview, hors-consensus)

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

## Ce qu'il MANQUE encore pour le consensus

2. **Audit de déterminisme.** Canonicaliser les NaN flottants (ou interdire les
   flottants), interdire tout import non déterministe (horloge, aléa, threads),
   fixer le comportement de la croissance mémoire.
3. **API hôte d'accès à l'état**, chacune **gas-métrée** : `storage_get/set`
   (KV par contrat), `caller`, `self`, `balance`, `transfer`, `emit_event`,
   `block_height/time` (valeurs du bloc, déterministes).
4. **Types de transaction** : `wasm_deploy` + `wasm_call` + stockage **par
   contrat** dans la racine d'état.
5. **Limites anti-DoS** : taille max du bytecode, profondeur de pile, mémoire,
   nombre d'exports. **Audit externe** (exécute du bytecode hostile).

## Plan d'implémentation (tranches)

| Tranche | Contenu | État |
|---|---|---|
| 1 | Instrumentation de gas (injection bytecode) + fuzzing | ✅ **livré** |
| 2 | Tarification fine (coût par bloc de base) + audit déterminisme | ⬜ |
| 3 | **API hôte d'état en SANDBOX** (`host.go` : storage_read/write, caller, value, transfer, log) | ✅ **livré** (hors-consensus, en mémoire) |
| 3b | Câblage de l'API hôte sur la VRAIE machine d'état (storage par contrat dans la racine) | ⬜ |
| 4 | Tx `wasm_deploy` / `wasm_call` + frais de déploiement/appel | ⬜ |
| 5 | Limites anti-DoS (taille bytecode, pile) + **audit externe** | ⬜ |

## API hôte (tranche 3, sandbox) — `Sandbox`

Un contrat peut appeler (module `env`) : `storage_read/write` (KV par contrat),
`caller`, `value`, `transfer`, `log`. ABI mémoire par (pointeur, longueur).
L'état est **en mémoire** (`Sandbox`) — prototype de ce que la tranche 3b
branchera sur la racine d'état. Exemple de contrat Rust :
[examples/contracts/counter](../../examples/contracts/counter). Wiring testé
(un contrat appelle `value()` et atteint le sandbox), logique d'état testée.

## Décision

Le moteur EVM-like est un **différenciateur fort** (contrats arbitraires +
post-quantique), mais c'est **le plus gros chantier restant** et il exécute du
code hostile → **il ne sera câblé en consensus qu'après audit**. Pour le
**testnet de lancement**, les **templates no-code** (vesting, escrow, multisig,
DAO) couvrent largement les usages ; la VM arrive comme une **Phase 4 avancée**,
après le lancement et avec une revue de sécurité.
