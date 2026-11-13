# MAX: pay a name, not an address

MAX gives every wallet a readable name, like `max.alice`, that works on every chain. Anyone can pay that name with whatever coin they hold; the recipient gets paid on the chain and in the coin they chose. When the two differ, the conversion settles through **NEAR Intents** in the same payment. MAX never holds funds or keys: the sender always signs from their own wallet.

AI agents can use MAX too, through an MCP server and a TypeScript SDK, inside spending limits their owner sets.

- **Website:** https://www.usemax.xyz (also https://app.usemax.xyz)
- **API:** https://api.usemax.xyz
- Built at the NEAR Hackathon, 2026 (see [Hackathon disclosure](#hackathon-disclosure)).

## The problem

Sending crypto means copying a 42-character address, checking you're on the network the recipient asked for, and bridging or swapping first when your chains don't match. One wrong character and the money is gone. AI agents that pay for things hit the same wall, with less judgement.

## What MAX does

| Feature | What it means |
|---|---|
| **Names that resolve per chain** | `max.alice` points to Alice's wallet on Ethereum, Base, Arbitrum, Optimism, Polygon, BNB Chain, Avalanche and Solana, plus receive-only Bitcoin, Litecoin and Tron addresses. The sender picks a chain; MAX returns the one address for it, or says there isn't one. |
| **Pay in anything, receive anything** | The sender pays with what they hold. "Recipient receives" lets them pick the chain, then any coin NEAR Intents delivers there (104 assets across 11 chains today). |
| **Your own wallets** | Sign in with any EVM-compatible or Solana wallet by signing a message, never a payment. Link more wallets, choose which receives on each chain, or paste a receive-only address (marked *Unverified*). |
| **Every account gets a name** | A free default ID (like `max.6f8d6gum9e2sy5fa`) from first sign-in, so anyone can be paid straight away. |
| **Custom names** | Claim `max.yourname` by signing a claim message with a linked wallet. Claims need a $50 balance on a mainnet chain (testnet coins don't count) to discourage squatting. |
| **Reserved brand names** | Names like `max.solana` or `max.near` can only be claimed by proving the matching X account over OAuth. |
| **Renames and marketplace** | Paid renames, and a marketplace to list, offer on and buy names with on-chain settlement checks (Solana USDC, 10% fee). |
| **Agents** | Owners authorize agents with scopes and a policy: per-transfer and 24-hour limits per asset, a list of names they may pay, and a minimum sale price. Anything outside the policy waits for the owner. |

## How NEAR fits in

Cross-chain payments are built on **NEAR Intents (1Click)**: [`max-core/internal/routing/nearintents/client.go`](max-core/internal/routing/nearintents/client.go).

1. **What can be delivered:** `GET /v1/routes/assets` lists every current 1Click token on MAX's chains (deprecated 1Click entries are skipped, since they can share a contract with their replacement).
2. **Quote:** when the sender's chain or coin differs from what the recipient receives, `POST /v1/transfers/prepare` asks 1Click for a quote to the recipient's resolved address.
3. **Validate before signing:** MAX checks the quote against the payment (recipient, amounts, slippage, deadline, decimals) in [`max-core/internal/routing/validate.go`](max-core/internal/routing/validate.go) and caps a route's USD value.
4. **One signature:** the sender signs a single deposit to the 1Click deposit address from their own wallet.
5. **Track to completion:** the payment worker follows the deposit with the 1Click status API until it settles or refunds.

So someone holding SOL can pay `max.alice` and she receives BTC, USDC on Base or USDT on Tron, with no bridge screens and no custodian.

## Try it

On the hosted site (https://www.usemax.xyz), with an EVM-compatible or Solana wallet installed:

1. **Look up a name:** type `theyodafx` in "Find your handle" on the home page. You see that it's taken; open its profile to see the chains it receives on.
2. **Sign in:** click *Connect wallet*, pick your wallet and sign the message. You land on the dashboard with your free default ID and the wallet you signed in with.
3. **Choose where you're paid:** on the dashboard, set which wallet receives on each chain, or paste a receive-only Bitcoin address. Your public profile updates at once.
4. **Claim a name:** go to *Claim*, type a name and click *Claim*. Your wallet asks you to sign "Claim max.yourname for your MAX account"; after that the name is yours. (Needs $50 on a mainnet chain in a linked wallet.)
5. **Pay a name across chains:** open *Send*, enter a name, pick your network, then *Recipient receives* → pick a chain → pick a coin. The confirmation shows "They receive: USDC on Base". You see the NEAR Intents quote before you sign.

API only, no wallet needed:

```bash
curl "https://api.usemax.xyz/v1/resolve/max.theyodafx?chain=base"
```

```bash
curl https://api.usemax.xyz/v1/routes/assets
```

## Run it locally

Requirements: Go 1.27, Node 22+, Docker.

```bash
cd max-core && cp .env.example .env && make up && make migrate && make run
```

The API listens on `:8080` with a local Postgres and Redis from Docker. `.env.example` explains every setting; cross-chain routes need `NEAR_INTENTS_API_KEY` (optional; without it 1Click charges its platform fee) and mainnet `RPC_URL_*` values.

```bash
cd max-sdk && npm install && npm run build
```

```bash
cd max-frontend && cp .env.example .env.local && npm install && npm run dev
```

The website runs on http://localhost:3000. The mobile app (`max-app`) is an Expo app: `npm install && npx expo start`.

## Repository

| Folder | What it is |
|---|---|
| [`max-core`](max-core) | Go API: names, wallets, resolution, payments, NEAR Intents routing, renames, marketplace, agents, X verification. Postgres via sqlc and golang-migrate. |
| [`max-frontend`](max-frontend) | Next.js website. |
| [`max-app`](max-app) | Expo mobile app: a self-custody wallet you can pay by name (TestFlight coming). |
| [`max-sdk`](max-sdk) | TypeScript SDK (`@max/sdk`). |
| [`max-mcp`](max-mcp) | MCP server for agents (`max_resolve`, `max_propose_transfer`, market tools). It never signs or submits payments. |
| [`security-review`](security-review) | The security review done during the hackathon; its findings are fixed on `main`. |
| `MAX_PRODUCT_SPEC.md`, `max-core/docs/` | Product spec and design notes for each feature. |

## Tests

```bash
cd max-core && go test ./...
```

```bash
cd max-core && TEST_DATABASE_URL=postgres://max:max@localhost:5433/max_test?sslmode=disable go test -tags integration ./internal/server/
```

```bash
cd max-sdk && npm test
```

```bash
cd max-mcp && npm test
```

The SDK and MCP packages also have end-to-end suites against a running API: `MAX_E2E=1 MAX_API_URL=http://localhost:8080 npm run test:e2e`.

## Hackathon disclosure

- **Timing:** the repository's first commit is from 23 October 2026, after the hackathon was announced on 19 October. Everything here was built in the hackathon window.
- **AI assistance:** much of the code was written with Claude Code.
- **Libraries:** Go: gin, pgx, golang-migrate, sqlc, go-playground/validator, decred secp256k1, mr-tron/base58, zap. Web: Next.js, React, TanStack Query, react-hook-form, zod, zustand, viem, @solana/web3.js, @solana/spl-token. App: Expo, React Native, @noble/curves, @scure/bip32 and bip39, viem, @solana/web3.js. Agents: @modelcontextprotocol/sdk. Chain and token logos come from [@web3icons/core](https://github.com/0xa3k5/web3icons) (MIT).
- **External services:** NEAR Intents 1Click (routing), Alchemy (RPC and token balances), CoinGecko (prices), X OAuth (reserved names).

## License

[MIT](LICENSE)
