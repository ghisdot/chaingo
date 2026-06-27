# Brouillon de post de lancement

Trois formats prêts à adapter : **Show HN**, **Reddit (r/golang / r/cryptography)**
et **un fil X/Twitter**. Le ton vise un public **technique**, mise sur la
**transparence**, et évite tout vocabulaire de « shill ».

---

## 1. Show HN (Hacker News)

**Titre** (≤ 80 car., factuel, pas de hype) :

> Show HN: ChainGO – a post-quantum blockchain written from scratch in Go

**Corps** :

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

**Conseils HN** : poste en semaine, en matinée US. Réponds vite et techniquement
aux commentaires. N'efface pas les critiques — argumente.

---

## 2. Reddit

**r/golang** — angle « ingénierie Go » :

> **Title:** I wrote a post-quantum blockchain from scratch in Go (consensus,
> zk-STARK, WASM VM) — looking for feedback
>
> No framework, standard library + a few crypto deps. The fun Go bits: parallel
> PQ signature verification (~31k tps locally), a deterministic state machine
> (canonical JSON, sorted map keys — any non-determinism splits the chain), a
> from-scratch zk-STARK prover I profiled from 141s down to ~1.8s, and a
> gas-metered WASM interpreter. Code, tests and an honest security review are in
> the repo. Roast the design.

**r/cryptography** — angle « PQ + zk-STARK », **sans** prix de token (sub strict) :

> **Title:** A home-grown post-quantum zk-STARK shielded-transaction circuit
> (Goldilocks/FRI/Poseidon) — design notes & honest limitations
>
> Focus on the construction: DEEP-ALI multi-column AIR, Fiat-Shamir grinding,
> multi-point OOD soundness amplification over a 64-bit field, bit-decomposition
> range proofs to close modular value-creation. I'm explicit that Poseidon
> params are non-standard and soundness is conjectured, not proven. Feedback on
> the soundness analysis especially welcome.

**Règle Reddit** : lis les règles de chaque sub (beaucoup interdisent la promo de
token). Reste sur la **technique**, pas sur « investissez ».

---

## 3. Fil X/Twitter

> 1/ J'ai construit ChainGO : une blockchain **post-quantique** écrite de zéro en
> Go. Toutes les signatures en **ML-DSA-65** (standard NIST FIPS 204). Pas
> d'ECDSA nulle part. Testnet public en ligne. 🧵
>
> 2/ Pourquoi ? Le jour où un ordinateur quantique casse ECDSA, la quasi-totalité
> des chaînes deviennent forgeables. ChainGO part du principe inverse : la
> résistance quantique est l'exigence n°1, pas une option.
>
> 3/ Ce qui tourne déjà : consensus BFT (finalité, slashing), frais EIP-1559
> brûlés, staking/délégation, tokens & 8 contrats no-code déployables ET
> opérables depuis le navigateur.
>
> 4/ Le morceau de R&D : une pile **zk-STARK post-quantique maison** (sécurité
> hash-only, zéro trusted setup) avec transactions blindées — montants et
> destinataire cachés.
>
> 5/ Et je joue franc-jeu : crypto vie-privée **maison, non auditée par un
> tiers** → désactivée sur mainnet jusqu'au durcissement communautaire.
> Roadmap honnête, rapport de sécurité public.
>
> 6/ Open-source (Apache 2.0). Code + testnet + rapport de sécurité :
> github.com/ghisdot/chaingo · chaingo.org
> Venez l'attaquer : un bug bounty est ouvert. 🔓

---

## Checklist avant de poster

- [ ] Le testnet est **en ligne et stable** (le lien doit marcher quand le trafic arrive).
- [ ] Le **faucet** fonctionne (les curieux voudront essayer en 2 min).
- [ ] README + rapport de sécurité + BUG-BOUNTY à jour (la première impression technique).
- [ ] Tu es **disponible quelques heures** après publication pour répondre.
- [ ] Pas de promesse de prix / rendement / « to the moon » — uniquement la technique et l'honnêteté.
