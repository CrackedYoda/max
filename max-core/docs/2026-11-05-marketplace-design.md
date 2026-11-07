# MAX Marketplace — Design Spec

Date: 2026-11-05
Status: Implemented on `feat/marketplace`; updated 2026-11-06 to match what was built

### Changes after implementation

The sections below were updated so the spec matches the code. In short:

1. Agent market policy is `{canSell, minSalePrices: [{chain, asset, amount}]}`;
   selling in an asset with no minimum goes to `pending_approval` (§4, §8).
2. A prepared transfer already in flight when a name is sold still pays the
   old owner; only new prepares are refused (§2, §6).
3. A finalized transaction that pays toward the sale but does not match goes
   to `manual_review`; any other mismatch is treated as not found (§5).
4. The transaction deadline is `greatest(lock_expires_at, submitted_at) +
   30m`; then the sale expires with no strike (§6).
5. `GET /v1/agents/:agentId/market-authorizations?decision=`; approvals are
   API/SDK/MCP only in v1 (§8, §9).
6. New error codes: `NAME_LISTED`, `OWNER_CHANGED`, `RENAME_PENDING`,
   `SALE_LIMIT`, plus `MAX_ID_LIMIT_REACHED` at purchase (§9).
7. Admin routes 404 for non-admins; `GET /v1/admin/market/restrictions`;
   approve takes an optional reason (§9).
8. Client env: `NEXT_PUBLIC_MARKET_ASSETS` / `EXPO_PUBLIC_MARKET_ASSETS` as
   `chain:mint:decimals`, `NEXT_PUBLIC_RPC_SOLANA[_DEVNET]`, and the optional
   `*_MARKET_TREASURY` pins (§11).
9. An offer's spend counts when it is paid; offers are never held; new `pay`
   authorization action; migration `000010` columns (§4, §8).
10. Paused trading still allows delist, withdraw and decline;
    `MARKET_DISABLED` is a 404 (§7).
11. Bodies: accept `{payoutAddress?}` (required when the name has no
    listing); pay's `idempotencyKey` is optional (§9).
12. Complete: `max_ids.type` follows the buyer's account type; the
    destination is set only if the paying wallet is still linked; the
    seller's listing is sold even for an offer sale; a lapsed offer paid
    late completes and loses its strike (§6).
13. Expiry and late payment: an expired sale with a tx expires its offer
    with no strike; a late tx moves the sale to `confirming` and re-locks
    the listing; a late payment completes even after a relist at the old
    price (§6).
14. Admin and guards: deny declines the offer and reactivates the listing;
    cancel from `locked` (no tx) or `manual_review`; a tx after a cancel is
    observed before any `manual_review`; `pending_review` auto-cancels after
    24h; at most 2 open sales per buyer; a 1h buy cooldown after an unpaid
    buy-now expiry (§6, §7, §9).
15. Pre-flight: native ≥ network fee + rent for missing token accounts +
    the payer's rent-exempt floor; only the buyer's token account balance
    counts; classic Token program only; the fee payer must be the buyer
    (§2, §5).
16. Registration gate: X- and manual-review reserved claims are not gated;
    only chains with an RPC count; testnets are skipped in production; EVM
    balances come from a read-only reader (§5).
17. `MARKET_FEE_BPS` is `0..5000`; `MARKET_HOLD_PERIOD` must be positive;
    `MARKET_MIN_OFFER` must keep the fee above zero (§10).
18. SDK: `assertSaleInstructionsMatch(sale, instructions, expect?)` with
    `SaleExpectations`, `buildSolanaSaleTransaction`,
    `buildVerifiedSolanaSaleTransaction(view, payer, blockhash, expect?)`
    and the method names as shipped (§12).
19. App sale flow is `SaleReview.tsx` + `saleTx.ts`; web cards show no last
    sale (§11).
20. A ban blocks buying and offers (`MARKET_RESTRICTED`); public
    `NOT_LISTABLE` reasons are masked to `unavailable`; the MAX ID cap
    applies at purchase (§3).

## 1. Goal

Let users and agents buy and sell registered MAX IDs peer to peer. A buyer
becomes the full owner of the name, exactly as if they had claimed it. MAX
takes a 10% fee from the seller's proceeds. Settlement is a single atomic
on-chain transaction; MAX never holds user funds.

The same work adds a **registration gate**: claiming a new name requires a
minimum USD balance across the claimant's linked wallets.

### In scope (v1)

