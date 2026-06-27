# Checklist — "listing readiness" (spread out over time)

Honest from the start: ChainGO is a **sovereign L1** (not an ERC-20). Getting
"listed" cannot be decreed — it is a **tiered** journey measured in
**months**, gated by mainnet, decentralization and liquidity. Here is the
realistic order.

---

## Step 0 — Prerequisites (before any thought of listing)

- [ ] **Mainnet live and stable** (the testnet does not get listed).
- [ ] **Independent validators** ([VALIDATOR-CHECKLIST.en.md](VALIDATOR-CHECKLIST.en.md)) —
      a single-operator chain is not listable.
- [ ] **Clear, published token distribution** (genesis, on-chain vesting,
      team/community/treasury share — already documented in
      [MAINNET.en.md](MAINNET.en.md)).
- [ ] **Public explorer** + **stable public API** (aggregators and
      exchanges need them to index supply, transactions, etc.).

## Step 1 — Data aggregators (the first real "listing")

CoinGecko / CoinMarketCap: the most accessible tier. They typically require:

- [ ] Mainnet live + public **explorer** + **API**.
- [ ] **Verifiable circulating supply** (dedicated endpoint, e.g. `/v1/supply`).
- [ ] **Website**, logo, descriptions, social links, open-source repository.
- [ ] An **active market** (even a small one) where the token trades — see Step 2.
- [ ] Submission form filled out, team contacts.

> Effect: price/supply/volume visible publicly. This is what people
> often call "being listed", without an exchange.

## Step 2 — Liquidity (essential before any exchange)

A sovereign L1 has no market by default. Two paths:

- [ ] **Bridge + wrapped CGO** to an established chain (e.g. a wCGO) to
      create liquidity on an existing **DEX**; **or**
- [ ] **Native market** (your own order book / AMM) with bootstrap
      liquidity.
- [ ] **Real liquidity** locked (timelock/vesting — the ChainGO contracts
      do this), for credibility and anti-rug.

## Step 3 — CEX (the high bar)

A centralized exchange has to **integrate your chain** (run a node,
wire up deposits/withdrawals). Near-systematic expectations:

- [ ] **Volume & liquidity** demonstrated (from steps 1-2).
- [ ] **Active and real community** (no inflated numbers).
- [ ] ⚠️ **Security audit by a third-party firm** — most CEXs
      require it for their due diligence. **This is where the decision to skip
      the external audit comes back**: not for your own confidence, but as an
      **imposed requirement**. To budget if a CEX listing is a serious goal.
- [ ] **Legal clarity**: token classification (utility vs security) per your
      jurisdiction, team KYC/AML, sometimes a legal entity.
- [ ] **Technical integration**: node integration doc, clear finality
      (confirmation depth), proven stability.
- [ ] Often **listing fees** and/or market-making.

---

## Reality & sequencing

```
Mainnet → Independent validators → Liquidity (bridge/DEX) → Aggregators (CG/CMC) → CEX (+ third-party audit)
```

- What is **free and soon feasible**: aggregators (from mainnet +
  minimal liquidity).
- What **costs money and takes months**: CEX (third-party audit, legal, liquidity).
- **Trap**: paying for a "fast listing" on an obscure exchange or a
  shady market-maker. Don't do that — it burns the hard-won credibility
  of your honest approach.

## What helps, and what you already have

- ✅ Open-source code, public **security report**, honest roadmap → good
  due-diligence signal.
- ✅ Token with **transparent rules** (genesis, no hidden premine).
- ✅ A **differentiating narrative** (natively post-quantum) — rare, and
  appealing to a technical audience.

> Next concrete step on the listing side: **nothing**, as long as mainnet and
> independent validators are not there. Focus the energy on Step 0.
