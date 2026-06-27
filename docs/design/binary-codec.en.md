# Compact binary codec

> Status: delivered (slices 1 to 5). Issue [#8](https://github.com/ghisdot/chaingo/issues/8).

ChainGO serializes transactions, blocks and votes in **compact binary** for
P2P transport and storage, where JSON was used before. Goal: reduce
bandwidth and disk footprint, and speed up encoding/decoding — without ever
touching the format that signatures cover.

## Founding invariant: `SigningBytes` stays canonical JSON

The binary codec is **strictly separated** from what a signature covers.

- `Transaction.SigningBytes()`, `Vote.SigningBytes()` and
  `Block.SigningBytes()` (header) stay **canonical JSON**: the field order
  of the struct IS the signed format.
- Consequence: any ML-DSA-65 signature produced before the codec arrived
  remains valid after any binary round-trip. The hash of a tx or a block
  (computed over `SigningBytes`) is unchanged.

This invariant is verified by dedicated tests: after `MarshalBinary` →
`UnmarshalBinary`, we re-verify the proposer's signature, the txs, the
votes and the equivocation proofs, and we compare the hashes.

## Primitives — `internal/codec`

Reflection-free encoder/decoder, with anti-DoS protections:

| Primitive | Encoding |
|---|---|
| unsigned integer | `uvarint` (LEB128) — 1 byte for small values |
| signed integer | zigzag `varint` |
| `bool` | 1 byte (0/1, any other value rejected) |
| `uint8` | 1 byte |
| `string` / `[]byte` | `uvarint` length + bytes; **length 0 = absent** |

Protections: `MaxBytesLen` (32 MB) on every length read, `ErrTruncated`
on a short buffer, `ErrTooLarge`, `ErrInvalidTag`, and `MustFinish()` which
detects trailing garbage bytes (anti-injection on P2P frames).

## P2P transport format (slice 3)

TCP gossip uses **frames**:

```
[1 byte: type][uvarint: payload length][payload]
```

| Code | Type | Payload |
|---|---|---|
| `0x01` | hello | `chain_id` + height |
| `0x02` | tx | `Transaction.MarshalBinary` |
| `0x03` | block | `Block.MarshalBinary` |
| `0x04` | vote | `Vote.MarshalBinary` |
| `0x05` | get_blocks | start height |

- Any unknown type **disconnects the peer** (future evolutions will be
  negotiated via the `hello`, not through silent codes).
- Limit: **16 MB per frame** (fits a full block, rejects huge forged
  frames).
- Optimized re-gossip: a valid frame received is rebroadcast **as is**,
  without re-marshaling the payload.

## Storage format (slice 4)

Blocks are persisted in binary in bbolt (bucket `blocks`), with
**lazy backward-compatible migration**:

```
old:  [raw JSON]                   → 1st byte '{' (0x7b)
new:  [0x01][Block.MarshalBinary]  → 1st byte 0x01
```

On read, the 1st byte routes decoding. No conversion pass: old JSON blocks
stay readable indefinitely, new ones are written in binary, and a mixed
database (post-upgrade) reads back without trouble. **State and genesis
stay in JSON** — the state root depends on `encoding/json` (map key
sorting), which is a determinism invariant between nodes.

## Measured gains

### Size (over bytes actually transmitted/stored)

| Object | JSON | Binary | Gain |
|---|---|---|---|
| Signed transaction | 7,410 B | 5,425 B | **27%** |
| Full block (header + 2 tx + 2 precommits) | 36,597 B | 26,999 B | **26%** |

### Encode + decode speed (`go test -bench=Codec -benchmem`)

| Object | Binary | JSON | Speedup |
|---|---|---|---|
| Transaction | 3.9 µs/op | 91.9 µs/op | **~23×** |
| Full block | 48 µs/op | 329 µs/op | **~6.8×** |

JSON was dominated by base64 encoding of the ML-DSA-65 signatures
(~3.3 KB each); binary writes them as is.

## Reproducing the measurements

```bash
# Sizes (logged by the compactness tests)
go test ./internal/types/ -run Compact -v

# Throughput + allocations
go test ./internal/types/ -bench=Codec -benchmem -run='^$'
```

## Compatibility

The binary P2P protocol is **not** compatible with the old JSON protocol:
all nodes on a network must run the same version. Storage, however, is
backward compatible (it reads old databases). At the testnet stage, a
coordinated redeployment of all nodes is enough.
