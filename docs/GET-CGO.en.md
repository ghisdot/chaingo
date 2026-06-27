# Getting CGO

## On the public testnet (`chaingo-testnet-1`)

The testnet is a test network: the CGO obtained here have **no monetary
value**. They are used to experiment with transfers, create tokens and
contracts, delegate to validators, and stress the network.

### Method 1 — from the web wallet (recommended, no installation)

1. Open [chaingo.org/wallet](https://chaingo.org/wallet/).
2. Create a wallet (a password encrypts the seed in the browser, nothing
   is sent to a server).
3. Click the **Faucet** button (or start directly from the site)
   — the balance is credited within a few seconds.

### Method 2 — from the CLI

```bash
chaingo wallet new <name>
chaingo faucet --to <name> --amount 100 --api https://node.chaingo.org
chaingo balance <name> --api https://node.chaingo.org
```

### Method 3 — direct API call (curl)

```bash
curl -X POST https://node.chaingo.org/v1/dev/faucet \
  -H 'Content-Type: application/json' \
  -d '{"address":"<cg-address>","amount":100000000000}'
```

The amount is expressed in **ucgo** (1 CGO = 10⁹ ucgo). The example above
sends 100 CGO.

### Testnet faucet limits

The faucet is open on the testnet (by design, to make testing easier),
but limits may be applied at any time to prevent abuse.
If the request fails, retry later or join the community support
channel.

---

## On the mainnet (not yet launched)

The ChainGO mainnet **is not yet live**. Its launch is
conditioned on:

1. completion of Phase 2 of the roadmap (production security),
2. passing an external security audit,
3. the commitment of at least 4 independent validators,
4. finalization of the genesis document.

See [ROADMAP.md](../ROADMAP.md) for progress, and [MAINNET.en.md](MAINNET.en.md)
for the full procedure.

### Distribution planned at launch

| Share | CGO | Allocation |
|---|---|---|
| 50% | 500 M | **Community** — airdrops to testnet testers, adoption programs, early-holder rewards |
| 20% | 200 M | **Treasury** — funding development, audits, infrastructure |
| 15% | 150 M | **Team** — 4-year on-chain vesting (locked from block 0) |
| 10% | 100 M | **Ecosystem** — partnerships, developer grants |
| 5%  | 50 M  | **Genesis and liquidity** — stakes of the initial validators and market seeding |

### How to take part in community airdrops

The simplest way to be eligible for post-launch
distributions: **actively use the testnet**.

- Create a wallet and make transfers.
- Test the no-code contracts (vesting, escrow, multisig).
- Run a node, or even become a testnet validator (see
  [VALIDATOR.en.md](VALIDATOR.en.md)).
- Contribute to the source code (see [CONTRIBUTING.md](../CONTRIBUTING.md)).

The exact terms of the airdrops will be published before the mainnet
launch, on the official site.

### Exchange listings

No listing is in place yet. The process will begin after the mainnet
launch and the network's stabilization. Any announcement will go
through the project's official channels — be wary of intermediaries who
claim to sell CGO pre-mainnet.

---

## Resources

- [Web wallet](https://chaingo.org/wallet/) · [Explorer](https://chaingo.org/explorer/) · [API](https://chaingo.org/api/)
- [Validator & delegator guide](VALIDATOR.en.md)
- [Mainnet preparation](MAINNET.en.md)
- [Roadmap](../ROADMAP.md)