# Design — WASM virtual machine (arbitrary smart contracts)

> Status: **shipped and wired into consensus on testnet/devnet** (`internal/wasmvm` +
> tx `wasm_deploy`/`wasm_call`, per-contract storage in the state root).
> Enabled by the genesis Param **`WasmEnabled`**: **ON** on devnet/testnet,
> **OFF on mainnet** until **community hardening** — safety invariant:
> no arbitrary code is executed in mainnet consensus before this hardening. The
> testnet exists precisely to battle-test this engine before mainnet.

## Objective

Let developers deploy **arbitrary contracts** (not just the no-code templates),
like the EVM on Ethereum/BNB — but in **WebAssembly**, more modern and
multi-language (Rust, AssemblyScript, TinyGo, C).

## The execution engine (`internal/wasmvm`)

`internal/wasmvm` loads and **actually executes** WASM via **wazero** (pure Go
runtime, **no CGO** — preserves the project's native Windows/Linux/macOS
compilation):
- `Run(wasm, fn, timeout, args…)`: instantiates a module, minimal host API
  (`env.log`), bounded memory (16 MiB), wall-clock timeout (sandbox).
- **`RunMetered(wasm, fn, gasLimit, args…)`: DETERMINISTIC GAS** — see below.
- Tests: runs a real `add(i32,i32)`, interrupts an infinite loop, fails cleanly
  on an invalid module / missing function.

## ✅ Slice 1 SHIPPED: deterministic gas by instrumentation (`meter.go`)

The real blocker is lifted. `instrument(wasm, gasLimit, cost)` **rewrites the
bytecode**: adds an i64 gas global (init = gasLimit, **no renumbering** since it
is added at the end of the index space) and injects a **gas charge** (decrement +
test; `unreachable` if < 0) at the head of **each function body** and **each
`loop`**. Since loop and recursion are the only ways to repeat work in WASM,
these two points **guarantee halting**: no module runs beyond its gas, and the
halting point is **identical on all nodes** (deterministic out-of-gas, not a
timeout).

Safety: opcode set restricted to the MVP subset; any unknown opcode
(SIMD/atomics/bulk-mem 0xfc-0xfe…) makes the module **rejected** (never a guessed
jump). Correct **signed LEB128** encoding (≠ Go's zig-zag). **Fuzzed**: 5.3 M
executions on arbitrary bytecode without a panic. Tests: the semantics are
preserved (instrumented `add` = 42), an infinite loop halts by gas in < 1 s.

CLI: `chaingo wasm run --gas N <file.wasm> <fn> [args]`.

**v1 scope**: **halting** guarantee. Fine-grained pricing (gas proportional to
work, per basic block) is a refinement (fixed charge per point for now).

## How determinism is held (in consensus)

Executing arbitrary code on all nodes requires a **bit-for-bit identical**
result, otherwise the state roots diverge and the chain splits. The guarantees
in place:
- **Deterministic halting** by gas instrumentation (cf. slice 1) — no wall-clock
  timeout in the consensus path.
- **Restricted opcode set**, *validated at deployment* (`wasmvm.Validate`): any
  unmastered opcode (SIMD, atomics…) makes the bytecode rejected → only
  instrumentable code lands on-chain.
- **Forced wazero interpreter** (`RunDeterministic`): the same execution path on
  all CPU architectures.
- **Deterministic imports only**: `env` module (storage, caller, value,
  transfer, log) — no clock, no randomness, no threads.
- **Atomic revert**: trap/out-of-gas → contract effects ignored, fees burned
  (anti-DoS); success → storage/balance/transfers committed atomically.

Verified by a **multi-validator integration test**: deployment then call via the
real consensus path, 4 nodes agreeing on the state root at each step.

## What is still MISSING (before mainnet activation)

1. **Community hardening** (hostile bytecode execution, bug bounty) — the mainnet
   **blocker**.
2. **Floating-point review**: NaN canonicalization or ban at deployment (today:
   allowed, the wazero interpreter is deterministic; to be confirmed by the
   audit).
3. **Fine-grained gas pricing**: cost proportional to work per basic block
   (today: fixed charge per point — guarantees halting, not a gas market).
4. **Finer anti-DoS limits**: stack depth, number of exports (already in place:
   max bytecode size, 16 MiB memory, gas cap).

## Implementation plan (slices)

| Slice | Content | Status |
|---|---|---|
| 1 | Gas instrumentation (bytecode injection) + fuzzing | ✅ **shipped** |
| 2 | Fine-grained pricing (cost per basic block) + determinism review | ⬜ (gas = halting only; hardening in progress) |
| 3 | **SANDBOXED state host API** (`host.go`: storage_read/write, caller, value, transfer, log) | ✅ **shipped** |
| 3b | Wiring the host API onto the REAL state machine (per-contract storage in the root) | ✅ **shipped** |
| 4 | Tx `wasm_deploy` / `wasm_call` + fees + determinism (interpreter) | ✅ **shipped** (testnet/devnet) |
| 5 | Anti-DoS limits (bytecode size ✅, memory ✅, gas ✅; stack ⬜) + **community hardening** ⬜ | 🟡 partial |

## Host API (`env` module)

A contract can call: `storage_read/write` (per-contract KV), `caller`, `value`,
`transfer`, `log`. Memory ABI by (pointer, length). In consensus, these hosts
are backed by the REAL state machine: the contract's storage is loaded from the
root, re-committed atomically on success. Example Rust contract:
[examples/contracts/counter](../../examples/contracts/counter).

## Deploy / call

- **Studio** (no-code, web): "WASM Contracts" tab → upload a compiled `.wasm` →
  tx signed by the post-quantum wallet. Call form + list.
- **CLI**: `chaingo wasm deploy --from <wallet> <file.wasm>` then
  `chaingo wasm call --from <wallet> [--gas N] [--value CGO] <address> <fn> [args]`.
  `chaingo wasm list` lists the contracts. `chaingo wasm run` remains the local
  sandbox.
- **API**: `GET /v1/wasm/contracts`, `GET /v1/wasm/contracts/{addr}` (+ storage).
  The bytecode is submitted in the `code` field (base64) of a `wasm_deploy` tx.

A contract's address = hash of its deployment tx. Fees: `WasmDeployFee` /
`WasmCallFee` burned (genesis Params).

## Decision

The WASM engine is a **strong differentiator** (arbitrary contracts +
post-quantum). It is **shipped and wired** on testnet/devnet — that is where we
battle-test it with real contracts and traffic. It remains **disabled on mainnet
(`WasmEnabled=false`)** until **community hardening**: it executes potentially
hostile code, and the project's invariant is to never run that in mainnet
consensus without this review. The **8 no-code templates** (vesting, escrow,
multisig, DAO, presale, timelock, airdrop, streaming) remain the recommended
option for the majority of uses, with no VM attack surface.
