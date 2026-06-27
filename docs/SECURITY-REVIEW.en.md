# Security review report — ChainGO

> **Network**: public testnet `chaingo-testnet-1`
> **Nature**: **internal** security review (self-audit) + proof dossier.
> **Method**: deterministic adversarial tests, fuzzing, multi-node tests,
> soundness analysis of the proof system.

---

## 1. What this document is — and is not

This report is an **internal security review** conducted by the project team,
together with a **reproducible proof dossier**. **It is not an audit performed
by an independent third-party firm.**

ChainGO's security strategy is deliberate and explicit:

- **Standardized cryptography where it exists**: **ML-DSA-65** signatures
  (FIPS 204, NIST level 3), **SHA3-256** hashing, keystore sealing with
  scrypt + AES-GCM. No classical elliptic curves anywhere.
- **In-house cryptography for strong anonymity** (zk-STARK stack): it is
  **open, reproducible and hardened by a community** (open bug bounty,
  cf. §6), not by a firm. Its limitations are documented plainly (§4.4,
  §5) and its activation on mainnet is **locked behind a gate** until
  community hardening has taken place.

Every claim in this document is **backed by an executable test** (cf. §7). No
security assertion is made "on trust."

---

## 2. Scope and threat model

| Domain | Package | Threats considered |
|---|---|---|
| Post-quantum crypto | `internal/crypto` | signature forgery, malleability, address derivation |
| BFT consensus "Aurora" | `internal/consensus` | double-signing, network partition, deep reorg, loss of finality, equivocation |
| State machine | `internal/state` | non-determinism, non-atomicity, value creation, bypass of econ/token/contract rules |
| zk-STARK anonymity | `internal/stark` | proof forgery, value creation, note theft, amount extraction, conservation overflow |
| WASM VM | `internal/wasmvm` | non-determinism, non-termination (gas), non-deterministic opcodes |
| P2P network | `internal/p2p` | parsing of untrusted inputs, DoS by size |
| Genesis | `internal/genesis` | non-deterministic fingerprint |

**Assumptions**: byzantine adversary controlling < 1/3 of the stake for BFT
safety; unreliable network channels; hostile inputs (tx, blocks, P2P messages).

---

## 3. Methodology

- **~390 automated tests**, including **246 on the zk-STARK stack alone** and **31
  consensus fault tests**. Deterministic (no uncontrolled `time`/`rand`;
  fixed-seed PRNG).
- **Dedicated adversarial tests** (`*_adverse_*`, `*_fault_*`,
  `*_forgerie_*` files): each test **attempts an attack** and **requires rejection**.
- **Fuzzing** (6 `Fuzz*` targets) on parsing and instrumentation.
- **Multi-node tests**: 4 nodes must converge on the **same state root**
  (inter-node determinism).
- **Soundness analysis** of the proof system (FRI query budget +
  OOD amplification; cf. §4.4 and `docs/PREUVE-PHASE3.en.md`).

Fully reproducible (§7).

---

## 4. Results by domain

### 4.1 Post-quantum cryptography — ✅ standard

- All signatures (tx, blocks, votes) go through **ML-DSA-65**. Centralized
  in `internal/crypto`; no reintroduction of ECDSA/Ed25519 is possible without
  breaking the tested invariant.
- Addresses derived by hashing the public key; encrypted keystore
  (scrypt + AES-GCM), private key never transmitted (web wallet: everything local).
- **Residual risk**: ML-DSA's implementation maturity is still younger
  than that of classical curves (inherent to the entire PQ ecosystem).

### 4.2 BFT consensus "Aurora" — ✅ covered by fault tests

- **Stake-weighted deterministic proposer**; backup rounds for
  liveness.
- **Persistent BFT finality** (`LastCommit` ≥ 2/3) verifiable from the chain.
- Fault tests: **double-sign → slashing**, **partition → no finality
  without quorum**, **atomic deep (buried) reorg** (snapshot/restore,
  metadata erased only after success, **never below finality**),
  **prevote-the-lock** (POL lock).
- **Residual risk**: real decentralization depends on **independent
  validators** (an operational milestone, not code).

### 4.3 State machine — ✅ covered

- **Atomicity**: `Execute(strict=true)` snapshot/restore — a block that fails
  leaves no trace.
- **Determinism**: canonical JSON serialization (field order frozen, map
  keys sorted), clock = header timestamp (never the local clock).
- **Economic rules** = genesis parameters (never hard-coded):
  EIP-1559 (burned base fee + tip), stake inflation, unbonding.
- **Tokens**: unique symbol, **max-supply** cap applied at mint (+ anti-overflow
  guard), **burn** bounded to balance, bounded metadata.
