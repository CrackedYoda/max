Build the web frontend for **MAX**, a universal payment identity for crypto. People and AI agents claim a handle like `max.yoda`, link wallets they already own, and anyone can then pay them by typing that handle. MAX never holds funds or keys. Only the sender signs; the recipient does nothing.

Read `../MAX_PRODUCT_SPEC.md` for product context and `../max-core/README.md` for the exact API contract. The Go API runs at `http://localhost:8080`. Treat the README as the source of truth for endpoints, request/response shapes and error codes.

## Stack
- Next.js (App Router) + TypeScript + Tailwind CSS + shadcn/ui
- EVM: wagmi + viem (MetaMask / injected + WalletConnect)
- Solana: `@solana/wallet-adapter-react` (Phantom)
- TanStack Query for API state
- API client: use the local SDK at `../max-sdk` (`npm install ../max-sdk`, then `new MaxClient({ baseUrl })`). It already handles `credentials: "include"`, unwraps `{data}`, throws `MaxApiError` with `.code`, types every response and provides `parseUnits` / `formatUnits` / `normalizeName`. Don't hand-roll fetch calls
- Base URL from `NEXT_PUBLIC_API_URL` (default `http://localhost:8080`). The app runs on `http://localhost:3000`, which must match the API's `AUTH_URI` / `CORS_ORIGINS`.

## Visual direction: warm orange on white
Calm, editorial, trustworthy. Think Claude/Anthropic's warmth, not neon crypto.
- Background `#FAF9F5` (warm off-white), surfaces `#FFFFFF`, borders `#E8E6DC`
- Text `#141413`, muted text `#6B6A63`
- **Accent orange `#D97757`**, hover `#C6613F`, soft tint `#F5E6DF` for selected/badge backgrounds
- Success `#2F7D5B`, danger `#B33A3A`
- Dark mode: background `#1F1E1D`, surface `#262624`, text `#F5F4EE`, same orange accent
- Define every color as a CSS variable/Tailwind token. Never hardcode hex values in components.
- Type: a refined serif for display headings (e.g. Fraunces or Source Serif) and a clean sans for UI (Inter). Monospace for addresses and handles.
- Generous whitespace, 12–16px radii, hairline borders, very soft shadows. Orange is used sparingly: primary buttons, focus rings, the `max.` prefix, active states.
- The handle is the hero. Always render it as `max.` in orange followed by the name in the text color, in mono.

## Pages
1. **Landing `/`**: a hero line in the spirit of "Send crypto to a name, not an address." A big live handle search box calls `GET /v1/ids/availability` (debounced 300ms) and shows available / taken / reserved / invalid with the `detail` text. Primary CTA: "Claim yours". Add a short 3-step explainer: Connect → Claim → Get paid.
2. **Connect / sign in `/connect`**: choose Phantom or MetaMask. Flow: connect wallet → `POST /v1/wallets/challenge {purpose:"login"}` → show the exact message in a readable preview with a "This does not authorize any payment" reassurance → sign → `POST /v1/wallets/verify`.
   - Solana signature = base58 of the `signMessage(new TextEncoder().encode(message))` bytes. Use `bs58`.
   - EVM signature = `signMessage({ message })` hex.
3. **Claim `/claim`** (auth): name input with the same live availability check, a human/agent toggle and an optional display name → `POST /v1/ids`. Map the error codes `MAX_ID_TAKEN`, `MAX_ID_RESERVED`, `MAX_ID_VERIFICATION_REQUIRED` ("Reserved — verify with X, coming soon") and `MAX_ID_LIMIT_REACHED` to friendly copy.
4. **Dashboard `/dashboard`** (auth):
   - **My MAX IDs**: cards showing handle, type badge, and per-namespace destinations (EVM / Solana) with truncated addresses and copy buttons. Edit the profile with `PATCH`. A per-namespace destination picker calls `PUT /v1/ids/:id/destinations/:namespace` with a wallet from the user's linked wallets.
   - **Wallets**: list from `GET /v1/wallets`. "Link another wallet" runs the challenge flow with `purpose:"link"`. Remove calls `DELETE`; handle `LAST_WALLET` and `WALLET_ALREADY_LINKED`.
   - Sign out → `POST /v1/auth/logout`.
5. **Public profile `/[handle]`** (e.g. `/max.yoda` or `/yoda`): `GET /v1/profiles/:maxId` shows display name, avatar, description, and chain chips for reachable vs. unreachable chains. Include a "Send" button.
6. **Send `/send`**: recipient handle input, a chain selector from `GET /v1/chains`, and asset = native or a pasted token contract/mint address (never pick a token by symbol alone). Steps:
   1. `GET /v1/resolve/:maxId?chain=&asset=`. Show a review card with the resolved address (full, mono, copyable), network and `notes`. `NO_COMPATIBLE_WALLET` → "max.x has no wallet on this chain yet". Never fall back to another chain.
   2. On confirm: `POST /v1/transfers/prepare` with `{recipient, chain, asset, amount, senderAddress, idempotencyKey: crypto.randomUUID() (generated once per review), resolutionVersion}`. Show `warnings` prominently, plus the asset's `verified` badge and `amount.display`. `RESOLUTION_STALE` → re-run resolve and ask the user to review again.
   3. Sign exactly what `instructions` says. EVM: `sendTransaction({to, data, value: BigInt(value), chainId})` via wagmi, switching chains first if needed. Solana: build a tx with a `SystemProgram.transfer`, or with `createAssociatedTokenAccountIdempotentInstruction` + `createTransferCheckedInstruction` using the returned accounts, mint, decimals and `tokenProgram`.
   4. `POST /v1/transfers/:id/submit {txHash}`, then a live status screen polling `GET /v1/transfers/:id` every 2s. Show a stepper (Signed → Submitted → Confirming n/m → Completed), the explorer link, and `statusReason` on FAILED / MANUAL_REVIEW.
   Put all chain-signing code in `lib/transfer.ts`.
   **Cross-chain option:** on the Send page, a "Recipient receives" selector (default: same as sent) sets `destinationChain` / `destinationAsset`, plus an "I want them to receive exactly" toggle (`amountType: "output"`). When the response has `route: "cross_chain"` (including automatically, when the profile shows a `settlement` preference and they have no wallet on your chain), show a quote card before signing: you send `crossChain.amountIn`, they receive at least `minAmountOut` (expected `expectedAmountOut`), USD values, ETA, slippage, a live countdown to `expiresAt`, and the refund-to address. Show "via NEAR Intents". After submit, extend the stepper: Deposit confirmed → Swapping → Delivered (link each `crossChain.destinationTxs` entry), or Refunded with `refundReason`. Handle `ROUTE_UNAVAILABLE`, `QUOTE_REJECTED` (show its message), `ROUTE_LIMIT_EXCEEDED`.
   **Settlement preference** (dashboard, per MAX ID): "If someone pays on a chain I don't have a wallet for, convert it to ___ on ___" → `PUT /v1/ids/:id/settlement`. Mainnet chains only; show `SETTLEMENT_NO_WALLET` as "add a wallet on that chain first".
