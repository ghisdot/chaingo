# Design — Validator set frozen by height (#5)

> A slice of the BFT hardening. Prerequisite of POL locking (#6) and fork-choice (#7).

## Problem

The BFT quorum checks (a commit must gather > 2/3 of active power) were reading
validator power from the **live** state:

- `verifyCommit`: `state.PowerOf(voter)` and `state.TotalPower()`
- `AddVote`: `state.PowerOf(voter)` to accept/reject a vote
- `buildLastCommit`: `state.TotalPower()` as the 2/3 denominator

But the state changes continuously (stake, unstake, jail, delegations). When we
verify the commit of block H — carried by block H+1, hence evaluated *after* the
state has already advanced — the `TotalPower()` denominator and each voter's power
can **differ from what they were at height H**.

Possible consequences: a commit legitimately ≥ 2/3 at H rejected later (finality/
liveness loss), or a quorum mis-evaluated if the set has grown/shrunk. For a
consensus that protects real money, **the 2/3 must be measured against a frozen set,
identical for all nodes**.

## Definition

> **Voting set of height H = the set of active validators as it stands just after
> applying block H-1** (the "post-(H-1)" state).

This is consistent with proposer selection: `SelectProposer(H, …)` is already called
when the state is at H-1, so the proposer of H is drawn from the post-(H-1) set. The
precommits of H must gather 2/3 of **this same** set.

## Mechanics

- `state.SnapshotActiveSet()` returns an **immutable snapshot** `{powers, total}`
  of validators with power > 0.
- The engine keeps `setByHeight : height → *ValidatorSet`.
- At the **entry** of processing a block H (production or receipt), before
  `Execute(H)`, the state is exactly post-(H-1): we freeze `setByHeight[H]`.
- All quorum checks for height H use `setForHeight(H)` instead of the live state.
- `setForHeight(H)` falls back to a snapshot of the current state **if the height is
  not yet frozen**. This case only arises (a) for a vote received on the next height
  before we have processed its block — where the current state IS the right set — or
  (b) just after a restart for the single not-yet-finalized height. The fallback is
  thus never worse than the current behavior.
- We purge `setByHeight[h]` for `h ≤ finalized` (a tiny window kept: from
  `finalized+1` to `current+1`).

## Non-goals (later slices)

- **Locking** (lock/unlock on proof of a more recent quorum): #6.
- **Lock-driven fork-choice**: #7.

This change merely makes the quorum denominator deterministic and stable per height —
the necessary foundation before reasoning about locks.