- Fixed-price listings ("Buy now").
- Offers on any listable name, listed or not.
- Solana + USDC only (mainnet USDC in production, devnet USDC in dev).
- Agents as first-class traders under scopes and the existing policy engine.
- Admin controls and anti-fraud rules.
- Registration balance gate on claims.
- Web, app, SDK and MCP surfaces.

### Out of scope (v1)

- Auctions (v1.1; will need escrow, decided then).
- EVM, BTC, LTC, Tron settlement (the adapter interface leaves room; EVM
  will use a stateless splitter contract).
- Counter-offers.
- Email/push notifications (in-product inbox only).
- Primary sales of MAX-owned names.
- Automatic refunds (any refund is a manual admin action from the treasury).

## 2. Decisions

| Topic | Decision |
|---|---|
| Market | Peer-to-peer resale; agents and humans both trade. |
| Tradable names | Only registered names (existing `max_ids`). |
| Listing types | Fixed price + offers. Auctions later. |
| Settlement | One atomic transaction pays treasury and seller together. Solana: one tx with two `transferChecked` instructions + memo. No contract in v1. |
| Assets | Solana USDC only, set by `MARKET_ASSETS`. |
| Fee | 10% (`MARKET_FEE_BPS=1000`), deducted from the seller. Buyer pays the listed price. |
| Fee destination | Per-namespace treasury `TREASURY_<NS>`, falling back to `RENAME_TREASURY_<NS>`. |
| Pre-flight | Before any tx is built: the buyer's own token account (classic Token program only) holds ≥ price, since only it is spent from; buyer SOL ≥ network fee + rent for any missing token accounts + the payer's rent-exempt floor. The fee payer must be the buyer. |
| Registration gate | Sum of stablecoins (USDC, USDT at $1) + native coins (ETH, SOL, POL via price feed) across linked wallets ≥ `REGISTRATION_MIN_BALANCE_USD` (default 100; `0` disables). Checked once at claim. Feed failure or stale price fails closed. |
| Offers | Bound to one linked wallet that must hold the offer amount + gas, re-checked continuously. |
| Owner change | Destinations, settlement prefs and profile cleared; buyer's paying wallet set as destination if still linked to the buyer; `resolution_version` bumped; history kept; "owner changed" shown for 30 days. A prepared transfer already in flight still pays the old owner; only new prepares see the new owner. |

## 3. Eligibility

A name is **listable** when all hold:

- `max_ids.status = 'active'` and the owner account is `active`.
- Not backed by a live `reserved_name_claims` row (X- or manual-review-claimed
  names are never tradable).
- `now() >= max_ids.listable_at` (holding period, `MARKET_HOLD_PERIOD`,
  default 72h, set on claim, rename and purchase).
- `now() >= max_ids.rename_available_at` (not inside a rename cooldown).
- No rename request `awaiting_payment`.
- The owner has no active `ban` in `market_restrictions`. A ban also blocks
  the account from buying and making offers (`MARKET_RESTRICTED`).
- For agent-owned names: the agent has `market:sell`; listing/accepting
  without policy `market.canSell = true` goes to human approval.

`NOT_LISTABLE` carries a reason code. Anyone but the owner sees moderation
reasons (`market_banned`, `owner_suspended`) and `payout_wallet_removed` as
`unavailable`. Buy, offer and accept also enforce the buyer's MAX ID limit
(10, counting purchases in progress): `MAX_ID_LIMIT_REACHED`.

While a name has an active listing, renames are refused. While a name has an
open sale (`pending_review`, `locked`, `submitted`, `confirming`), the listing
cannot be changed or delisted and profile/destination edits are refused.

Blocked reserved names never exist as MAX IDs, so they are never tradable.

## 4. Data model (migration `000009_marketplace`)

### `market_listings`

| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| max_id | uuid FK max_ids | |
| seller_account_id | uuid FK accounts | |
| chain | text | e.g. `solana` |
| asset_address | text | verified token, canonical |
| asset_decimals | int | |
| price_base_units | text | `^[1-9][0-9]{0,77}$` |
| payout_address | text | must be a wallet linked to the seller, same namespace |
| status | text | `active, locked, sold, cancelled, admin_delisted` |
| status_reason | text | shown to seller on admin delist |
| expires_at | timestamptz null | optional |
| created_at, updated_at | timestamptz | |

Unique partial index: one row per `max_id` where status in (`active`, `locked`).

### `market_offers`

| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| max_id | uuid FK | |
| buyer_account_id | uuid FK | |
| from_address | text | linked wallet that must fund and pay |
| chain, asset_address, asset_decimals | | |
| amount_base_units | text | ≥ `MARKET_MIN_OFFER` |
| status | text | `live, unfunded, accepted, declined, superseded, expired, withdrawn, lapsed, completed` |
| decline_note | text | |
| expires_at | timestamptz | default +7d, max +30d |
| funded_checked_at | timestamptz | |
| created_at, updated_at | | |

Unique partial index: one row per (`buyer_account_id`, `max_id`) where status
in (`live`, `unfunded`, `accepted`). Index on (`status`, `funded_checked_at`)
for the funding worker.

### `market_sales` (immutable snapshot)

| Column | Type | Notes |
|---|---|---|
| id | uuid PK | also the memo `max:sale:<id>` |
| max_id | uuid FK | |
| listing_id | uuid null FK | set for buy-now |
| offer_id | uuid null FK | set for accepted offers |
| seller_account_id, buyer_account_id | uuid FK | |
| buyer_address | text | the only allowed sender |
| seller_payout_address | text | |
| treasury_address | text | |
| chain, asset_address, asset_decimals | | |
| price_base_units | text | |
| fee_bps | int | |
| fee_base_units | text | `price * fee_bps / 10000`, rounded down |
| seller_base_units | text | `price - fee` |
| quoted_network_fee | text | lamports, informational |
| idempotency_key | text | unique per (buyer_account_id, key) |
| status | text | `pending_review, locked, submitted, confirming, completed, expired, cancelled, manual_review, reversed` |
| status_reason | text | |
| lock_expires_at | timestamptz | |
| tx_hash | text null | |
| confirmations | bigint | |
| submitted_at, completed_at, next_check_at | timestamptz | |
| created_at, updated_at | | |

- Exactly one of `listing_id` / `offer_id` is set (CHECK).
- CHECK `fee + seller = price` is enforced in code and re-verified in the trigger.
- Unique partial index: one row per `max_id` where status in
  (`pending_review`, `locked`, `submitted`, `confirming`).
- Unique partial index on (`chain`, `tx_hash`) where `tx_hash` is not null.
- Trigger `market_sales_snapshot_immutable` rejects changes to every party,
  address, asset and amount column, and to `tx_hash` once set (same pattern as
  `payments_snapshot_immutable`).

### `market_sale_events`

`id, sale_id, event_type, from_status, to_status, detail jsonb, created_at`,
mirroring `payment_events`.

### `market_restrictions`

`id, account_id, kind (ban | offer_suspension), until timestamptz null,
reason, created_by, created_at, lifted_at`. Active = `lifted_at is null and
(until is null or until > now())`.

### `market_strikes`

`id, account_id, offer_id, created_at`. Three strikes in 30 days create an
`offer_suspension` for 30 days.

### `agent_market_authorizations`

Same shape as `agent_transfer_authorizations`, with `action (buy | offer |
list | accept | pay)`, nullable `sale_id`, `offer_id`, `listing_id`, `decision`,
`reasons`, `policy_version`, `amount_base_units`, `decided_by`, `decided_at`.

Migration `000010_agent_market_approvals` adds the `pay` action and three
columns: `payload jsonb` (the full input, so an approval replays the action
exactly), `failure text` (set when the action or its replay failed; such an
authorization never counts toward spend) and `finished_at timestamptz`.

### `market_buy_cooldowns` (migration `000011_market_guards`)

`buyer_account_id, listing_id` (primary key), `sale_id`, `until`,
`created_at`. Written when a buy-now sale expires unpaid. The same migration
adds partial indexes for stale reviews, open sales per buyer and
transactions submitted after a cancel.

### Changes to existing tables

- `max_ids`: add `owner_changed_at timestamptz null`, `listable_at timestamptz
  not null default now()`. Backfill `listable_at = created_at`.
- `max_id_ownership_events`: add `sale_id uuid null references market_sales(id)`.
- `reserved_names`: insert `('market', 'blocked') on conflict do nothing`. The
  migration fails loudly if a MAX ID named `market` already exists, so an
  operator decides; it never renames silently.
- `agent_policies.limits` is reused for buy/pay spend; a new
  `market jsonb not null default '{}'` column holds
  `{ "canSell": bool, "minSalePrices": [{ "chain", "asset", "amount" }] }`
  (decimal amounts per chain and asset). Selling in an asset with no
  minimum goes to `pending_approval`.

## 5. Components

