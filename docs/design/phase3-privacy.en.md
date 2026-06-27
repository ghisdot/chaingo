# Design — Phase 3: Strong anonymity (shielded transaction pool)

> Status: **R&D prototype delivered** (`internal/shielded` + ML-KEM view keys in
> `internal/crypto`). **OFF-CONSENSUS · UNAUDITED · parameter OFF everywhere ·
> forbidden on mainnet until external audit.** The encryption layer is standardized
> crypto (safe); the spend validity proof is a **transparent placeholder** (not yet
> zero-knowledge) — see "Status" below.

## Goal and threat model

Today ChainGO is **pseudonymous**: addresses are not nominative, but the history is
**public** (who pays whom, how much). "Strong anonymity" = hiding, on-chain:

1. the **recipient** (impossible to link a payment to the receiving address);
2. the **amount** (values are not readable).

All while remaining **post-quantum** — the constraint that eliminates nearly all
existing privacy tooling.

## Why we CANNOT reuse classical privacy crypto

| Classical building block | Problem for ChainGO |
|---|---|
| Stealth addresses (ECDH: `O = S + H(s)·G`) | relies on the **homomorphism of elliptic curves** — broken by a quantum computer, and ML-DSA/ML-KEM **do not have** this property. |
| Pedersen commitments, Bulletproofs (amounts/range) | elliptic curves → **not post-quantum**. |
| zk-SNARK (Groth16, PLONK…) | "trusted setup" + pairing curves → **not post-quantum**. |

The **only** plausible post-quantum zero-knowledge proof family is the **zk-STARK**
(security based solely on hash functions, transparent, no trusted setup).

## Chosen architecture: note pool (Zcash-Sapling style, PQ-adapted)

Rather than accounts, the shielded pool manipulates unspent **notes**:

- **Note** = (amount, recipient's view key, randomness `rho`).
- **Commitment** `cm = SHA3("cm" ‖ amount ‖ H(owner) ‖ rho)` — **hiding** (thanks to
  `rho`) and **binding**. This is what we publish, never (amount, recipient).
- **Commitment tree** (Merkle accumulator): lets us prove a note exists **without
  saying which one** (membership proof in ZK).
- **Nullifier** `nf = SHA3("nf" ‖ nk ‖ cm)`: revealed on spend to prevent
  double-spend. Derived from a **nullifier key** `nk` held only by the owner →
  unlinkable to the `cm` without `nk`.
- **Encrypted delivery + scan**: the note is encrypted to the recipient's **ML-KEM
  view key** and published; the recipient **scans** notes and opens their own — **no
  on-chain index links the note to its address**. This is the mechanism that hides
  the recipient (and it does NOT depend on the missing homomorphism).
- **Spend proof (zk-STARK)**: on spend, we prove in zero-knowledge that (a) each
  input is in the tree, (b) we know its opening and its `nk`, (c) the nullifiers are
  well-formed, (d) **Σinputs = Σoutputs + fees** — without revealing amounts or
  notes. **This is the research+audit core.**

## Status: what is delivered (slice 1)

| Element | Status | Safety |
|---|---|---|
| **ML-KEM-768** view keys (FIPS 203) derived from the seed (`crypto.DeriveViewKey`) | ✅ delivered | **standardized** (CIRCL) |
| Note encryption/opening + **scan** (`SealTo`/`OpenWith`) | ✅ delivered + tested | **real confidentiality** |
| Note, **commitment** (hash), **nullifier**, serialization (`internal/shielded`) | ✅ delivered + tested | PQ hash (ROM) |
| Pool + double-spend + value conservation (end-to-end flow) | ✅ delivered + tested | correct logic |
| **In-house STARK engine** (Goldilocks field, NTT, Merkle, transcript, **FRI**, toy STARK) | ✅ delivered R&D — adversarial review (7 classes of forgery rejected) | hash-only PQ |
| **Poseidon algebraic hash** + STARK-friendly Merkle | ✅ delivered + tested | in-house params to audit |
| **Multi-column AIR engine** + **full Poseidon AIR** (consistent with the native hash) | ✅ delivered + tested | — |
| **Merkle membership circuit** (ZK, depth 8) | ✅ delivered + tested | private witness |
| **Shielded SPEND circuit** (membership + nullifier + conservation) + **ZK masking** | ✅ **delivered + tested** (24 tests, amount non-extractable) | **real ZK proof** (≠ placeholder) |
| On-chain tx `shield`/`shielded_transfer`/`unshield` + `PrivacyEnabled` gate | ⬜ stage 5 (final) |
| **Community audit** (hackers) | ⬜ — **mainnet blocker** |

> Full proof dossier (components, tests, reproducibility, caveats, call for audit):
> [docs/PREUVE-PHASE3.md](../PREUVE-PHASE3.en.md).

### STARK engine caveats (to harden / audit)

The engine (`internal/stark`) is an **in-house unaudited prototype**. The built-in
adversarial review confirmed that the tested forgeries are rejected. **Hardening** has
since resolved several caveats:
- **Fiat-Shamir grinding delivered** (`FriParams.GrindBits`, default 16): anti-grinding
  proof-of-work before drawing the positions, +16 bits of soundness, verifier cost
  = 1 hash. Remaining: a formally proven "128-bit" target (fine-grained analysis).
- **Sampling without replacement delivered** (`ChallengeIndicesDistinct`): FRI query
  positions pairwise distinct → exact per-query soundness.
- **Variable FRI folding depth delivered** (`FoldStopBits`); **batch inversions**
  (Montgomery) + denominator dedup + parallelization → prover ~77× faster; **M-inputs
  / N-outputs circuit** (join-split) delivered.

### Warnings (not to oversell)

- **Two paths coexist.** The `SpendWitness`/`VerifyTransparent` of `internal/shielded`
  is the **TRANSPARENT prototype** (reveals amounts) — it served to establish the pool
  model. The **real zk-STARK circuit** (`internal/stark`, `poseidon_spend*.go`)
  **replaces** it: it hides amounts (ZK masking tested). The on-chain wiring (stage 5)
  will use the ZK circuit, not the placeholder.
- **Anonymity ≠ confidentiality.** ML-KEM encryption guarantees that no one **reads** a
  note. Whether the note is **unlinkable** to a *known* view key depends on ML-KEM's
  *key-privacy* (anonymity) — a distinct property **to verify/audit**. We claim today
  **confidentiality**, not yet formal anonymity.
- **Nothing is wired into consensus.** No shielded tx exists yet; it is an R&D bench
  (`internal/shielded`) with its tests.

## Plan by slices

| # | Content | Risk |
|---|---|---|
| 1 | ML-KEM view keys + notes/commitment/nullifier + encryption/scan + transparent pool (+ tests) | low (off-consensus) |
| 2 | Merkle tree of commitments (via `internal/smt`) + tx `shield`/`transfer`/`unshield`, **`PrivacyEnabled` gate OFF** | medium (state, still gated) |
| 3 | **zk-STARK circuit** (membership + ownership + nullifier + conservation) — deterministic prover/verifier | **high — research** |
| 4 | **External audit** + ML-KEM key-privacy review + test vectors | blocker |
| 5 | Mainnet activation by governance (`PrivacyEnabled` param) | post-audit |

## Decision

Strong anonymity is a **major differentiator** (**post-quantum** privacy), but its
core — the zk-STARK circuit — is **research+audit** crypto. We therefore build the
architecture in stages, each notch **gated and off-consensus**, and **we will never
wire the shielded pool into mainnet consensus before an external audit**. A privacy bug
is silent (users believe themselves anonymous without being so) or worse (theft of
funds): here, "carefully coded" is not enough — a formal review is required. Slice 1
delivers the **safe and standardized** building blocks (encryption, scan, note model)
on which everything else relies.
