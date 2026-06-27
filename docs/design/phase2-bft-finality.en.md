# Design — Phase 2: BFT finality (issue #6)

## Problem

Today (Phase 1), a block is "committed" as soon as the elected proposer
produces it. Security = trust in that proposer. With multiple validators, we
want a **block to be final only if a supermajority of the stake attests to
it** — Byzantine fault tolerance (BFT) up to < 1/3 of the stake being
dishonest or offline.

## Approach: a *finality gadget* on top of Aurora

We do NOT replace the existing mechanism (deterministic proposer + fallback
rounds for liveness). We add a **finality layer**:

```
            production (Aurora, Phase 1)              finality (Phase 2, this design)
   elected proposer ──► block committed locally ──► precommit signed by each validator
                                                 │
                                  gossip of votes ▼
                       stake-weighted accumulation per (height, hash)
                                                 │
                          ≥ 2/3 of total stake ? ▼
                                          height FINALIZED (irreversible)
```

- **Liveness / safety separation.** The chain keeps moving forward (blocks
  produced) even if finality falls behind. A block goes through two states:
  *committed* (applied, reversible in theory) then *finalized* (≥ 2/3 of the
  stake has precommitted it, irreversible).
- **Vote = precommit.** `Vote{ChainID, Height, BlockHash, Voter, VoterPub, Signature}`,
  signed in **ML-DSA-65** (never any other scheme — project invariant).
- **Voting power = validator weight** = `stake + delegations` (consistent
  with the proposer draw).
- **Quorum**: `Σ power(voters for (h, hash)) × 3 > 2 × total_power`. Strictly
  greater than 2/3.
- **Finality = prefix.** A quorum at height `h` on the hash of OUR local
  block finalizes `h` and, transitively (`prev_hash` chaining), all its
  ancestors. We track `FinalizedHeight` (monotonically increasing).

## Counting rules

1. A vote is only counted if: valid signature, `voter` is an active
   validator (power > 0), `height > FinalizedHeight`.
2. The quorum at `h` only advances `FinalizedHeight` if the quorum hash ==
   the hash of the local block at `h` (finality follows our canonical chain).
3. Deduplication by vote hash; a validator only counts once per (h, hash).

## What this slice covers / does not cover

**Covered (slice 1):** signed Vote type, weighted vote pool, 2/3 quorum
computation, automatic emission of a precommit after each local commit, P2P
gossip of votes, exposure of `finalized_height` (API + status).

**Out of scope (following slices, still #6/#7):**
- Full Tendermint locking (prevote + precommit + POL, locking/unlocking):
  here a single precommit round, no protection against certain partition
  scenarios. Double-proposal remains detectable and will be **slashed** (#7).
- Fork choice between two competing chains both finalized (impossible if
  < 1/3 Byzantine — BFT guarantee — but to be hardened).
- Validator set frozen by height (here: current set; OK as long as the set
  moves slowly, to be hardened).

These limits are accepted and listed to stay honest: this is a first
verifiable BFT step, not yet a complete audited BFT consensus.