| Unit | Responsibility | Depends on |
|---|---|---|
| `internal/market` | Listings, offers, sales, eligibility, state machine, handlers, worker, admin | maxids repo, agents policy, settle, audit |
| `internal/market/settle` | `Adapter` interface: `Quote`, `CheckFunds`, `BuildTx`, `Observe`. v1: `solana` adapter | `solanarpc`, `chains` |
| `internal/pricing` | USD price for native coins, cached, fail-closed | HTTP (CoinGecko by default) |
| `internal/maxids` | Registration gate in `Claim`; `TransferOwnership(ctx, q, TransferParams)` (runs inside the caller's transaction) | pricing, wallet balances via settle adapters |
| `internal/agents` | New scopes; policy evaluation for market actions | |

### Settlement adapter

```go
type Adapter interface {
    // Quote returns the network cost of the settlement tx and whether
    // recipient token accounts must be created.
    Quote(ctx, Order) (Quote, error)
    // CheckFunds verifies the payer holds the asset amount and enough
    // native coin for Quote.NetworkFee.
    CheckFunds(ctx, Order, Quote) error
    // BuildTx returns the instructions for one atomic tx.
    BuildTx(Order) (Instructions, error)
    // Observe reports a tx's confirmation and whether it exactly matches Order.
    Observe(ctx, Order, txHash string) (Observation, error)
}
```

`Order` is derived only from the `market_sales` snapshot, never from client
input.

**Solana v1 transaction:**

1. `createAssociatedTokenAccountIdempotent` for treasury ATA (if missing).
2. `createAssociatedTokenAccountIdempotent` for seller ATA (if missing).
3. `transferChecked` buyer ATA → treasury ATA, `fee_base_units`.
4. `transferChecked` buyer ATA → seller ATA, `seller_base_units`.
5. Memo `max:sale:<sale_id>`.

**Observe** accepts the tx only if it is confirmed (finalized commitment, the
same rule as payments), the fee payer and transfer authority is
`buyer_address`, it moves exactly those two amounts of the snapshot mint to
the snapshot ATAs, it makes no other transfers of that mint from the buyer, and
it carries the memo. A finalized transaction that pays something toward the
sale but does not match exactly sends the sale to `manual_review` (a human
sorts out refunds). Any other mismatch is recorded as an event and treated
as not found until the deadline (§6).

### Price feed

`pricing.Feed` returns `{symbol: usd}` for ETH, SOL and POL. Values are
cached for `PRICE_FEED_TTL` (60s). If the cached value is older than
`PRICE_FEED_MAX_AGE` (5m) and a refresh fails, `Claim` returns
`PRICE_UNAVAILABLE`. It is never called when the gate is `0`.

### Registration gate

In `maxids.Claim`, before inserting: for each active linked wallet, read
balances of USDC, USDT (verified tokens on that wallet's chains) and the
native coin, on configured chains only. Sum the USD value. If below the
minimum, return `INSUFFICIENT_REGISTRATION_BALANCE` with
`{requiredUsd, countedUsd}`. Agent claims through `/v1/agents/:id/ids` use
the agent's own linked wallets. The result is recorded in `audit_logs`
(amount only, no addresses beyond what the account already owns).

Only chains with an RPC URL count, and testnets are skipped in production.
EVM balances come from a read-only reader (native balance and ERC-20
`balanceOf`); there is no EVM settlement. Reserved names claimed through X
or manual review are not gated.

## 6. Flows

### List

1. Check eligibility (§3), asset is in `MARKET_ASSETS`, payout address is a
   linked wallet of the seller in that chain's namespace.
2. Agent: `market:sell`; without `market.canSell` → `pending_approval`
   authorization; listing is created as `active` only after approval.
3. Insert `market_listings` row `active`.

### Buy now

1. Check market on, not paused, listing `active`, name still listable, the
   listing's payout address still an active wallet of the seller, buyer not
   banned, not in a buy cooldown for this listing, under the open-sale cap
   and the MAX ID limit, self-trade rules (§7), agent policy (§8).
2. `Quote` + `CheckFunds` for `fromAddress`.
3. In one DB tx: lock the max_id row, insert the sale snapshot, set the listing to
   `locked`. Status `pending_review` if price > `MARKET_REVIEW_THRESHOLD`,
   else `locked` with `lock_expires_at = now + MARKET_BUY_LOCK` (15m).
4. Return `{sale, quote, instructions}`.
   - `quote`: price, fee, sellerReceives, networkFee, totalAsset, totalNative.
5. Client signs and broadcasts, then `POST /sales/:id/submit {txHash}`.
6. Worker observes. On a match: complete (below).

For `pending_review`, the lock window starts when an admin approves; the
buyer then calls `GET /sales/:id` to receive instructions.

### Offer

1. Make: checks as buy (except listing), `MARKET_MIN_OFFER`, buyer not under
   `offer_suspension`, ≤ 20 live offers per account, one live offer per name.
   `CheckFunds` on `fromAddress` with a fresh quote. Insert `live`.
2. Funding worker (every 5m): re-run `CheckFunds` for `live`/`unfunded`
   offers; flip between them. Unfunded offers are hidden from the seller.
3. Accept: seller (or approved agent) accepts a `live` offer. The payout is
   the listing's payout address when the name is listed (re-checked as still
   an active wallet of the seller), otherwise `payoutAddress` from the body.
   `CheckFunds` runs again; if it fails, the offer flips to `unfunded` and
   accept fails. In one DB tx: offer → `accepted`, other live/unfunded offers
   on the name → `superseded`, listing (if any) → `locked`, sale snapshot
   inserted with `lock_expires_at = now + MARKET_OFFER_PAY_WINDOW` (24h).
4. Pay: buyer calls `POST /offers/:id/pay`, gets fresh quote and instructions
   (re-runs `CheckFunds`). This is where an agent's offer spend is checked
   (`pay` authorization). Then submit as buy-now.
5. Lock expires unpaid → sale `expired`, offer `lapsed`, strike recorded,
   listing back to `active`.

### Complete (one DB transaction)

1. Sale `submitted`/`confirming` → `completed`, `completed_at`.
2. The name's live listing → `sold`, whether the sale came from it or from
   an accepted offer; offer (if any) → `completed` (a lapsed offer paid late
   completes too and its strike is removed); all other live offers on the
   name → `superseded`.
3. `maxids.TransferOwnership`:
   - `owner_account_id = buyer`; `max_ids.type` follows the buyer's account
     type
   - delete `max_id_destinations`, then insert the buyer's paying wallet for
     its namespace, only if it is still an active wallet of the buyer
   - delete `settlement_preferences`
   - clear `display_name`, `description`, `avatar_url`
   - `resolution_version += 1`
   - `owner_changed_at = now()`, `listable_at = now() + MARKET_HOLD_PERIOD`,
     `rename_available_at = greatest(rename_available_at, listable_at)`
   - insert `max_id_ownership_events (transferred, sale_id)`
4. `audit_logs` entry.

If the transfer is refused (the seller no longer owns the name, or a rename
is pending) the paid sale goes to `manual_review` instead.

A payment to the name prepared before the sale completed (an in-flight
prepared transfer) still pays the old owner's resolved address; only
prepares after completion resolve to the buyer.

`max_id_history` and `name_quarantine` rows are untouched.

### Expiry and late payment

- Worker expires `locked` sales past `lock_expires_at` with no submitted tx.
  An unpaid offer sale lapses its offer with a strike; an unpaid buy-now
  sale starts a 1h cooldown before that buyer may buy the same listing again
  (`SALE_LIMIT`, `details.reason: buy_cooldown`).
- A sale with a submitted tx is kept until the tx is final or provably failed,
  even past the lock. A tx not found, or final but not a payment for the
  sale, expires the sale at `greatest(lock_expires_at, submitted_at) + 30m`,
  with no strike; an accepted offer of a sale that expired with a tx expires
  too, with no strike. A tx that failed on chain expires the sale at once.
- A tx submitted for an `expired`, unpaid sale is a late payment. If the
  seller still owns the name and no other sale is open on it, the sale moves
  to `confirming` and re-locks the name's active listing (even one relisted
  at the old price since), then completes on a match. If the name has moved
  on, the sale goes to `manual_review` for an admin refund from the treasury
  and a refund from the seller.
