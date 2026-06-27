# Design — BFT safety: invariants, current state, path to full locking

This document frames the most sensitive topic of the project — **consensus
safety** — and honestly distinguishes it from what is delivered.

## Current model (delivered)

- Deterministic stake-weighted proposer + fallback rounds (liveness).
- A single voting round: **precommit** signed with ML-DSA-65.
- **Finality = commit ≥ 2/3 carried by the next block** (`LastCommit`),
  persisted and verifiable from the chain (lags by one block).
- Slashing of equivocation (double-signing) and of inactivity.

## Current safety property: "fail-safe", not "always live"

The system **never silently corrupts state**:
- A conflicting block (same height already committed, or `prev_hash` that
  does not match) is **rejected** by `ApplyExternalBlock` — never
  overwritten.
- A finalized height is never reverted.
- **Self-equivocation impossible**: a node never emits two precommits at the
  same height (the `castVote` guard + `voted` map). Without this guard, a
  node that reprocessed a height would sign a 2nd vote and slash itself.

**Accepted limit:** under asynchrony/partition, two valid blocks can coexist
at the *tip* (round 0 of the elected proposer vs a fallback round). Nodes
that committed the minority branch **get stuck** (they reject the majority
branch for lack of fork-choice) — a loss of **liveness** on those nodes,
**not** a loss of safety. Finality (≥ 2/3) can only designate a single block
per height (impossible to have two 2/3 quorums with < 1/3 Byzantine), so
never two conflicting finalized blocks.

## Why we don't "hack together" a naive reorg

Switching a node from one branch to the other requires **changing its
precommit** for the contested height. But re-signing another hash at the same
height = **self-equivocation** → self-slash. This is precisely the problem
that **Tendermint locking** solves. A naive reorg would therefore be *less*
safe than the current state.

## Path to full safety (Tendermint-like)

Two voting rounds per height/round, with locking:

1. **Propose**: the round's proposer proposes a block.
2. **Prevote**: each validator prevotes the block (or nil).
3. **Lock**: at ≥ 2/3 prevotes for a block B, the validator **locks** on B and
   precommits B. It will only precommit another block if it sees ≥ 2/3 of
   prevotes for that other block at a **strictly higher round** (proof of a
   more recent lock, *POL*).
4. **Precommit / Commit**: at ≥ 2/3 precommits for B, B is committed and final.

Locking guarantees that we only change a precommit on proof of a more recent
quorum — hence: **never two conflicting finalized blocks, and safe reorg**
toward the legitimate branch (without self-equivocation).

### Planned implementation slices
1. `Prevote` type (distinct from the precommit) + gossip.
2. Per height/round tracking: prevotes received, current lock (round + hash).
3. Locking rule in production and voting.
4. Lock-driven fork-choice (POL) + bounded reorg (down to the last finalized,
   immutable).
5. Validator set frozen by height (quorum verification against the epoch set).
6. Integration tests of the partition/fork scenarios in the `simNet` harness.

> The height-frozen set and the fork-choice only make sense with locking: they
> will be delivered together, after slices 1-3.