7. **Activity** (dashboard tab): `GET /v1/transfers?direction=received` and `?direction=sent`. Each row shows the counterparty handle, amount + symbol, chain, a status pill and an explorer link.

8. **Agents** (`/dashboard/agents`, signed-in humans): see the "AI agents" section of the README.
   - List the user's agents with status pills, plus a "Create agent" dialog.
   - Agent detail page with tabs:
     - **Overview:** pause/resume toggle; claim an agent ID (`max.<name>`); link the agent's receiving wallet. The user connects the agent's wallet in their browser, then the app calls `/v1/agents/:id/wallets/challenge` and `/verify`.
     - **Policy:** a per-asset limits table (chain, native or verified token, max per transfer, max per 24h), a recipient allowlist with handle autocomplete, and an "allow any recipient" switch with a warning. Show `INVALID_POLICY` messages inline.
     - **Keys:** issue a key with scope checkboxes and optional expiry. Show the secret once, in a copy-only modal that says it won't be shown again. Also rotate and revoke, plus last-used and expiry.
     - **Approvals:** pending transfers showing amount, recipient, chain, the policy `reasons` and the agent's `instruction`. Label the instruction "Agent's request (not an authorization)" and show it in a muted quote block. Approve and Deny buttons; Deny takes an optional reason.
     - **History:** all decisions.
   - Show a badge with the pending-approval count in the dashboard nav.
9. **Rename** (on each MAX ID card in the dashboard): a "Rename" action opens a dialog.
   - **Eligibility:** read it from `GET /v1/ids/:id/rename`. If not eligible, show a countdown to `renameAvailableAt` with "Renames are limited to once every 72 hours".
   - **Request:** new-name input with the live availability check (treat `pending_rename` and `quarantined` as unavailable, with friendly copy), plus a fee picker built from `fees`. Spell out what happens: "Your old name will be held for 30 days so no one can impersonate you. Payments already sent are unaffected."
   - **Pay:** sign `instructions`, then submit the hash to `/v1/transfers/:feePaymentId/submit`.
   - **Track:** poll `GET /v1/ids/:id/rename/:requestId` with the stepper Fee sent → Confirming → Renamed. On `failed` or `expired`, say "No rename happened and your rename allowance is unchanged."
   - **History:** a "Previous names" list from `/v1/ids/:id/history`.

10. **Reserved names via X**:
   - **Availability:** when availability returns `reason: "reserved"` with `claimPolicy` `x_oauth` or `manual_review`, show "Reserved for @{xHandle} on X" and a "Verify with X" button.
   - **Verify:** the button calls `POST /v1/verifications/x/start` and does a full-page redirect to `authorizeUrl`. Never use a popup or iframe; the callback must land in the same browser session.
   - **Result page `/verify/x`:**
     - `?x=verified`: show the verified @username and immediately offer "Claim max.{name}" (`POST /v1/reserved-claims`). Mention that verifications must be used within 15 minutes.
     - `?x=error`: map `code` to friendly copy. `OAUTH_SESSION_MISMATCH` → "Start verification again from this browser". `X_ACCOUNT_LINKED_ELSEWHERE` → "This X account already verifies another MAX account". `X_HANDLE_MISMATCH` → "You signed in to X as a different account".
   - **Pending review:** a 202 shows "Under review — we'll confirm this brand name manually".
   - **Dashboard:** list reserved-name claims with their status, plus a "Re-verify by {verifiedUntil}" reminder. A lapsed ID gets a red banner ("This ID is paused until you re-verify @{xUsername}") and a re-verify button.
   - **Admin view:** `/admin`, shown only when `GET /v1/admin/reserved-claims` succeeds. A review queue with approve/deny/revoke and a required note.

## Quality bar
- Fully responsive down to 360px. Keyboard accessible. Visible orange focus rings. WCAG AA contrast (orange text on white only at ≥18px or bold; use the hover shade for small orange text).
- Skeletons for loading, clear empty states, inline error messages keyed off API error codes, toasts for success.
- Truncate addresses as `0x5aAe…eAed` in lists, but always show the full address on the send confirmation.
- No mock data in shipped pages. Every screen talks to the real API.
- Show testnet chains with a "Testnet" badge.
- Provide `.env.example` and a README covering how to run against `max-core` locally.
