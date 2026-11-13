# MAX web

Web frontend for MAX, built from the "MAX Web Frontend" canvas: Landing, Connect, Claim, Dashboard, Profile and Send. It uses Next.js 16 (App Router, Turbopack), React 19, Tailwind 4, TanStack Query, zustand and react-hook-form + zod. It talks to `max-core` through `@max/sdk` (linked from `../max-sdk`).

## Run it

```bash
npm install
cp .env.example .env.local
npm run dev            # http://localhost:3000
```

`max-core` must be running (`NEXT_PUBLIC_MAX_API_URL`, `http://localhost:8080` by default), and this origin must be in its `CORS_ORIGINS`. Sessions are the API's HttpOnly `max_session` cookie, so requests go out with `credentials: "include"`.

Checks: `npx tsc --noEmit`, `npm run lint`, `npm run build`.

## Pages

| Route | What it does |
| --- | --- |
| `/` | Landing with a live handle availability check |
| `/connect` | Sign in with Phantom (SIWS) or an EIP-6963 EVM wallet (SIWE). `?mode=link` links another wallet |
| `/claim` | Claim a handle for yourself or an agent |
| `/dashboard` | Your IDs and per-network destinations, linked wallets, sent and received activity |
| `/[handle]` | Public profile |
| `/send` | Resolve a handle, prepare the payment, sign in your wallet, record it with MAX |
| `/agents` | Your AI agents; create one |
| `/agents/[id]` | Approve or deny held payments, claim agent handles, link agent wallets, edit the spending policy (per-chain limits + recipient allowlist), issue/rotate/revoke API keys (secret shown once), activity |
| `/rename/[id]` | Paid rename: cooldown check, availability, choose a fee option, sign the fee, watch it apply |
| `/verify` | Reserved names: verify an X account, claim a name reserved for it, see claims and re-verify |
| `/market` | Search listings (name, price range, sort, cursor pagination); Buy now opens the quote, lock countdown and verified Solana signing |
| `/dashboard/admin/market` | Admins only (max-core answers 404 to everyone else): review queue, sales, listings, offers, restrictions |
| `/verify/x` | Where MAX's X OAuth callback lands (`?x=verified\|error`) |

## Structure

```
src/app/            routes + providers (QueryClient, session boot, toasts, ErrorBoundary)
src/api/            SDK client singleton, account-scoped query keys
src/services/       session boot/logout, wallet discovery + connect, transfer signing
src/store/          session status (no persisted auth flag), wallet, flow drafts, toasts
src/hooks/          React Query hooks, debounce
src/components/     ui primitives, identity, payments, Header, RequireAuth
src/error/          error-code → copy map, ErrorBoundary
src/validation/     zod schemas mirroring API rules
```

Chain reads and broadcasts (blockhashes, mint decimals, payment lookups) go through the API's `POST /v1/rpc/:chain` proxy, so no RPC keys ship to the browser. Marketplace env: `NEXT_PUBLIC_MARKET_ASSETS` (`chain:mint:decimals`, comma-separated, mirrors max-core `MARKET_ASSETS`, e.g. `solana-devnet:4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU:6`). The decimals pin how a shown price becomes base units before signing (cross-checked against the mint on chain); the first entry is also the asset for offers on names with no listing or sale history. Optional `NEXT_PUBLIC_MARKET_TREASURY` pins the MAX Solana treasury (max-core `TREASURY_SOLANA`): when set, a sale whose fee goes elsewhere is refused before Phantom opens. See `.env.example`.

Top-level route names (`agents`, `verify`, `rename`, `dashboard`, …) are reserved by `max-core` migration 000007 so no handle is shadowed by a page.

X verification needs `X_CLIENT_ID` / `X_CLIENT_SECRET` in `max-core`; without them `/verify` explains that it isn't set up.

## Payment safety

- **Senders confirm against a fresh resolution.** Prepare passes `resolutionVersion`, so a recipient wallet change between review and signing fails with `RESOLUTION_STALE` instead of paying the old address.
- **One idempotency key per exact set of terms.** Retrying never creates a second payment.
- **The wallet signs exactly MAX's instructions**, and only from the address the payment was prepared for.
- **Retrying a failed record doesn't re-sign.** If recording the tx hash with MAX fails, a retry re-submits the same hash. A leave-page guard protects in-flight sends.

The mobile app lives in `../max-app`.
