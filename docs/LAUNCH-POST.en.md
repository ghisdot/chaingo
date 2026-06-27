# Launch post draft

Three ready-to-adapt formats: **Show HN**, **Reddit (r/golang / r/cryptography)**
and **an X/Twitter thread**. The tone targets a **technical** audience, bets on
**transparency**, and avoids any "shill" vocabulary.

---

## 1. Show HN (Hacker News)

**Title** (≤ 80 chars, factual, no hype):

> Show HN: ChainGO – a post-quantum blockchain written from scratch in Go

**Body**:

> ChainGO is an L1 blockchain where **every signature** — transactions, blocks,
> consensus votes — uses **ML-DSA-65** (FIPS 204, the NIST post-quantum
> signature standard). No ECDSA/Ed25519 anywhere; hashing is SHA3-256. It's
> written from scratch in Go, no framework.
>
> What's actually working on the public testnet today:
> - PoS + BFT consensus (deterministic stake-weighted proposer, persistent
>   finality, slashing, tested fork-choice/reorg under partition);
> - EIP-1559 fees (burned base fee + free-market tip), staking/delegation;
> - no-code tokens (mintable/cap/burnable) and 8 contract templates
>   (vesting, escrow, multisig, DAO, presale, timelock, airdrop, streaming),
>   deployable **and operable** from the browser;
> - an experimental **home-grown post-quantum zk-STARK** stack (Goldilocks field,
>   FRI, Poseidon) with a working shielded-transaction circuit — hidden amounts
>   and recipient, hash-only security, no trusted setup;
> - a WASM VM (deterministic via gas metering).
>
> I'm deliberately transparent about what's **not** done: the privacy crypto is
> home-made and **not third-party audited** (it stays gated off on mainnet until
> community hardening), soundness is **conjectured ≥128-bit** (not formally
> proven — that would need an extension field for FRI), and the chain isn't
> decentralized yet (the next milestone is independent validators).
>
> Internal security review + reproducible proof dossier, ~390 tests (246 on the
> zk-STARK stack), full suite green.
>
> Repo: https://github.com/ghisdot/chaingo · Site/testnet: https://chaingo.org
>
> Happy to answer questions about the consensus, the from-scratch zk-STARK, or
> the design trade-offs.

**HN tips**: post on a weekday, in the US morning. Reply quickly and technically
to comments. Don't delete criticism — argue.

---

## 2. Reddit

**r/golang** — "Go engineering" angle:

> **Title:** I wrote a post-quantum blockchain from scratch in Go (consensus,
> zk-STARK, WASM VM) — looking for feedback
>
> No framework, standard library + a few crypto deps. The fun Go bits: parallel
> PQ signature verification (~31k tps locally), a deterministic state machine
> (canonical JSON, sorted map keys — any non-determinism splits the chain), a
> from-scratch zk-STARK prover I profiled from 141s down to ~1.8s, and a
> gas-metered WASM interpreter. Code, tests and an honest security review are in
> the repo. Roast the design.

**r/cryptography** — "PQ + zk-STARK" angle, **without** token price (strict sub):

> **Title:** A home-grown post-quantum zk-STARK shielded-transaction circuit
> (Goldilocks/FRI/Poseidon) — design notes & honest limitations
>
> Focus on the construction: DEEP-ALI multi-column AIR, Fiat-Shamir grinding,
> multi-point OOD soundness amplification over a 64-bit field, bit-decomposition
> range proofs to close modular value-creation. I'm explicit that Poseidon
> params are non-standard and soundness is conjectured, not proven. Feedback on
> the soundness analysis especially welcome.

**Reddit rule**: read each sub's rules (many ban token promotion). Stay on the
**technical** side, not on "invest".

---

## 3. X/Twitter thread

> 1/ I built ChainGO: a **post-quantum** blockchain written from scratch in
> Go. Every signature in **ML-DSA-65** (NIST FIPS 204 standard). No
> ECDSA anywhere. Public testnet online. 🧵
>
> 2/ Why? The day a quantum computer breaks ECDSA, nearly all
> chains become forgeable. ChainGO starts from the opposite premise: quantum
> resistance is requirement #1, not an option.
>
> 3/ What's already running: BFT consensus (finality, slashing), burned
> EIP-1559 fees, staking/delegation, tokens & 8 no-code contracts deployable AND
> operable from the browser.
>
> 4/ The R&D piece: a **home-grown post-quantum zk-STARK** stack (hash-only
> security, zero trusted setup) with shielded transactions — amounts and
> recipient hidden.
>
> 5/ And I play it straight: privacy crypto **home-made, not third-party
> audited** → disabled on mainnet until community hardening.
> Honest roadmap, public security report.
>
> 6/ Open-source (Apache 2.0). Code + testnet + security report:
> github.com/ghisdot/chaingo · chaingo.org
> Come attack it: a bug bounty is open. 🔓

---

## Checklist before posting

- [ ] The testnet is **online and stable** (the link must work when traffic arrives).
- [ ] The **faucet** works (the curious will want to try in 2 min).
- [ ] README + security report + BUG-BOUNTY up to date (the first technical impression).
- [ ] You are **available for a few hours** after publishing to respond.
- [ ] No promise of price / yield / "to the moon" — only the technique and honesty.
