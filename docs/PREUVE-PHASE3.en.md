# Phase 3 proof dossier — Post-quantum shielded transactions (ChainGO Phase 3)

> **Status: enabled on testnet/devnet, verified by a large battery of tests
> (positive + adversarial), security review in progress.** The `PrivacyEnabled` gate is
> ON on the test networks and remains OFF on mainnet until the security review
> is complete. This document gathers **what is built, what is proven by
> tests, and what is under review.** It is meant to be read by auditors
> (and skeptics).

## 1. In one sentence

ChainGO has an **entirely post-quantum, in-house shielded transaction
system**: a **zk-STARK** engine (security based on hashing alone,
with no elliptic curve and no *trusted setup*), an algebraic hash **Poseidon**,
and a **spend circuit** that proves in *zero-knowledge* that a spend is
valid — **without revealing the amount or the sender↔recipient link** — verified
by a battery of positive and adversarial tests.

## 2. Why "post-quantum"

| Building block | ChainGO choice | Why PQ |
|---|---|---|
| ZK proof | **zk-STARK** (FRI) | security = hashing (SHA3) only; no curve, no trusted setup |
| In-circuit hash | **Poseidon** over Goldilocks | algebraic, provable; hash security |
| Note encryption | **ML-KEM-768** (FIPS 203) | standardized quantum-resistant KEM |
| Signatures (chain) | **ML-DSA-65** (FIPS 204) | already everything else in ChainGO |

No primitive breakable by a quantum computer. This is the differentiator:
**privacy + post-quantum**, which neither Zcash (curves/SNARK) nor Monero are.

## 3. Where the computation happens (key performance point)

- **Prove** (generate the proof): in the **sender's wallet**, **off-chain**,
  **once** per transaction. Current cost ≈ **1.8 s** (≈**77×** faster than at
  the start: NTT, Montgomery-style batch inversion, deduplication of boundary
  denominators, deterministic parallelization — pure Go).
- **Verify**: on **each node**, in **milliseconds** (logarithmic STARK
  verification). **The network never does the heavy computation.**

## 4. The delivered stack (`internal/stark`, `internal/crypto`, `internal/shielded`)

| Component | File(s) | Commit |
|---|---|---|
| Goldilocks finite field + NTT | `field.go`, `ntt.go` | `1a70517` |
| Merkle (SHA3) + Fiat-Shamir transcript | `merkle.go`, `transcript.go` | `1a70517` |
| **FRI** (low-degree proximity) | `fri.go` | `1a70517` |
| DEEP-ALI STARK (Fibonacci toy) | `stark.go`, `stark_air.go` | `1a70517` |
| **Poseidon** hash + algebraic Merkle | `poseidon.go`, `merkle_poseidon.go` | `72a1780` |
| **Multi-column AIR** engine | `stark_mc.go` | `1c680b3` |
| **Full Poseidon AIR** (consistent with the native hash) | `poseidon_air_full.go` | `1c680b3` |
| Merkle **membership circuit** (ZK) | `membership_air.go` | `9165b44` |
| **Shielded spend circuit** 1-in/1-out + ZK masking | `poseidon_spend*.go` | `44aa0b9` |
| **M-input / N-output circuit** (join-split) | `poseidon_spendn.go` | (Phase 3 hardening) |
| ML-KEM view keys + note encryption | `internal/crypto/view.go` | `2be5ea4` |
| Shielded pool model (notes, nullifiers) | `internal/shielded/` | `2be5ea4` |

**STARK hardening** ("zk-STARK hardening" track):
- **Fiat-Shamir grinding** (`FriParams.GrindBits`): anti-grinding proof-of-work
  before the position draw, +16 bits of soundness, verifier cost = 1 hash.
- **Query sampling WITHOUT REPLACEMENT** (`ChallengeIndicesDistinct`).
- **Variable FRI folding depth** (`FoldStopBits`).
- **Soundness ≥128 bits conjectured**: **40 queries** (FRI term ≈136 b) +
  **multi-point OOD amplification** (`mcExtraOodPoints` = 2 independent
  out-of-domain points in addition to the main one ⇒ OOD error ~2⁻¹⁴⁴ over Goldilocks).