- A tx submitted for a sale an admin cancelled is taken only if the sale was
  ever locked (a sale denied in review refuses with `SALE_EXPIRED`). The sale
  stays `cancelled` with the tx recorded; the worker observes it and moves
  it to `manual_review` (event `tx_after_cancel`) only if the finalized tx
  paid toward the sale. A tx that fails, pays nothing toward the sale, or is
  not found by the deadline leaves it `cancelled`
  (`tx_after_cancel_unverified`).
- A sale left in `pending_review` for 24h is cancelled by the worker
  ("review timed out", no strike): the offer is declined and the listing
  returns to active.

### Public read model

- `GET /v1/resolve/:maxId` and `/v1/profiles/:maxId` add `ownerChangedAt`
  (only when within the last 30 days).
- `GET /v1/market/names/:name` returns listing, live offer count, last sale
  price and date, `listable` with reason.
- `GET /v1/market/listings` hides listings whose name is not active or whose
  owner is suspended or banned.

## 7. Anti-fraud

- `MARKET_ENABLED=false`: market routes return 404 `MARKET_DISABLED`, worker
  idle.
- `MARKET_TRADING_PAUSED=true`: reads work; new listings, price changes,
  offers, accepts and buys return `MARKET_PAUSED`. Delist, withdraw offer and
  decline offer still work (closing never starts a trade), and pay, submit
  and open sales still complete.
