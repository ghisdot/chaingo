# Design — Network upgrades & version governance

> How to push an upgrade (feature or security fix) without leaving
> vulnerable nodes lying around or breaking the network.

## Principle: 3 distinct cases

| Type of upgrade | The out-of-date node… | Mechanism |
|---|---|---|
| **Consensus** (validation rule, signed format) | drops off / forks on its own (blocks "invalid" to it) | **Flag day**: activation by height |
| **Non-consensus** (e.g. DoS fix in a decoder) | stays compatible but **vulnerable** | **Kick at handshake** (protocol version) |
| **Network/P2P** (new message, frame format) | doesn't understand → errors | **Protocol version** |

Key point: a **consensus** change is **self-enforced** — an out-of-date
node cannot validate the new blocks and leaves the chain on its own. The
risk is therefore not the laggard that "pollutes", but the **split** if
the upgrade is not coordinated → hence the flag-day.

## Delivered: protocol version + kick + alert

- `p2p.ProtocolVersion` (current binary protocol = **v2**) is announced in
  the `Hello`. `p2p.MinPeerProtocol` = minimum accepted version.
- **KICK**: a peer below `MinPeerProtocol` is **disconnected** at the
  handshake. A laggard cannot stay connected → forced to update. This is
  the "block out-of-date nodes", at the network level.
- **Bidirectional ALERT**:
  - we log each kick (`[p2p] KICK … protocol vN < minimum vM`);
  - if a peer announces a version **higher** than ours, then WE are the
    ones behind → log `⚠ OUT-OF-DATE NODE` + `Server.Outdated()` = true,
    exposed in `/v1/status` (`outdated`), shown as a **red banner** on the
    validator dashboard.
- Transition backward-compat: an old `Hello` (without the version field)
  decodes as v0 (legacy) → kicked; an old node fails to read our longer
  `Hello` → disconnects. The transition therefore requires a **coordinated
  update** (acceptable at the testnet stage, like the other format changes).

## Upcoming: flag-day for consensus changes

For a consensus rule change to switch over **cleanly and together**: add an
`activation_height` (in the Params/genesis) — "from block N onward, the new
rule applies". Everyone updates BEFORE N; at N, up-to-date nodes switch
together, laggards fork onto a dead chain. Deterministic, no surprises
(Bitcoin/Ethereum model).

## Auto-update policy

**No auto-update imposed on third-party validators**: auto-updating
consensus software is dangerous (a bad update = global network outage;
supply-chain vector). We **announce**, we **kick** the too-old, we **alert**
the laggards; operators update **manually before the flag-day**.

Acceptable, on the other hand, for **our own** testnet nodes: a systemd
timer that fetches a **signed release** (ML-DSA, consistent with the
project) then rebuilds/restarts — without imposing it on others.

## Typical upgrade procedure

1. Develop + test (green CI on the platforms).
2. If a consensus change: set a future `activation_height` with margin.
3. Bump `ProtocolVersion` (and `MinPeerProtocol` if we want to kick the old
   ones).
4. Announce (community channels) the version + the activation height.
5. Deploy the bootstrap nodes, then the validators, BEFORE the height.
6. Laggards see the "OUT OF DATE" banner and/or are kicked.
