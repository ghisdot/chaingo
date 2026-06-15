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
- `Run(wasm, fn, timeout, args…)` : instancie un module, fournit une API hôte
  minimale (`env.log`), appelle une fonction exportée, borne la mémoire (16 Mio).
- Protection anti-runaway : timeout **wall-clock** qui interrompt une boucle
  infinie (testé).
- Tests : exécute un module `add(i32,i32)` réel, interrompt une boucle infinie,
  échoue proprement sur module invalide / fonction absente.

**Pourquoi « hors-consensus »** : le timeout est *wall-clock*, donc **non
déterministe** — deux nœuds l'atteindraient à des points différents = **split de
la chaîne**. C'est exactement ce qui interdit de l'exécuter dans un bloc en l'état.

## Ce qu'il MANQUE pour le rendre consensus-grade (le vrai chantier)

1. **Gas DÉTERMINISTE par instruction.** Le point dur. Sans CGO, wazero n'a pas
   de *fuel* intégré (contrairement à wasmtime). Il faut **instrumenter le
   bytecode** : injecter un décrément + un test de gas en tête de chaque bloc de
   base, recompiler. C'est un mini-compilateur. Garantit qu'une boucle infinie
   s'arrête de façon **identique sur tous les nœuds** (out-of-gas déterministe).
2. **Audit de déterminisme.** Canonicaliser les NaN flottants (ou interdire les
   flottants), interdire tout import non déterministe (horloge, aléa, threads),
   fixer le comportement de la croissance mémoire.
3. **API hôte d'accès à l'état**, chacune **gas-métrée** : `storage_get/set`
   (KV par contrat), `caller`, `self`, `balance`, `transfer`, `emit_event`,
   `block_height/time` (valeurs du bloc, déterministes).
4. **Types de transaction** : `wasm_deploy` (stocke le bytecode, frais de
   déploiement comme un token/contrat) et `wasm_call` (exécute, gas = frais).
   Stockage **par contrat** dans l'état (intégré à la racine d'état).
5. **Limites anti-DoS** : taille max du bytecode, profondeur de pile, taille de
   mémoire, nombre d'exports, validation stricte à l'instanciation.
6. **Fuzzing** des décodeurs/exécution + **audit externe** (du code qui exécute
   du bytecode arbitraire envoyé par n'importe qui = surface d'attaque maximale).

## Plan d'implémentation (tranches, post-testnet)

| Tranche | Contenu |
|---|---|
| 1 | Instrumentation de gas (injection bytecode) + tests de coût déterministe |
| 2 | API hôte d'état (storage KV par contrat) gas-métrée |
| 3 | Tx `wasm_deploy` / `wasm_call` + stockage contrat dans la racine d'état |
| 4 | API hôte économique (balance/transfer/events) + limites anti-DoS |
| 5 | Fuzzing intensif + préparation audit |

## Décision

Le moteur EVM-like est un **différenciateur fort** (contrats arbitraires +
post-quantique), mais c'est **le plus gros chantier restant** et il exécute du
code hostile → **il ne sera câblé en consensus qu'après audit**. Pour le
**testnet de lancement**, les **templates no-code** (vesting, escrow, multisig,
DAO) couvrent largement les usages ; la VM arrive comme une **Phase 4 avancée**,
après le lancement et avec une revue de sécurité.