- **Listing-lock griefing:** a buyer has at most 2 open sales
  (`pending_review`, `locked`, `submitted`, `confirming`), checked under a
  per-buyer lock (`SALE_LIMIT`, `details.reason: open_sales`); a 1h buy
  cooldown follows an unpaid buy-now expiry; reviews time out after 24h.
- **Self-trade / wash-trade:** refuse if buyer and seller accounts are the
  same, share any wallet ever linked to both (`wallets` history, any status),
  the paying wallet was ever linked to the seller, or one is an agent
  administered by the other (or both are agents of the same administrator).
- Rate limits: list/offer/accept 30/min, buy 60/min (existing limiter).
- Holding period, registration gate, offer strikes as above.
- Review threshold sends large sales to `pending_review`.
- Exact-match observation (§5).
- Every state change writes `market_sale_events`; admin actions write
  `audit_logs`.

## 8. Agents

New scopes in `agents.AllScopes`:

| Scope | Allows |
|---|---|
| `market:read` | eligibility, own inbox, sale status |
| `market:sell` | list, update, delist, accept, decline |
| `market:buy` | buy, offer, withdraw, pay, submit |

Policy:

- **Buy / pay** spend is evaluated against `agent_policies.limits` for
  (chain, asset) like transfers (`maxPerTransfer`, `maxPer24h`). An offer
  spends nothing until it is paid, so making one is never held; paying an
  accepted offer is the `pay` action. Within limits → `auto_approved`;
  otherwise `pending_approval` until the administrator approves.
- **List / update price / accept** need `market.canSell = true` and a price
  ≥ the `market.minSalePrices` entry for that chain and asset; no entry →
  `pending_approval`.
- A held action keeps its full input; the administrator's approval replays
  it exactly, and a failed action never counts toward spend.
- Approvals use `POST /v1/agent-approvals/market/:authorizationId/approve|deny`
  and `GET /v1/agents/:agentId/market-authorizations?decision=`. In v1 they
  are API, SDK and MCP only; the web and app dashboards do not show them.

Human market routes are not session-only.

## 9. API

Public (`lookupLimit`; 404 when the market is disabled):

- `GET /v1/market/listings?q=&minPrice=&maxPrice=&sort=newest|price_asc|price_desc&cursor=`
- `GET /v1/market/names/:name`

Authenticated (session or agent key with scope):

| Method & path | Scope |
|---|---|
| `GET /v1/market/eligibility/:maxId` | market:read |
| `POST /v1/market/listings` `{maxId, price, chain, asset, payoutAddress, expiresAt?}` | market:sell |
| `PATCH /v1/market/listings/:id` `{price}` | market:sell |
| `DELETE /v1/market/listings/:id` | market:sell |
| `POST /v1/market/listings/:id/buy` `{fromAddress, idempotencyKey}` | market:buy |
| `POST /v1/market/offers` `{name, amount, chain, asset, fromAddress, expiresAt?}` | market:buy |
| `DELETE /v1/market/offers/:id` | market:buy |
| `POST /v1/market/offers/:id/accept` `{payoutAddress?}` (required when the name has no listing) | market:sell |
| `POST /v1/market/offers/:id/decline` `{note?}` | market:sell |
| `POST /v1/market/offers/:id/pay` `{idempotencyKey?}` | market:buy |
| `POST /v1/market/sales/:id/submit` `{txHash}` | market:buy |
| `GET /v1/market/sales/:id` | market:read |
| `GET /v1/market/me` | market:read |

Agent approvals (session, administrator; API/SDK/MCP only in v1):

