# Send: Wallet Token List and Balance Bar — Design Spec

Date: 2026-11-10
Status: Approved design

## 1. Goal

On Send, once a network is chosen, show every token the sending wallet holds
on that chain (like Phantom or MetaMask), unknown and spam tokens included, and
a balance bar above the amount with the wallet balance and its USD value.

## 2. Decisions

| Topic | Decision |
|---|---|
| Data source | Alchemy Portfolio API `POST https://api.g.alchemy.com/data/v1/{key}/assets/tokens/by-address`, metadata and prices included. One wallet, one network per call. |
| Key | `ALCHEMY_API_KEY` in `max-core/.env`, server-side only. |
| Networks | ethereum `eth-mainnet`, base `base-mainnet`, arbitrum `arb-mainnet`, optimism `opt-mainnet`, polygon `matic-mainnet`, bnb `bnb-mainnet`, avalanche `avax-mainnet`, solana `solana-mainnet`. |
| Fallback | Testnets, chains without a slug, or no key: native coin only, read through the chain's RPC URL. No RPC either: `HOLDINGS_UNAVAILABLE` (503). |
| Paying chains only | Receive-only chains (BTC, LTC, Tron) → `UNSUPPORTED_CHAIN`. |
| Included | Native coin always (even at zero); every token with a non-zero balance, spam included. |
| Order | Native first, then priced tokens by USD value (descending), then unpriced by symbol. |
| Labels | `verified` = native, or on MAX's verified token list. The UI labels the rest "Unverified". Unpriced → "No price". |
| Cache | 20 s per (chain, address); pages followed up to 5. |
| Endpoint | `GET /v1/holdings?chain=<key>&address=<addr>`, public, `lookupLimit`. |

## 3. Response

```json
{ "data": [ { "kind": "native" | "token", "address": "…" | "", "symbol": "BONK",
  "name": "Bonk", "logo": "https://…", "decimals": 5,
  "balance": { "raw": "120450000", "display": "1204.5" },
  "priceUsd": "0.0000153" | null, "valueUsd": "0.02" | null, "verified": false } ] }
```

## 4. Interfaces

- **SDK:** `client.holdings(chain, address): Promise<Holding[]>`.
- **Web Send:** "Native / Token by contract" becomes a token list for the
  chosen network (logo, symbol, name, balance, USD). Before a wallet is
  connected: "Connect wallet to see your tokens" (connects, sends nothing).
  "Paste a contract" stays as a fallback. A balance bar above the amount shows
  `Balance <amount> <symbol> · ≈ $<value>` and a Max button; the amount box
  shows its USD value. If the lookup fails, the old native / paste flow shows.
- **App Send:** the native / USDC pills become the same list for the phone's
  account on that chain; "Available" becomes the balance bar with USD and Max.

## 5. Out of scope

Swaps, price impact warnings, NFTs, holdings for receive-only chains.

## 6. Testing

Go unit tests for the Alchemy parser (hex balances, null token address,
missing metadata, prices, pagination, per-token errors), sorting, filtering,
verified flag, fallback and errors, using an `httptest` server. SDK unit test.
Web and app tsc and lint. Live check against Alchemy once the key is in `.env`.