- **No-code contracts** (vesting, escrow, multisig, DAO, presale, timelock,
  airdrop, streaming): **per-role** authorizations tested, fund
  conservation, terminal states.

### 4.4 zk-STARK anonymity (in-house) — 🟡 functional, hardened, NOT externally audited

**Post-quantum hash-only stack** (Goldilocks field, FRI, multi-column AIR,
Poseidon) + M-input/N-output shielded transaction circuit. Adversarial tests:
proof forgery, falsified/permuted/truncated OOD, non-conservation, note theft,
amount extraction — **all rejected**.

Hardening delivered and tested:

- **Soundness ≥ 128 bits (conjectured)**: 40 FRI queries + 16-bit grinding
  (term ≈ 136 b) **and multi-point OOD amplification** (3 independent
  out-of-domain points ⇒ Schwartz-Zippel error ~2⁻¹⁴⁴ over the 64-bit field).
- **Range-proofs**: note values bounded `< 2⁴⁸` ⇒ **value creation by
  modular overflow closed**; the state also rejects out-of-bound deposits.
- **Intra-tx nullifier dedup** on the state side (anti double-spend).
- **ZK masking** of amounts (tested as non-extractable).

**Assumed limitations** (cf. §5): Poseidon **non-standardized**, soundness
**conjectured** (not formally proven), **formal ZK** not demonstrated,
**ML-KEM key-privacy** to be established. → **Gate `PrivacyEnabled` OFF on mainnet**
until community hardening. Details: `docs/PREUVE-PHASE3.en.md`.

### 4.5 WASM VM — 🟡 functional, mainnet gate

- Determinism by **gas** (instrumentation **fuzzed over 5.3M executions**), a
  restricted opcode set validated at deployment, wazero interpreter.
- Verified by a **multi-validator** test (4 nodes, same root).
- **Gate `WasmEnabled` OFF on mainnet** until hardening.

### 4.6 P2P network & codecs — ✅ covered

- Binary codec with **bounded lengths** (anti-DoS); robust decoding, never
  panics on malformed input (tested, including truncated buffers).

---

## 5. Known limitations (transparency)

1. **No audit by an independent third party** — "self-audit +
   community hardening" strategy assumed.
2. **In-house privacy stack**: non-standardized Poseidon, **conjectured and
   unproven soundness** (the *proven* 128-b bound would require an extension field for
   the FRI folding randomness), formal ZK not demonstrated, ML-KEM key-privacy to be established.
3. **Decentralization**: independent validators not yet in place.
4. **Mainnet not open**: the `PrivacyEnabled` and `WasmEnabled` gates remain
   **OFF on mainnet** until both surfaces are hardened.

These limitations are **traceable** in `ROADMAP.md` (section "Upcoming").

---

## 6. Open attack surface (bug bounty)

ChainGO **invites** the community to attack the code. High-interest targets:

- forge a shielded spend proof for a non-existent note;
- **create value** (overflow, nullifier duplicate, conservation);
- steal a note without the `nk` key; extract an amount from a proof;
- break the collision resistance of Poseidon (in-house parameters);
- trigger a **state-root disagreement** between nodes (non-determinism);
- bypass a contract authorization (role, threshold, deadline).

Reporting: see [SECURITY.md](../SECURITY.md) (coordinated disclosure,
public credit). Everything is open-source (Apache 2.0) and reproducible.

---

## 7. Reproducibility

```bash
# Full suite (includes zk proofs — several minutes):
go test ./... -count=1

# Fast (skips heavy zk proofs):
go test -short ./...

# Targeted domains:
go test ./internal/consensus/ -run 'Fault|Reorg|Finality|Lock' -v   # BFT fault
go test ./internal/stark/ -run 'Forgerie|Soundness|Adverse|SpendN'  # zk soundness
go test ./internal/state/ -run 'Token|Template|Shield'              # state/tokens/contracts

# Fuzzing (example):
go test ./internal/wasmvm/ -run x -fuzz FuzzGasMetering -fuzztime 60s
```

**Latest full run**: see the table below (updated at each
review).

| Domain | Tests | Verdict |
|---|---|---|
| `internal/crypto` | 3 | ✅ |
| `internal/consensus` | 31 | ✅ |
| `internal/state` | 46 | ✅ |
| `internal/stark` | 246 | ✅ |
| `internal/types` | 24 | ✅ |
| `internal/wasmvm` | 13 | ✅ |
| others (`p2p`, `smt`, `store`, `genesis`, `shielded`…) | ~25 | ✅ |
| **Total** | **~390** | **✅ build + vet + tests green** |

---

*Document maintained alongside the code. Latest review aligned with the current
state of the repository. For the cryptographic detail of strong anonymity, see
[docs/PREUVE-PHASE3.en.md](PREUVE-PHASE3.en.md).*