- `GET /v1/agents/:agentId/market-authorizations?decision=`
- `POST /v1/agent-approvals/market/:authorizationId/approve`
- `POST /v1/agent-approvals/market/:authorizationId/deny`

Admin (`/v1/admin/market`, `ADMIN_ACCOUNT_IDS`; every route answers 404 to
anyone else):

- `GET listings`, `GET offers`, `GET sales` (filters: status, account, name)
- `POST listings/:id/delist {reason}`
- `POST sales/:id/approve {reason?}`: `pending_review` → `locked`; the lock
  window starts now.
- `POST sales/:id/deny {reason}`: `pending_review` → `cancelled`; the offer is
  declined ("denied by admin: …", no strike) and the listing returns to
  active.
- `POST sales/:id/cancel {reason}`: from `locked` with no tx, or
  `manual_review`; the listing returns to active and an accepted offer
  expires with no strike. Cancelling a `manual_review` offer sale whose offer
  had lapsed removes that offer's strike. Refunds are off-platform. A tx
  submitted afterwards is handled as in §6.
- `POST sales/:id/reverse {reason}`: ownership back to the seller via
  `TransferOwnership`; money is handled manually.
- `GET restrictions?accountId=&all=`, `POST restrictions {accountId, kind,
  until?, reason}`, `DELETE restrictions/:id`

### Errors

`MARKET_DISABLED`, `MARKET_PAUSED`, `NOT_LISTABLE` (with reason code),
`LISTING_NOT_ACTIVE`, `NAME_LOCKED`, `SELF_TRADE`, `MARKET_RESTRICTED`,
`ASSET_NOT_ACCEPTED`, `INSUFFICIENT_FUNDS` (with `{needAsset, haveAsset,
needNative, haveNative}`), `OFFER_LIMIT`, `OFFER_BELOW_MINIMUM`,
`OFFER_UNFUNDED`, `SALE_EXPIRED`, `TX_MISMATCH`, `PENDING_APPROVAL`,
`INSUFFICIENT_REGISTRATION_BALANCE`, `PRICE_UNAVAILABLE`, `NAME_LISTED`
(rename of a listed name), `OWNER_CHANGED` and `RENAME_PENDING` (reverse or
transfer refused), `SALE_LIMIT` (409; `details.reason` is `open_sales` or
`buy_cooldown`), and `MAX_ID_LIMIT_REACHED` at purchase.

## 10. Configuration

| Variable | Default | Meaning |
|---|---|---|
| `MARKET_ENABLED` | `false` | master switch |
| `MARKET_TRADING_PAUSED` | `false` | read-only mode |
| `MARKET_ASSETS` | — | `chain:asset` list, e.g. `solana-devnet:4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU`; verified tokens only |
| `MARKET_FEE_BPS` | `1000` | fee from seller proceeds, `0..5000` |
| `MARKET_HOLD_PERIOD` | `72h` | after claim / rename / purchase; must be positive |
| `MARKET_BUY_LOCK` | `15m` | buy-now window |
| `MARKET_OFFER_PAY_WINDOW` | `24h` | after accept |
| `MARKET_REVIEW_THRESHOLD` | — (off) | decimal amount; sales above need admin review |
| `MARKET_MIN_OFFER` | `1` | decimal amount; also the minimum listing price. With a fee it must be ≥ `ceil(10000 / MARKET_FEE_BPS)` base units of every asset, so the fee is never zero |
| `TREASURY_EVM`, `TREASURY_SOLANA` | fall back to `RENAME_TREASURY_*` | fee recipients |
| `REGISTRATION_MIN_BALANCE_USD` | `100` | `0` disables the gate |
| `PRICE_FEED_URL` | CoinGecko simple price | |
| `PRICE_FEED_TTL` / `PRICE_FEED_MAX_AGE` | `60s` / `5m` | |

Startup fails if `MARKET_ENABLED=true` and an asset in `MARKET_ASSETS` has no
treasury for its namespace or no RPC configured, and whenever the fee, hold
period or minimum offer break the rules above.

## 11. UI surfaces

### Web (`max-frontend`)

- `/market`: search, price filter, sort, listing cards (handle, price, listed
  ago; no last sale). Read-only without a wallet.
- `/[handle]`: market panel. When listed: price, **Buy now**, **Make offer**.
  When not listed: **Make offer**. Banner "Owner changed on <date>" for 30 days.
- Buy/pay modal: quote breakdown, funds check, lock countdown, sign with the
  connected Solana wallet, submit, poll to completion. After an uncertain
  wallet error it looks for the payment and offers a new signature only once
  the first attempt's blockhash has expired.
