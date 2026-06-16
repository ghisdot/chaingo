# Exemple : contrat compteur (WASM)

Un smart contract WebAssembly minimal pour ChainGO. `increment()` lit un
compteur dans le **stockage du contrat**, l'incrémente, le réécrit et renvoie la
nouvelle valeur — il démontre l'**API hôte d'état** (`storage_read`,
`storage_write`).

> Le moteur WASM est **câblé en consensus sur testnet/devnet** : ce contrat se
> déploie et s'appelle on-chain (`wasm_deploy`/`wasm_call`). Il reste **désactivé
> sur mainnet** (`WasmEnabled=false`) jusqu'à un audit externe — voir
> [docs/design/wasm-vm.md](../../../docs/design/wasm-vm.md). `chaingo wasm run`
> reste disponible pour un test **en sandbox locale** (stockage en mémoire).

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

(En sandbox locale, chaque exécution part d'un stockage vide → le compteur
renvoie `1`.)

## Déployer et appeler ON-CHAIN (testnet/devnet)

```bash
# déploie le bytecode ; l'adresse du contrat = hash de la tx
chaingo wasm deploy --from <wallet> \
  target/wasm32-unknown-unknown/release/counter.wasm
# appelle increment() — le storage PERSISTE entre les appels (on-chain)
chaingo wasm call --from <wallet> <adresse> increment   # → 1
chaingo wasm call --from <wallet> <adresse> increment   # → 2
chaingo wasm list                                        # contrats déployés
```

Ou via le **Studio** (`/studio/`, onglet « Contrats WASM ») : téléverser le
`.wasm`, puis appeler — la signature post-quantique se fait dans le navigateur.

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
