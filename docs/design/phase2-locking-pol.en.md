# Design — POL locking — #6

> The most sensitive piece of Phase 2. This document settles the architecture
> decision BEFORE writing a single line, because a mistake here = double-spend.
> Prerequisite delivered: validator set frozen by height (#5).

## Recap of the current model

ChainGO currently does **finality by attestation**:

1. The elected proposer produces block H and commits it locally.
2. Each validator, upon seeing H, emits **one** prevote **and one** precommit for H.
3. When block H+1 carries ≥ 2/3 of precommits on H (`LastCommit`), H is finalized.

There are **no internal voting rounds within a height**. The header's `Round`
field is for **backup proposers** (liveness: if the round-0 electee is offline,
the round-1 electee proposes after an interval) — it is NOT a Tendermint voting
round.

Current safeguard (`castVoteKind`): a node **refuses to sign a 2nd vote of the
same kind at the same height**. Without a round in the vote, changing a precommit
= producing one's own equivocation proof → auto-slash. This is safe (fail-safe)
but **blocks any reorg**: a node that committed a minority branch cannot rally to
the majority one (liveness loss on that node, never a safety loss).

## What locking must guarantee

**Never two conflicting finalized blocks at the same height**, AND the ability to
**rally the legitimate branch** without self-slashing. Tendermint mechanics:

- At ≥ 2/3 of **prevotes** for a block B at a round r → this is a *polka* (Proof-of-
  Lock, POL). The validator **locks** on (r, B) and precommits B.
- It only **unlocks** (and precommits another block B′) if it sees a polka for B′
  at a **strictly higher** round. The proof of a more recent quorum justifies the
  change — so no punishable equivocation.

## The architecture decision

Locking presupposes **voting rounds**. ChainGO has none. Two paths:

### Path A — full Tendermint rewrite
State machine per height: `propose → prevote → precommit → commit`, with per-round
timeouts, round increment on timeout, etc. This is the textbook approach. **Cost**:
replaces the current "produce-then-attest" model (elegant and running in prod) with
a synchronous, timeout-driven consensus loop. High risk, several weeks, and we throw
away working code.

### Path B — graft POL onto the existing backup rounds ✅ recommended
The backup rounds **already produce competing candidate blocks at the same height**
(round-0 proposer vs round-1…). We reuse this round number as the voting round:

- We add `Round` to the `Vote` (= round of the voted block's header).
- We track prevotes by `(height, round, hash)` and detect polkas.
- Per-height lock: `(lockedRound, lockedHash)`. A validator only precommits a
  competing hash on a polka at a round **> lockedRound**.
- **Equivocation becomes round-aware**: two precommits at the same height AND the
  same round for different hashes = fault (slash). At different rounds WITH POL
  justification = legitimate change, no slash. (Ties into #8.)

**Why B**: it embraces the existing architecture (the backup rounds ARE already our
mechanism for competing blocks), keeps the liveness/safety separation, and turns the
binary "anti-2nd-vote" safeguard into a nuanced locking rule, without rewriting block
production.

## `Vote` format change (breaking)

`Vote` gains a `Round uint32` field. Since `SigningBytes` is canonical JSON and the
field order IS the signed format, adding `Round` **invalidates old vote signatures**
→ a coordinated upgrade of all nodes (acceptable at testnet stage, like the move to
binary P2P). The Vote's binary codec (slice 2) also gains the field.

## Status: DELIVERED (path B, top-of-chain reorg)

Slices 1-5 delivered and tested. Fork-choice switches to a competing block **at the
top** if it carries a polka at a higher round; validated by a **partition simulation
test** (`TestForkChoiceConvergesViaHigherRoundPolka`): a minority node on B0 (round
0) reorgs to B1 (round 1) as soon as it sees the polka, and the 4 nodes converge
without double-finality.

**Assumed scope**: reorg of the **top** (1 block). This is the dominant case because
finality only lags by one block. A **buried** fork (several divergent non-finalized
blocks) is out of scope — it is ignored, and the system stays fail-safe (never
double-finality). Multi-block reorg is a later hardening (replay an N-block branch
from the fork point).

## Implementation slices (each tested, committed separately)

1. **`Vote.Round`**: field + signing bytes + binary codec + call updates. Tests:
   signature round-trip, serialization. *(breaking — announced.)*
2. **Tracking prevotes by (height, round, hash) + polka detection** in the
   `votePool`. Tests: polka detected at ≥ 2/3, not below, by round.
3. **Per-height lock state** `(lockedRound, lockedHash)` + lock/unlock rule in vote
   emission (replaces the current binary safeguard). Tests: we only change a
   precommit on a higher-round polka; otherwise we stay locked.
4. **Round-aware equivocation** (same height+round, hash ≠ ⇒ fault; round ≠ ⇒
   legitimate). Ties into #8 (equivocating prevote slash). Tests: fault detected
   intra-round, inter-round change not punished.
5. **Lock-driven fork-choice** (#7): choose the branch covered by the highest-round
   polka; reorg bounded at the last finalized (immutable). Tests: partition scenario
   in the `simNet` harness → convergence without double-finality.

> #7 (fork-choice) is listed here as slice 5 because it is inseparable from the lock:
> the locks ARE the input to fork-choice. #5 (frozen set) is already delivered and
> provides the stable denominator of the quorums used by the polkas.

## Invariants preserved

- All signatures in ML-DSA-65 (never another scheme).
- `Execute(strict=true)` stays atomic (snapshot/restore).
- The block timestamp remains the rules' clock.
- A finalized height is never reverted (the reorg is bounded above the last
  finalized).