- `/dashboard` → Market: my listings (create with "you receive 90%",
  eligibility reason, edit, delist), offers received (accept/decline), offers
  made (withdraw, pay, unfunded warning), sales history.
- `/dashboard/admin/market` (admins): review queue, listings, delist,
  restrictions, reverse.
- Claim page: registration-gate message with counted vs. required USD.

### App (`max-app`)

- New **Market** tab in `(tabs)`: search and listings.
- `market/[name].tsx`: detail, Buy now, Make offer.
- **Sell** action on ID cards → `sell.tsx`.
- `market/offers.tsx`: inbox (received / made).
- Payment through the sale variant of the review sheet,
  `components/market/SaleReview.tsx`, with `services/saleTx.ts` (instructions
  and pure helpers) and `services/chain.ts` signing the multi-instruction
  Solana transaction. Signed bytes and their blockhash are kept: a retry
  re-sends the same bytes, and a new signature waits for the first to expire.
- Claim screen shows the registration-gate message.

Client configuration: `NEXT_PUBLIC_MARKET_ASSETS` / `EXPO_PUBLIC_MARKET_ASSETS`
(`chain:mint:decimals`, mirroring `MARKET_ASSETS`; the decimals pin how a
shown price becomes base units, cross-checked on chain);
`NEXT_PUBLIC_RPC_SOLANA` / `NEXT_PUBLIC_RPC_SOLANA_DEVNET` (and the app's
`EXPO_PUBLIC_RPC_*`); optional `NEXT_PUBLIC_MARKET_TREASURY` /
`EXPO_PUBLIC_MARKET_TREASURY`, which pin the treasury passed to
`assertSaleInstructionsMatch` as `expect.treasury`.

## 12. SDK and MCP

### SDK (`max-sdk`)

`client.market`:

- `search`, `getName`, `eligibility`
- `list`, `updateListing`, `delist`
- `buy`, `offer`, `withdrawOffer`, `acceptOffer(offerId, {payoutAddress?})`,
  `declineOffer`, `payOffer(offerId, {idempotencyKey?})`
- `submit`, `getSale`, `inbox`
- `client.agents.marketAuthorizations(agentId, {decision?})`,
  `approveMarket(authorizationId)`, `denyMarket(authorizationId)`
- `assertSaleInstructionsMatch(sale, instructions, expect?)`: throws unless the
  instructions pay exactly the snapshot fee and seller amounts; `expect:
  SaleExpectations` pins `buyer`, `asset`, `priceBaseUnits` and `treasury`
  you trust independently (dependency-free, in the main entry).
- `buildSolanaSaleTransaction(instructions, payer, recentBlockhash)` →
  `VersionedTransaction` (unverified), and
  `buildVerifiedSolanaSaleTransaction(view, payer, recentBlockhash, expect?)`,
  which asserts first. Both are exported from the `@max/sdk/solana` subpath
  with `@solana/web3.js` and `@solana/spl-token` as optional peer
  dependencies, so the main SDK stays dependency-free.

Plus types, new error codes, `ownerChangedAt` on resolve/profile types, and a
README section.

### MCP (`max-mcp`)

`market_search`, `market_get_listing`, `market_list_name`, `market_delist`,
`market_make_offer`, `market_accept_offer`, `market_buy`, `market_get_sale`.
Tools that may need approval (`market_list_name`, `market_accept_offer`,
`market_buy`) return `{status: "pending_approval", authorizationId}`;
`market_make_offer` is never held.

## 13. Testing

- Unit: fee maths (rounding, `fee + seller = price`), eligibility matrix,
  state machine transitions, self-trade detection, offer funding flips,
  strikes, price-feed fail-closed, registration gate at 0 / below / above.
  Fakes, like `renames/service_test.go`.
- Solana adapter: `Observe` against forged txs (wrong sender, wrong split,
  wrong mint, missing memo, extra transfer, unconfirmed).
- Integration against a local Postgres (never Railway's `railway` database):
  buy-now end to end, offer → accept → pay, lock expiry, late valid tx,
  review threshold, agent within/over limits, admin reverse, migration
  up/down.
- Manual: devnet run with devnet USDC on web and app.

## 14. Future work

- Auctions with escrow (v1.1).
- EVM settlement via a stateless splitter contract; BTC/LTC multi-output txs;
  Tron splitter.
- Notifications.
- Counter-offers.