- **Range-proofs** (`poseidon_spendn.go`, `snRangeBits=48`): each note value
  bounded `< 2⁴⁸` by binary decomposition in the circuit — closes the modular
  overflow of conservation (value creation).
- **Prover ~77× faster** (141 s → ~1.8 s): batch inversion (Montgomery),
  `x^n` as a geometric series, boundary-denominator dedup, deterministic
  parallelization (`parallel.go`). Bit-for-bit identical outputs (determinism preserved).

## 5. What is PROVEN by tests (reproducible)

```bash
go test ./internal/stark/ -count=1 -v     # entire STARK stack + shielded circuit
go test ./internal/crypto/ -run View      # ML-KEM note encryption
go test ./internal/shielded/               # pool model
```

> Reference result: **`internal/stark` = all sub-tests PASS, 0 FAIL**.
> Since hardening, the prover runs in **~1.8 s/proof** (versus ~95–140 s
> previously): batch inversion, denominator dedup and parallelization
> brought a **~77×** factor, without changing the result (determinism verified).

**Spend circuit (`TestSpend*`, 24 tests, all PASS):**
- `TestSpend_PreuveHonnete` — a valid spend produces a proof that verifies.
- `TestSpend_TemoinNonPublie` — the witness (amount, `nk`, path) appears in
  no public value.
- `TestSpendAdv_BitsMontantNonExtractiblesEnClair`, `…PasDeCelluleBruteDansPreuve`
  — **ZK masking** (randomized LDE): the amount is not extractable from the proof.
