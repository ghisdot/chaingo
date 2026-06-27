# Design — Sparse Merkle tree for the state root (#9)

> Pre-audit. Eventually replaces the O(n) hashing of the full state JSON.

## Problem

`state.rootLocked()` serializes the ENTIRE state (accounts, validators,
tokens, contracts) to canonical JSON and hashes it — **O(n) per block**,
where n = state size. At mainnet scale (millions of accounts), this is a
bottleneck. And this "blob" hash offers **no inclusion proof** for light
clients.

## Solution

A **sparse Merkle tree** (`internal/smt`):

- Logical key (address) → 256-bit path = SHA3-256(key). Value → leaf =
  SHA3-256(value).
- Empty subtrees folded onto precomputed default hashes ("sparse").
- **O(depth) update**: only the leaf→root path is recomputed.
  **O(1) root** (cached root node). Measured: ~250 µs/update, *stable* from
  100 to 100,000 leaves (vs O(n) today).
- **Inclusion AND exclusion proofs** verifiable without the tree —
  foundation of light clients.
- All in SHA3-256 (project invariant). Root **independent of insertion
  order** (inter-node determinism — tested).

## State: foundation delivered, wiring to come

**Delivered**: the standalone `internal/smt` package, tested (determinism,
order, update/delete, inclusion/exclusion proofs) + benchmark. No consensus
impact.

**To activate (BREAKING change)**: replace `state.rootLocked()` with the SMT
root. This changes the **state root** present in the block header and verified
by all nodes → also changes the genesis fingerprint → coordinated upgrade
(like the binary P2P switch / the storage format). To be done in a dedicated
step, with:
1. Maintaining an `smt.Tree` alongside the state, updated on every mutation
   of account/validator/token/contract.
2. `rootLocked()` returns `tree.Root()`.
3. Recomputing the reference genesis fingerprint.
4. Tests: identical root between two nodes replaying the same blocks; root
   stable after restart.

## Accepted limit (later optimization)

The store keeps every node on each key's path → O(keys × 256) memory. Fine
for testnet. For millions of accounts, add **path compression** (store only
the branching nodes, Jellyfish Merkle Tree style) — without changing the
semantics of the root.
