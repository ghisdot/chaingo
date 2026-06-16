# Exemple : contrat compteur (WASM)

Un smart contract WebAssembly minimal pour ChainGO. `increment()` lit un
compteur dans le **stockage du contrat**, l'incrémente, le réécrit et renvoie la
nouvelle valeur — il démontre l'**API hôte d'état** (`storage_read`,
`storage_write`).

> ⚠️ **PREVIEW expérimentale, hors-consensus.** Le moteur WASM n'est pas encore
> câblé dans les blocs (voir [docs/design/wasm-vm.md](../../../docs/design/wasm-vm.md)).
> `chaingo wasm run` l'exécute en **sandbox locale** : le stockage est en mémoire
> et réinitialisé à chaque appel.

## Compiler

Il faut la cible WebAssembly de Rust (une fois) :

```bash
rustup target add wasm32-unknown-unknown
```

Puis, depuis ce dossier :

```bash
cargo build --release --target wasm32-unknown-unknown
```

Le contrat est produit ici :

```
target/wasm32-unknown-unknown/release/counter.wasm
```

## Exécuter (sandbox locale)

```bash
chaingo wasm run --gas 1000000 \
  target/wasm32-unknown-unknown/release/counter.wasm increment
```

Sortie attendue :

```
⚠ PREVIEW expérimentale — exécution en sandbox LOCALE, pas sur la chaîne
Gas déterministe : 1000000 unités · appelant : cg-demo
gas consommé : <N> / 1000000
retour : [1]
```

(Chaque exécution part d'un stockage vide → le compteur renvoie `1`. La
persistance entre appels existe au sein d'un même sandbox — testée côté Go — et
viendra on-chain avec les tx `wasm_deploy`/`wasm_call`.)

## API hôte disponible (sandbox)

| Fonction (module `env`) | Rôle |
|---|---|
| `storage_read(kptr, klen, out, max) -> i32` | lit une clé (longueur écrite, ou -1) |
| `storage_write(kptr, klen, vptr, vlen)` | écrit une clé→valeur |
| `caller(out, max) -> i32` | adresse de l'appelant |
| `value() -> i64` | CGO envoyés avec l'appel |
| `transfer(toptr, tolen, amount_i64) -> i32` | transfert depuis le contrat (0 = ok) |
| `log(ptr, len)` | message de journal |

Le **gas est déterministe** (instrumentation du bytecode) : un contrat ne peut
pas tourner à l'infini, et le coût est identique sur tous les nœuds.