- Negatives **all rejected**: `NullifierFaux`, `NoteHorsArbre`, `OutCmFalsifie`,
  `FeeAnnonceeFausse`, `RejeuAutreEnonce`, `DesequilibreProuve`,
  `VolNoteAutruiNkFaux` (steal another's note without their key), `TraceFalsifieeRejeteeParSTARK`.

**Exact statement proven** (1 input / 1 output + fee):
- PUBLIC: `merkleRoot`, `nullifier`, `outCm`, `fee`.
- PRIVATE: `inValue`, `inRho`, `nk`, Merkle path; `outValue`, `outOwnerTag`, `outRho`.
- CONSTRAINTS: `inCm = commit(inValue, PoseidonHash(nk), inRho)` ; `inCm ∈ tree(merkleRoot)` ;
  `nullifier = Hash2(nk, inCm)` ; `outCm = commit(...)` ; `inValue = outValue + fee`.

**M-input / N-output circuit (`TestSpendN_*`, join-split)** — generalizes the
circuit beyond 1-in/1-out (merging AND splitting of notes):
- PUBLIC: `merkleRoot`, `nullifier_i` (i<M), `outCm_j` (j<N), `fee`.
- CONSTRAINTS: for each input i, `inCm_i = commit(...) ∈ tree(merkleRoot)` and
  `nf_i = Hash2(nk_i, inCm_i)` ; for each output j, `outCm_j = commit(...)` ; and
  **conservation by signed accumulator** `Σ inValue_i = Σ outValue_j + fee`.
- Mechanics: linear chaining of Poseidon blocks with a "witness-load" glue
  mode (`mPackNk`) to start each input on a fresh key, and
  padding to a power of two with identity blocks.
- Tests PASS: honest proofs (1,1)(1,2)(2,1)(2,2) ; **negatives rejected**:
  non-conservation, falsified nullifier, falsified `outCm` ; determinism.

## 6. Integrated adversarial reviews (attempted forgeries → rejected)

Each stage has an "attacker" pass (`*_adverse_test.go`, `*_forgerie_test.go` files):
- **FRI / STARK**: random (non-low-degree) function rejected; OOD that is lying but
  consistent in z rejected; grafting of a foreign FRI proof rejected; replay on a
  different statement rejected; Merkle falsification (root/leaf/path) rejected.
- **Poseidon**: MDS proven to be genuinely MDS; non-degenerate constants; disjoint
  Hash/Hash2 domains; forging an arbitrary digest rejected.
- **Multi-column**: unbound column, partial/permuted OOD, false MDS → rejected.
- **Spend**: note theft without `nk`, value creation, double-spend (nullifier),
  amount extraction → rejected.
- **M-input / N-output**: non-conservation (`Σ in ≠ Σ out + fee`), falsified
  nullifier, falsified `outCm` → rejected.

## 7. Points under security review

**In-house** crypto: here are the points currently **under security review**,
to be challenged in priority:

1. **In-house Poseidon parameters** (Cauchy MDS matrix + constants derived by
   SHAKE256). Not a standardized Poseidon. Collision/preimage resistance **not established**.
2. **Concrete soundness** → **128-bit target (conjectured) reached**: `blowup=8`,
   **40 queries** + **16-bit grinding** ⇒ FRI query term ≈ 40·log₂8 + 16 =
   **136 bits**. The limiting term was the **DEEP/OOD** step: over a
   64-bit field (Goldilocks), a **single** out-of-domain point `z` bounds the
   Schwartz-Zippel error to ~2⁻⁴⁸. We now draw **3 independent out-of-domain points**
   (`mcExtraOodPoints=2` in addition to the main one), each subjected to the
   constraint identity and to the DEEP recombination: the OOD error drops to
   ~(2⁻⁴⁸)³ ≈ **2⁻¹⁴⁴**. The overall **conjectured** soundness
   (list-decoding regime, like Plonky2 /
   Winterfell over 64-bit fields) is therefore **≥128 bits**. Still to establish: the
   **proven** bound (non-conjectured, Johnson analysis) — which would require an
   **extension field** for the FRI folding randomness (documented formal step).
3. ~~Sampling with replacement~~ → **resolved**: sampling **without replacement**
   (distinct positions) delivered.
4. **ZK masking**: randomized LDE present and tested (amount not extractable), but
   *formal zero-knowledge* (proven indistinguishability) is not demonstrated.
5. **Circuit scope**: tree depth **spendDepth=12 (4096 leaves)** —
   shielded pool capacity raised from 16 to **4096 notes**. **Multi-input /
   multi-output** (join-split) and **variable FRI folding depth** delivered.
   **Range-proofs delivered**: each note value (input as well as output) is bounded
   to **< 2⁴⁸** by bit decomposition in the circuit, which **closes the
   value-creation attack by overflow** (`Σ` modulo Goldilocks); the state layer
   additionally rejects any deposit `shield ≥ 2⁴⁸` (otherwise an unspendable note).
   Remaining: variable PER-PROOF tree depth (today a single format).
6. **ML-KEM anonymity**: note *confidentiality* is guaranteed; *unlinkability*
   (ML-KEM key-privacy) remains to be established.
7. ~~Performance ~95 s~~ → **resolved**: **~1.8 s/proof** (≈77× faster).

## 8. Call for community audit

ChainGO **embraces** the "in-house crypto + audit by a community of
hackers" strategy. Everything is open (Apache 2.0) and reproducible. Suggested attack
targets: forge a spend proof for a non-existent note, create value, steal
a note without `nk`, extract an amount from a proof, break the collision resistance
of Poseidon, exploit Fiat-Shamir grinding. The code lives in `internal/stark`.

## 9. Status & next step

- ✅ **Cryptographic stack + shielded circuit**: delivered, tested.
- ✅ **zk-STARK hardening**: Fiat-Shamir grinding, sampling without replacement,
  variable FRI depth, **M-input / N-output** circuit, prover ~77× faster
  (~1.8 s). Delivered, tested.
- ✅ **Range-proofs + soundness ≥128 bits (conjectured)**: note values bounded
  `< 2⁴⁸` (anti value-creation), 40 FRI queries + **multi-point OOD
  amplification** (3 independent out-of-domain points). Delivered, tested (negatives
  included: out-of-bound value and falsified extra OOD points → rejection).
- ✅ **On-chain wiring**: `shield`/`shielded_transfer`/`unshield` tx wired into
  consensus (commitment tree + nullifier set in the state root,
  STARK verification), **activated on testnet/devnet** (gate `PrivacyEnabled` ON).
- ⏭ **`state`/wallet integration of the M-in/N-out format** (1-in/1-out is active).
- ⏭ **Security review** — in progress; to be finalized before mainnet activation.

> This is a **functional and tested system**, activated on the test networks, on the
> path to strong post-quantum anonymity — the security review is in progress before
> the mainnet opening.
