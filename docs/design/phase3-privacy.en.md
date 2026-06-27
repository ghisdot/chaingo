# Design — Phase 3: Strong anonymity (shielded transaction pool)

> Status: **shipped and wired into consensus on testnet/devnet** (the
> `PrivacyEnabled` gate is **ON** on testnet/devnet, **OFF on mainnet** until
> community hardening). The encryption layer is standardized crypto
> (ML-KEM-768, safe); the spend-validity proof is a **real zk-STARK circuit**,
> M-in/N-out (hidden amounts, range-proofs, ≥128-bit conjectured soundness) —
> **not third-party audited**, see "Status" below and the
> [security review report](../SECURITY-REVIEW.en.md).

## Objective and threat model

Today ChainGO is **pseudonymous**: addresses are not tied to names, but the
history is **public** (who pays whom, how much). "Strong anonymity" = hiding,
on-chain:

1. the **recipient** (it is impossible to link a payment to the receiving address);
2. the **amount** (values are not readable).

All of this while remaining **post-quantum** — the constraint that eliminates
nearly all existing privacy tooling.

## Why we CANNOT reuse classical privacy crypto

| Classical building block | Problem for ChainGO |
|---|---|
| Stealth addresses (ECDH: `O = S + H(s)·G`) | relies on the **homomorphism of elliptic curves** — broken by a quantum computer, and ML-DSA/ML-KEM **do not have** this property. |
| Pedersen commitments, Bulletproofs (amounts/range) | elliptic curves → **not post-quantum**. |
| zk-SNARK (Groth16, PLONK…) | "trusted setup" + pairing-friendly curves → **not post-quantum**. |

The **only** family of zero-knowledge proofs plausible in a post-quantum
setting is the **zk-STARK** (security based solely on hash functions,
transparent, no trusted setup).

## Chosen architecture: note pool (Zcash-Sapling style, PQ-adapted)

Rather than accounts, the shielded pool manipulates unspent **notes**:

- **Note** = (amount, recipient's viewing key, randomness `rho`).
- **Commitment** `cm = SHA3("cm" ‖ amount ‖ H(owner) ‖ rho)` — **hiding** (thanks
  to `rho`) and **binding**. This is what gets published, never (amount, recipient).
- **Commitment tree** (Merkle accumulator): allows proving that a note exists
  **without saying which one** (ZK membership proof).
- **Nullifier** `nf = SHA3("nf" ‖ nk ‖ cm)`: revealed at spend time to prevent
  double-spending. Derived from a **nullifier key** `nk` that only the owner
  holds → not linkable to the `cm` without `nk`.
- **Encrypted delivery + scan**: the note is encrypted to the recipient's
  **ML-KEM viewing key** and published; the recipient **scans** notes and opens
  their own — **no on-chain index links the note to its address**. This is the
  mechanism that hides the recipient (and it does NOT depend on the missing
  homomorphism).
- **Spend proof (zk-STARK)**: at spend time, we prove in zero-knowledge that
  (a) each input is in the tree, (b) we know its opening and its `nk`, (c) the
  nullifiers are well-formed, (d) **Σinputs = Σoutputs + fees** — without
  revealing amounts or notes. **This is the research+audit core.**

## Status: what is shipped

| Element | Status | Safety |
|---|---|---|
| **ML-KEM-768** viewing keys (FIPS 203) derived from the seed (`crypto.DeriveViewKey`) | ✅ shipped | **standardized** (CIRCL) |
| Note encryption/opening + **scan** (`SealTo`/`OpenWith`) | ✅ shipped + tested | **real confidentiality** |
| Note, **commitment** (hash), **nullifier**, serialization (`internal/shielded`) | ✅ shipped + tested | PQ hash (ROM) |
| Pool + double-spend + value conservation (end-to-end flow) | ✅ shipped + tested | correct logic |
| **In-house STARK engine** (Goldilocks field, NTT, Merkle, transcript, **FRI**, toy STARK) | ✅ shipped R&D — adversarial review (7 forgery classes rejected) | hash-only PQ |
| **Algebraic Poseidon hash** + STARK-friendly Merkle | ✅ shipped + tested | in-house params to audit |
| **Multi-column AIR engine** + **full Poseidon AIR** (consistent with the native hash) | ✅ shipped + tested | — |
| **Merkle membership circuit** (ZK, depth 8) | ✅ shipped + tested | private witness |
| **Shielded SPEND circuit** M-in/N-out (membership + nullifier + conservation + **range-proofs**) + **ZK masking** | ✅ **shipped + tested** (amount not extractable, ≥128-bit conjectured soundness) | **real ZK proof** |
| On-chain tx `shield`/`shielded_transfer`/`unshield` + `PrivacyEnabled` gate | ✅ **wired** (state + wallet/CLI, nullifier dedup), **ON testnet/devnet** |
| **Community hardening** (bug bounty) | ⬜ — **mainnet blocker** |

> Full proof dossier (components, tests, reproducibility, reservations, call for
> audit): [docs/PREUVE-PHASE3.md](../PREUVE-PHASE3.en.md).

### STARK engine reservations (to harden / audit)

The engine (`internal/stark`) is an **in-house, unaudited prototype**. The
built-in adversarial review confirmed that the tested forgeries are rejected.
**Hardening** has since resolved several reservations:
- **Fiat-Shamir grinding shipped** (`FriParams.GrindBits`, default 16):
  anti-grinding proof-of-work before drawing the positions, +16 bits of
  soundness, verifier cost = 1 hash.
- **Sampling without replacement shipped** (`ChallengeIndicesDistinct`): FRI
  query positions pairwise distinct → exact per-query soundness.
- **Variable FRI folding depth shipped** (`FoldStopBits`); **batch inversions**
  (Montgomery) + denominator dedup + parallelization → prover ~77× faster;
  **M-inputs / N-outputs circuit** (join-split) shipped.
- **Range-proofs shipped**: note values bounded `< 2⁴⁸` (binary decomposition)
  → **value creation via modular overflow closed**.
- **≥128-bit conjectured soundness shipped**: 40 FRI queries + grinding +
  **multi-point OOD amplification** (3 independent out-of-domain points).
  Remaining: the **proven** bound (not conjectured), which would require an
  extension field for the FRI folding randomness.

### Warnings (not to oversell)

- **The real ZK circuit is the on-chain format.** The
  `SpendWitness`/`VerifyTransparent` of `internal/shielded` was the transparent
  prototype (reveals amounts) used to establish the model; it is **replaced** by
  the **real M-in/N-out zk-STARK circuit** (`internal/stark`,
  `poseidon_spendn.go`), which hides amounts and **is the one wired into
  consensus** (state + wallet/CLI).
- **Anonymity ≠ confidentiality.** ML-KEM encryption guarantees that no one
  **reads** a note. Whether the note is **unlinkable** to a *known* viewing key
  depends on ML-KEM's *key-privacy* (anonymity) — a distinct property **to
  verify**. We claim today **confidentiality**, not yet formal anonymity.
- **In-house crypto, not third-party audited.** This is why the `PrivacyEnabled`
  gate stays **OFF on mainnet** until community hardening, even though it is
  **ON on testnet/devnet** (usable, to be battle-tested).

## Plan by slices

| # | Content | Status |
|---|---|---|
| 1 | ML-KEM viewing keys + notes/commitment/nullifier + encryption/scan + transparent pool (+ tests) | ✅ shipped |
| 2 | Merkle tree of commitments (via `internal/smt`) + tx `shield`/`transfer`/`unshield`, **`PrivacyEnabled` gate** | ✅ shipped |
| 3 | **M-in/N-out zk-STARK circuit** (membership + ownership + nullifier + conservation + range-proofs) — deterministic prover/verifier | ✅ shipped |
| 4 | **Community hardening** (bug bounty) + ML-KEM key-privacy review + test vectors | ⬜ in progress — mainnet blocker |
| 5 | Mainnet activation by governance (`PrivacyEnabled` param) | ⬜ post-hardening (ON testnet/devnet) |

## Decision

Strong anonymity is a **major differentiator** (**post-quantum** privacy), but
its core — the zk-STARK circuit — is **in-house, non-third-party-audited**
crypto. We therefore built it in stages, **gated** (`PrivacyEnabled`): it is
**ON on testnet/devnet** (usable, to be battle-tested by the community) and
**will remain OFF on mainnet until community hardening**. A privacy bug is
silent (users believe themselves anonymous without being so) or worse (theft of
funds): "carefully coded" is not enough — hence the open hardening phase (bug
bounty) before any mainnet activation.
