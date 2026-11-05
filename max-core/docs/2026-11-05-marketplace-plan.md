# MAX Marketplace Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add fixed-price and offer trading of MAX IDs, settled by one atomic
Solana USDC transaction with a 10% treasury fee, and a USD balance gate on
claims.

**Architecture:** A new `internal/market` package (listings, offers, sales,
worker, admin) sits on top of `maxids` and `agents`. Chain-specific work goes
through a `settle.Adapter` (Solana only in v1). Ownership changes happen only
in `maxids.TransferOwnership`, inside the sale-completion DB transaction. A new
`internal/pricing` feed serves the registration gate.

**Tech Stack:** Go 1.x, Gin, pgx v5, sqlc, golang-migrate, Postgres;
TypeScript (`@max/sdk`, `max-mcp`); Next.js (`max-frontend`); Expo
(`max-app`); `@solana/web3.js` + `@solana/spl-token` in the clients.

**Spec:** `max-core/docs/2026-11-05-marketplace-design.md`. Read it first;
section numbers below (§n) refer to it.

## Global Constraints

- Settlement v1: Solana + USDC only, from `MARKET_ASSETS`; verified tokens only.
- Fee: `MARKET_FEE_BPS=1000`; `fee = price * fee_bps / 10000` rounded down; `seller = price - fee`.
- Minimum listing price and offer: `MARKET_MIN_OFFER` (default `1` USDC).
- Windows: `MARKET_HOLD_PERIOD=72h`, `MARKET_BUY_LOCK=15m`, `MARKET_OFFER_PAY_WINDOW=24h`, offer expiry default 7d, max 30d.
- Limits: one live offer per (buyer, name); 20 live offers per account; 3 strikes in 30 days → 30-day `offer_suspension`.
- Registration gate: `REGISTRATION_MIN_BALANCE_USD=100`; `0` disables and never calls the price feed; stale or failed feed fails closed with `PRICE_UNAVAILABLE`.
- `MARKET_ENABLED` defaults to `false`; when false, market routes 404 and the worker is idle.
- MAX never signs or holds user funds. No hot wallet.
- Every market and admin state change writes `market_sale_events` or `audit_logs`.
- Integration tests use `TEST_DATABASE_URL` pointing at a **local** Postgres. Never the Railway server's `railway` database; MAX dev uses the `max` database only.
- Never print `.env` secrets.
- Commit only when the user has said to. Each "Checkpoint" step means: tests green, `make lint` clean, commit if authorised. Never stash, reset or switch branches; other sessions may be editing.
- Error codes: exactly the names in §9 "Errors".

## Review Focus

1. **Two buyers press Buy now at once.** Exactly one sale is created; the other gets `NAME_LOCKED`. Test in Task 7.
2. **The same tx hash is submitted for two sales.** The second is refused (`TX_MISMATCH`, via the unique `(chain, tx_hash)` index), and only the matching sale can complete. Test in Task 7.
3. **The seller or treasury has no USDC token account yet.** The quote includes ATA rent, and the instructions include `createAssociatedTokenAccountIdempotent`. Test in Task 3.
4. **A sender prepared a transfer to max.alice just before she sold it.** Submitting that transfer after completion is rejected because `resolution_version` was bumped. Test in Task 5.
5. **The price feed returns 0, a negative value, NaN, or is missing a symbol.** This is treated as unavailable, and the claim fails closed. Test in Task 2.

---

## File Structure

```
max-core/
  db/migrations/000009_marketplace.{up,down}.sql   schema §4
  db/queries/market.sql                            sqlc queries for market
  db/queries/maxids.sql                            + TransferOwnership queries
  internal/config/config.go                        + MarketConfig, RegistrationConfig, PricingConfig
  internal/pricing/{feed.go,coingecko.go,feed_test.go}
  internal/solanarpc/client.go                     + Balance, TokenAccountBalance, LatestBlockhash, MinimumBalanceForRentExemption
  internal/market/settle/{adapter.go,solana.go,solana_test.go}
  internal/maxids/{gate.go,gate_test.go,transfer.go}   registration gate, TransferOwnership
  internal/market/
    domain.go      types, statuses, errors
    fees.go        SplitFee
    eligibility.go Listable(...) pure rules
    selftrade.go   SelfTrade(...) pure rules
    repository.go  pgx/sqlc, all locking txs
    service.go     listings, buy, submit, reads
    offers.go      offers, accept, pay, strikes
    worker.go      observe, expire, funding re-check
    admin.go       admin service methods
    agents.go      agent policy hooks
    handler.go, dto.go
    *_test.go
  internal/agents/{domain.go,policy.go}            + market scopes, MarketPolicy
  internal/renames/repository.go                   refuse rename while listed
  internal/resolver/*                              + ownerChangedAt
  internal/server/router.go                        routes §9
  internal/server/market_integration_test.go
  cmd/api/main.go                                  wiring, worker start
  .env.example, README.md
max-sdk/src/resources/market.ts, src/solana.ts, src/types.ts, src/client.ts, test/unit.test.ts, README.md
max-mcp/src/server.ts, test/server.test.ts
max-frontend/src/app/market/page.tsx, src/components/market/*, src/app/[handle]/ProfileView.tsx, src/app/dashboard/*
max-app/src/app/(app)/(tabs)/market.tsx, market/[name].tsx, market/offers.tsx, sell.tsx, src/services/chain.ts, src/hooks/queries.ts
```

---

### Task 1: Schema, queries and config

**Files:**
- Create: `max-core/db/migrations/000009_marketplace.up.sql`, `000009_marketplace.down.sql`, `max-core/db/queries/market.sql`
- Modify: `max-core/internal/config/config.go`, `max-core/.env.example`
- Test: `max-core/internal/config/config_test.go` (create if absent)

**Interfaces:**
- Produces:
  - `config.MarketConfig{Enabled, TradingPaused bool; Assets string; FeeBPS int; HoldPeriod, BuyLock, OfferPayWindow time.Duration; ReviewThreshold, MinOffer string; TreasuryEVM, TreasurySolana string}`
  - `config.RegistrationConfig{MinBalanceUSD float64}`
  - `config.PricingConfig{URL string; TTL, MaxAge time.Duration}`
  - Fields `Market`, `Registration`, `Pricing` on `config.Config`.
  - Generated sqlc types in `db/generated` for every table in §4.

- [ ] **Step 1: Write failing config test.** `TestLoad_MarketDefaults` sets no market env vars. Assert:
  - `Enabled == false`, `FeeBPS == 1000`, `HoldPeriod == 72*time.Hour`, `BuyLock == 15*time.Minute`, `OfferPayWindow == 24*time.Hour`, `MinOffer == "1"`;
  - `Registration.MinBalanceUSD == 100`;
  - `Pricing.TTL == 60*time.Second`, `Pricing.MaxAge == 5*time.Minute`.

  `TestLoad_TreasuryFallback` sets `RENAME_TREASURY_SOLANA=X` and no `TREASURY_SOLANA`, and asserts `Market.TreasurySolana == "X"`.
- [ ] **Step 2:** Run `cd max-core && go test ./internal/config/ -run TestLoad_ -v`. Expect FAIL (fields undefined).
- [ ] **Step 3: Implement the config.** Add the structs and the env parsing in the file's existing style. `FeeBPS` must be within 0–5000, otherwise `Load` returns an error.
- [ ] **Step 4: Write the migration** exactly per §4: tables, CHECKs, partial unique indexes, the `market_sales_snapshot_immutable` trigger (modelled on `payments_snapshot_immutable` in 000002), and the `max_ids`, `max_id_ownership_events` and `agent_policies` alterations. For the `('market','blocked')` insert, first run:

  ```sql
  DO $$ BEGIN IF EXISTS (SELECT 1 FROM max_ids WHERE canonical_name = 'market')
  THEN RAISE EXCEPTION 'a MAX ID named market exists; resolve before migrating'; END IF; END $$;
  ```

  The down migration drops everything in reverse order, deletes only the `market` reserved row, and drops the added columns.
- [ ] **Step 5: Write `db/queries/market.sql`.** Write a query for each repository method in Task 7 (named there), then run `make sqlc`. Expect generation to succeed with no errors.
- [ ] **Step 6: Verify the migration on a local DB.** Run `make migrate && make migrate-down && make migrate` with `DATABASE_URL` pointing at local Postgres (`make up`). Expect all three to succeed.
- [ ] **Step 7:** Run the config tests. Expect PASS. Add the new vars to `.env.example`, with comments and no secrets.
- [ ] **Step 8: Checkpoint.**

### Task 2: Price feed

**Files:**
- Create: `max-core/internal/pricing/feed.go`, `coingecko.go`, `feed_test.go`

**Interfaces:**
- Produces:
  - `type Feed interface { USD(ctx context.Context, symbols []string) (map[string]float64, error) }`
  - `func NewCached(src Source, ttl, maxAge time.Duration, now func() time.Time) Feed`
  - `type Source interface { Fetch(ctx, symbols []string) (map[string]float64, error) }`
  - `func NewCoinGecko(url string, hc *http.Client) Source`
  - `var ErrUnavailable = errors.New("price unavailable")`

- [ ] **Step 1: Write failing tests:**
  - `TestCached_ServesWithinTTL`: the source is called once for two calls 30s apart.
  - `TestCached_StaleAfterMaxAgeFailsClosed`: the source errors, and cached data is 6m old, so the call returns `ErrUnavailable`.
  - `TestCached_UsesStaleWithinMaxAgeOnError`: the data is 2m old and the source errors, so the call returns the cached value.
  - `TestCached_RejectsBadValues`: the source returns `{"SOL":0}`, `{"SOL":-1}`, `{"SOL":NaN}` or is missing `"ETH"`, and each gives `ErrUnavailable`.
  - `TestCoinGecko_ParsesSimplePrice` uses an `httptest` server returning `{"solana":{"usd":150.5},"ethereum":{"usd":3000},"polygon-ecosystem-token":{"usd":0.4}}` and maps it to `SOL`, `ETH`, `POL`.
- [ ] **Step 2:** Run `go test ./internal/pricing/ -v`. Expect FAIL.
- [ ] **Step 3: Implement.** Symbol→id map: `SOL→solana`, `ETH→ethereum`, `POL→polygon-ecosystem-token`. The cache is guarded by a mutex.
- [ ] **Step 4:** Run the tests. Expect PASS.
- [ ] **Step 5: Checkpoint.**

### Task 3: Solana RPC additions and the settlement adapter

**Files:**
- Modify: `max-core/internal/solanarpc/client.go`
- Create: `max-core/internal/market/settle/adapter.go`, `solana.go`, `solana_test.go`

**Interfaces:**
- Consumes: `solanarpc.Client.Transaction`, `ConfirmationStatus`, `AccountInfo`; `chains.Chain`; `chains.LookupVerifiedToken`.
- Produces (in `settle`):
  - `type Order struct { SaleID uuid.UUID; Chain chains.Chain; Mint string; Decimals int; Buyer, Seller, Treasury string; Price, Fee, SellerAmount *big.Int }`
  - `type Quote struct { NetworkFee *big.Int /*lamports*/; CreateSellerATA, CreateTreasuryATA bool; RentPerATA *big.Int }`
  - `type FundsShortfall struct { NeedAsset, HaveAsset, NeedNative, HaveNative *big.Int }`, which implements `error`
  - `type Instructions struct { Solana []SolanaIx }`, where `SolanaIx{Kind string /*create_ata_idempotent|transfer_checked|memo*/; Accounts map[string]string; Amount string; Decimals int; Memo string}`
  - `type Observation struct { Status string /*not_found|pending|confirmed|failed*/; Matches bool; Reason string; Confirmations int64 }`
  - `type Adapter interface { Quote(ctx, Order) (Quote, error); CheckFunds(ctx, Order, Quote) error; BuildTx(Order, Quote) (Instructions, error); Observe(ctx, Order, txHash string) (Observation, error); Balances(ctx context.Context, chain chains.Chain, owner string) (map[string]*big.Int /*"native" or mint*/, error) }`
  - `func NewSolana(clients map[string]SolanaRPC) Adapter`, where `SolanaRPC` is the narrow interface of the client methods used.
  - New `solanarpc` methods: `Balance(ctx, addr) (uint64, error)`, `TokenAccountsByOwner(ctx, owner, mint string) (uint64 /*total base units*/, error)`, `MinimumBalanceForRentExemption(ctx, size int) (uint64, error)`.
- Memo program id: `MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr`. Memo text: `max:sale:<uuid>`.

- [ ] **Step 1: Write failing tests** with a fake `SolanaRPC`:
  - `TestQuote_IncludesRentForMissingATAs`: the seller ATA is missing and the treasury ATA exists, so `CreateSellerATA` is true, `CreateTreasuryATA` is false, and `NetworkFee` = 5000 base fee + 1 × rent(165).
  - `TestCheckFunds_ShortAsset` and `TestCheckFunds_ShortNative` both return `*FundsShortfall` with correct need/have values.
  - `TestBuildTx_Order`: the instruction kinds are, in order, `[create_ata_idempotent (seller), transfer_checked (treasury, fee), transfer_checked (seller, sellerAmount), memo]`, and the treasury create appears only when needed.
  - `TestObserve_*`, one per case: `Exact` → Matches. `WrongSender`, `WrongSplit`, `WrongMint`, `MissingMemo`, `ExtraTransferFromBuyer`, `TxFailed`, `NotFinalized` → not Matches or not confirmed, each with a distinct `Reason`.
- [ ] **Step 2:** Run `go test ./internal/market/settle/ -v`. Expect FAIL.
- [ ] **Step 3: Add the RPC methods.** They wrap `getBalance`, `getTokenAccountsByOwner` (jsonParsed, summing `tokenAmount.amount`) and `getMinimumBalanceForRentExemption`.
- [ ] **Step 4: Implement the adapter.**
  - Derive ATAs with the PDA helper in `chains/solana_pda.go`.
  - `Observe` reuses `Transaction.AllInstructions()`. It reports confirmed only at `finalized`, the same rule as `payments.observeSolana`.
- [ ] **Step 5:** Run the tests. Expect PASS.
- [ ] **Step 6: Checkpoint.**

### Task 4: Registration gate

**Files:**
- Create: `max-core/internal/maxids/gate.go`, `gate_test.go`
- Modify: `max-core/internal/maxids/service.go` (`Claim`, `NewService`), `internal/maxids/error.go`, `internal/maxids/interfaces.go` (Repository: `ListActiveWallets(ctx, owner) ([]WalletRef, error)`), `db/queries/maxids.sql`, `cmd/api/main.go`, `internal/agents/service.go` (agent ClaimID goes through the same gate)

**Interfaces:**
- Consumes: `pricing.Feed`; `settle.Adapter.Balances`; `chains.LookupVerifiedToken`.
- Produces:
  - `type BalanceGate interface { Check(ctx context.Context, owner uuid.UUID) error }`
  - `func NewBalanceGate(minUSD float64, wallets WalletLister, balances map[chains.Namespace]BalanceReader, feed pricing.Feed, cfgChains []chains.Chain) BalanceGate`
  - `func NewService(repo Repository, gate BalanceGate) Service`
  - Errors: `ErrInsufficientRegistrationBalance` (code `INSUFFICIENT_REGISTRATION_BALANCE`, details `{requiredUsd, countedUsd}`) and `ErrPriceUnavailable` (`PRICE_UNAVAILABLE`).
- Counting rule: USDC and USDT (verified tokens on configured chains in the wallet's namespace) at $1 × amount / 10^decimals. Native ETH, SOL and POL use the feed. Nothing else counts.

- [ ] **Step 1: Write failing tests:**
  - `TestGate_DisabledAtZero`: the feed fake panics if called, and `Check` returns nil.
  - `TestGate_BelowMinimum`: 60 USDC plus 0.2 SOL at $150 ($90) gives `ErrInsufficientRegistrationBalance` with `countedUsd == 90`.
  - `TestGate_AtOrAboveMinimum`: 70 USDC plus 0.2 SOL ($100) gives nil.
  - `TestGate_IgnoresBONK`: 1e12 BONK and nothing else is insufficient.
  - `TestGate_FeedDown`: there are native balances and the feed errors, so the result is `ErrPriceUnavailable`.
  - `TestGate_StablesOnlySkipsFeed`: 150 USDC, and the feed errors, gives nil. The feed isn't needed when stables alone suffice, so stables are summed first and the feed is called only if they fall short.
  - `TestClaim_GateRunsBeforeCreate` is in `service_test.go`, with a fake repo asserting `Create` is not called on gate failure.
- [ ] **Step 2:** Run `go test ./internal/maxids/ -v`. Expect FAIL.
- [ ] **Step 3: Implement the gate and wire it into `Claim`,** after name validation and before `Create`. Record an `audit_logs` row with action `registration_gate` and metadata `{countedUsd, requiredUsd, passed}`.
- [ ] **Step 4:** Run the tests plus `go test ./...`. Expect PASS, after updating existing tests to pass a nil-safe disabled gate.
- [ ] **Step 5: Checkpoint.**

### Task 5: Ownership transfer, edit locks and `ownerChangedAt`

**Files:**
- Create: `max-core/internal/maxids/transfer.go`
- Modify: `db/queries/maxids.sql`, `db/queries/resolver.sql`, `internal/maxids/service.go` (destination/profile/settlement edits refuse while an open sale exists), `internal/resolver/*` (DTO `ownerChangedAt`, set only within 30 days), `internal/renames/repository.go` (`Reserve` refuses with `maxids.ErrNameListed` when the ID has an `active`/`locked` listing), `internal/maxids/error.go`
- Test: `internal/server/market_integration_test.go` (created here, `//go:build integration`)

**Interfaces:**
- Produces:
  - `type TransferParams struct { MaxID, From, To uuid.UUID; SaleID *uuid.UUID; DestinationWalletID uuid.UUID; HoldPeriod time.Duration; Reason string }`
  - `func TransferOwnership(ctx context.Context, q *generated.Queries, p TransferParams) error`. It runs on a caller-supplied transaction, the caller must hold the row lock, and it performs every bullet of §6 "Complete" step 3 in order.
  - Errors: `maxids.ErrNameListed` (`NAME_LISTED`) and `maxids.ErrNameLocked` (`NAME_LOCKED`).

- [ ] **Step 1: Write failing integration tests:**
  - `TestTransferOwnership_ResetsIdentity`: claim, set the profile, two destinations and settlement, then transfer. Assert the new owner, exactly one destination (the buyer wallet's namespace), no settlement row, an empty profile, `resolution_version` +1, `owner_changed_at` set, `listable_at ≈ now+72h`, `rename_available_at >= listable_at`, an ownership event `transferred` with the sale id, and an unchanged `max_id_history` count.
  - `TestTransferOwnership_StalePaymentRejected` (Review Focus 4): prepare a transfer to the ID, transfer ownership, then submit the prepared transfer. Expect the existing stale-resolution error code.
  - `TestResolve_OwnerChangedAt`: present after a transfer, and absent when `owner_changed_at` is 31 days ago.
  - `TestRename_RefusedWhileListed`: insert an active listing row, and the rename request returns `NAME_LISTED`.
- [ ] **Step 2:** Run `cd max-core && go test -tags integration ./internal/server/ -run 'TestTransferOwnership|TestResolve_OwnerChangedAt|TestRename_RefusedWhileListed' -v`. Expect FAIL.
- [ ] **Step 3: Implement** the queries, `TransferOwnership`, the resolver field, and the edit and rename guards.
- [ ] **Step 4:** Run the same command. Expect PASS. Then run `go test ./...` for unit tests. Expect PASS.
- [ ] **Step 5: Checkpoint.**

### Task 6: Market domain rules (pure)

**Files:**
- Create: `max-core/internal/market/domain.go`, `fees.go`, `eligibility.go`, `selftrade.go`, `rules_test.go`

**Interfaces:**
- Produces:
  - Status constants for listings, offers and sales, exactly as §4.
  - `func SplitFee(price *big.Int, feeBPS int) (fee, seller *big.Int)`
  - `type NameFacts struct { Status, OwnerStatus string; HasLiveReservedClaim, RenamePending bool; ListableAt, RenameAvailableAt time.Time; OwnerBanned bool }`
  - `func Listable(f NameFacts, now time.Time) (ok bool, reason string)`. Reason codes: `not_active`, `owner_suspended`, `reserved_claim`, `holding_period`, `rename_cooldown`, `rename_pending`, `market_banned`.
  - `type PartyFacts struct { BuyerAccount, SellerAccount uuid.UUID; SharedWallet, PayerWalletEverSellers bool; BuyerAdmin, SellerAdmin *uuid.UUID /*administrator if agent*/ }`
  - `func SelfTrade(p PartyFacts) bool`
  - All errors from §9 as `shared` error values.

- [ ] **Step 1: Write failing table tests:**
  - `TestSplitFee`: (1_000_000, 1000) → (100_000, 900_000); (999_999, 1000) → (99_999, 900_000); (1, 1000) → (0, 1). Fee + seller == price in every row.
  - `TestListable`: one row per reason code, plus the happy path.
  - `TestSelfTrade`: same account, shared wallet, payer wallet previously the seller's, agent↔its admin, two agents with the same admin → true; unrelated → false.
- [ ] **Step 2:** Run `go test ./internal/market/ -run 'TestSplitFee|TestListable|TestSelfTrade' -v`. Expect FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4:** Run the tests. Expect PASS.
- [ ] **Step 5: Checkpoint.**

### Task 7: Repository, listings, buy now and submit

**Files:**
- Create: `max-core/internal/market/repository.go`, `service.go`, `handler.go`, `dto.go`, `service_test.go`
- Modify: `db/queries/market.sql`, `internal/server/router.go`, `cmd/api/main.go`
- Test: `internal/market/service_test.go` (fakes), `internal/server/market_integration_test.go`

**Interfaces:**
- Consumes: Task 3 `settle.Adapter`; Task 5 `maxids.TransferOwnership`; Task 6 rules; `config.MarketConfig`.
- Produces:
  - `type Repository interface` with:
    - `NameFacts(ctx, maxID) (NameFacts, uuid.UUID /*owner*/, error)`
    - `PartyFacts(ctx, buyer, seller uuid.UUID, payer string) (PartyFacts, error)`
    - `CreateListing(ctx, NewListing) (Listing, error)`
    - `UpdateListingPrice(ctx, id, seller, price) (Listing, error)`
    - `CancelListing(ctx, id, seller) error`
    - `OpenSale(ctx, NewSale) (Sale, error)`, which locks the max_id row and the listing, refuses if an open sale exists (`ErrNameLocked`), and inserts the sale and its event
    - `AttachTx(ctx, saleID, buyer, txHash) (Sale, error)`, where a unique violation maps to `ErrTxMismatch`
    - `CompleteSale(ctx, saleID uuid.UUID, destWalletID uuid.UUID, hold time.Duration) (Sale, error)`, which in one tx does §6 Complete steps 1–4
    - `ExpireSale(ctx, saleID, reason) (Sale, error)`
    - `GetSale(ctx, id) (Sale, error)`
    - `SearchListings(ctx, ListingQuery) ([]ListingCard, string /*cursor*/, error)`
    - `NameView(ctx, name) (NameView, error)`
    - `Inbox(ctx, account) (Inbox, error)`
  - `type Service struct`, built by `func NewService(repo Repository, adapters map[chains.Namespace]settle.Adapter, cfg Config, agents AgentPolicy, log *zap.Logger) *Service`, with methods:
    - `Eligibility`
    - `List(ctx, p shared.Principal, in ListInput) (Listing, error)`
    - `UpdatePrice`, `Delist`
    - `Buy(ctx, p, listingID uuid.UUID, in BuyInput{FromAddress, IdempotencyKey string}) (SaleView, error)`
    - `Submit(ctx, p, saleID uuid.UUID, txHash string) (SaleView, error)`
    - `GetSale`, `Search`, `NameView`, `Inbox`
  - `type SaleView struct { Sale Sale; Quote *QuoteView; Instructions *settle.Instructions }`. `QuoteView` has `price, fee, sellerReceives, networkFee, totalAsset, totalNative` as decimal strings.
  - `type AgentPolicy interface { Authorize(ctx, p shared.Principal, action string, amount *big.Int, chain, asset string, ref AuthRef) (Decision, error) }`. Task 10 implements it; until then, `AllowAll` is used for humans.
  - Routes from §9 (public, authenticated and submit) under `marketGate` middleware, which returns 404 when `!Enabled` and `MARKET_PAUSED` for writes when `TradingPaused`.

- [ ] **Step 1: Write failing unit tests** with a fake repo and adapter:
  - `TestList_RejectsNotListable`: each reason maps to `NOT_LISTABLE`.
  - `TestList_RejectsPayoutNotOwned`.
  - `TestList_RejectsBelowMinimum`.
  - `TestBuy_SelfTrade` returns `SELF_TRADE`.
  - `TestBuy_InsufficientFunds` passes through `FundsShortfall` details.
  - `TestBuy_ReviewThreshold` gives `pending_review` with no instructions.
  - `TestBuy_Paused` returns `MARKET_PAUSED`.
  - `TestBuy_IdempotentReplay`: the same key returns the same sale.
  - `TestSubmit_WrongBuyer` returns 404.
- [ ] **Step 2:** Run `go test ./internal/market/ -v`. Expect FAIL.
- [ ] **Step 3: Write failing integration tests:**
  - `TestMarket_BuyNowHappyPath` uses a fake Solana adapter registered for `solana-devnet` that returns a matching observation. Flow: list, buy, submit, `worker.Tick`. Assert the sale is `completed`, the listing `sold`, the new owner, and the destination is the buyer's wallet.
  - `TestMarket_ConcurrentBuyOneWins` (Review Focus 1): 2 goroutines call buy. Exactly one 200 and one `NAME_LOCKED`.
  - `TestMarket_TxHashReuseRejected` (Review Focus 2).
  - `TestMarket_DisabledReturns404`.
  - Use the existing `newTestServer` helpers. The worker tick comes from Task 9, so mark these `t.Skip("needs Task 9")` until Task 9 lands, except the concurrency and disabled tests.
- [ ] **Step 4: Implement** the repository, service, handlers and routes.
- [ ] **Step 5:** Run the unit tests plus `go test -tags integration ./internal/server/ -run TestMarket_ -v`. Expect PASS, except the tests skipped for Task 9.
- [ ] **Step 6: Checkpoint.**

### Task 8: Offers, funding re-checks and strikes

**Files:**
- Create: `max-core/internal/market/offers.go`, `offers_test.go`
- Modify: `repository.go`, `handler.go`, `db/queries/market.sql`, `router.go`

**Interfaces:**
- Produces:
  - Service methods:
    - `MakeOffer(ctx, p, in OfferInput{Name, Amount, Chain, Asset, FromAddress string; ExpiresAt *time.Time}) (Offer, error)`
    - `WithdrawOffer(ctx, p, id) error`
    - `AcceptOffer(ctx, p, id) (SaleView, error)`
    - `DeclineOffer(ctx, p, id, note string) error`
    - `PayOffer(ctx, p, id, idemKey string) (SaleView, error)`
    - `RecheckFunding(ctx, limit int) (int, error)`
  - Repository methods:
    - `CreateOffer`
    - `AcceptOffer(ctx, offerID, seller, NewSale) (Sale, error)`: one tx that does accept, supersedes the others, locks the listing and opens the sale
    - `SetOfferFunded(ctx, id, funded bool, checkedAt) error`
    - `DueFundingChecks(ctx, olderThan time.Time, limit int) ([]Offer, error)`
    - `AddStrike(ctx, account, offerID) (strikesIn30d int, error)`
    - `Restrict(ctx, account, kind, until, reason, by) error`
    - `ActiveRestriction(ctx, account, kind) (*Restriction, error)`

- [ ] **Step 1: Write failing unit tests:**
  - `TestMakeOffer_RequiresFundedWallet`: short → `INSUFFICIENT_FUNDS`.
  - `TestMakeOffer_WalletMustBeBuyers`.
  - `TestMakeOffer_LimitTwenty` returns `OFFER_LIMIT`.
  - `TestMakeOffer_BelowMinimum` returns `OFFER_BELOW_MINIMUM`.
  - `TestMakeOffer_Suspended` returns `MARKET_RESTRICTED`.
  - `TestMakeOffer_ExpiryClamp`: no expiry → +7d; 40d → error.
  - `TestAccept_UnfundedFlipsAndFails` returns `OFFER_UNFUNDED`, and the status becomes `unfunded`.
  - `TestAccept_SupersedesOthers`.
  - `TestPayOffer_ReturnsFreshQuote`.
  - `TestRecheck_FlipsBothWays`.
  - `TestLapse_ThirdStrikeSuspends`: the third lapse in 30 days creates an `offer_suspension` until now+30d.
- [ ] **Step 2:** Run `go test ./internal/market/ -run 'Offer|Accept|Recheck|Lapse' -v`. Expect FAIL.
- [ ] **Step 3: Implement.** The seller only sees `live` offers in `Inbox`.
- [ ] **Step 4:** Run the tests. Expect PASS. Add the integration test `TestMarket_OfferAcceptPay`, which is skipped until Task 9.
- [ ] **Step 5: Checkpoint.**

### Task 9: Worker: observe, complete, expire, late payment

**Files:**
- Create: `max-core/internal/market/worker.go`, `worker_test.go`
- Modify: `cmd/api/main.go` (start the worker only when `Market.Enabled`, on the same ticker pattern as `renames/worker.go`)

**Interfaces:**
- Produces:
  - `func (s *Service) Tick(ctx context.Context) (Result, error)`, where `Result{Completed, Expired, ManualReview, FundingChecked int}`
  - `func RunWorker(ctx, s *Service, every time.Duration, log *zap.Logger)`

Order inside `Tick`:
1. Observe submitted and confirming sales (due by `next_check_at`).
2. Expire locked sales that are past their lock and have no tx. An expired offer lapses and records a strike.
3. Recheck funding.

- [ ] **Step 1: Write failing unit tests:**
  - `TestTick_CompletesOnMatch`.
  - `TestTick_MismatchKeepsSaleAndRecordsEvent`: `TX_MISMATCH` is recorded as an event, and the sale stays `submitted` until the lock expires, then expires.
  - `TestTick_ExpiresUnpaidLock`.
  - `TestTick_SubmittedPastLockStillCompletes`.
  - `TestTick_LateValidTxAfterExpiry_Completes`, when the name is unsold and still the seller's.
  - `TestTick_LateValidTxAfterResale_ManualReview`.
  - `TestTick_ExpiredOfferLapsesWithStrike`.
- [ ] **Step 2:** Run `go test ./internal/market/ -run TestTick -v`. Expect FAIL.
- [ ] **Step 3: Implement.** Late transactions are found by a Submit on an expired sale: `Submit` accepts a tx for `expired` sales and moves them to `confirming` for this path only.
- [ ] **Step 4:** Remove the Task 7/8 skips, then run `go test ./internal/market/ -v` and `go test -tags integration ./internal/server/ -run TestMarket_ -v`. Expect PASS.
- [ ] **Step 5: Checkpoint.**

### Task 10: Agents: scopes, market policy, approvals

**Files:**
- Create: `max-core/internal/market/agents.go`, `agents_test.go`
- Modify: `internal/agents/domain.go` (scopes), `internal/agents/policy.go` (`MarketPolicy{CanSell bool; MinSalePrice string}` in `Policy`; `EvaluateMarket`), `internal/agents/repository.go`, `internal/agents/handler.go` (policy DTO, approval routes), `db/queries/agents.sql`, `router.go`

**Interfaces:**
- Produces:
  - `agents.ScopeMarketRead = "market:read"`, `ScopeMarketSell = "market:sell"`, `ScopeMarketBuy = "market:buy"`, all added to `AllScopes`
  - `func EvaluateMarket(p Policy, status Status, action string, amount *big.Int, chain, asset string, spent24h *big.Int) Evaluation`
    - `buy`, `offer` and `pay` use the existing limits logic.
    - `list` and `accept` need `CanSell && amount >= MinSalePrice`.
  - `market.agentPolicy`, which implements `AgentPolicy`:
    - For human principals it returns `auto_approved`.
    - For agents it writes `agent_market_authorizations`. When the decision is `pending_approval`, the service returns `PENDING_APPROVAL` with `authorizationId`, and the action is resumed by the approval endpoint.
  - Routes `POST /v1/agent-approvals/market/:authorizationId/approve|deny`, for sessions only and only the agent's administrator. On approve, replay the stored action with its stored inputs.
  - An offer's spend counts at accept/pay, not at make (spec §8).

- [ ] **Step 1: Write failing tests:**
  - `TestEvaluateMarket_BuyWithinLimit`.
  - `TestEvaluateMarket_BuyOverLimitPending`.
  - `TestEvaluateMarket_SellNeedsCanSell`.
  - `TestEvaluateMarket_SellBelowMinPending`.
  - `TestAgentPolicy_HumanAutoApproved`.
  - `TestApprove_ReplaysBuy`.
  - `TestApprove_OnlyAdministrator`.
  - `TestScope_MissingMarketBuy403`, an integration test in which a key with only `market:read` calls buy and gets 403.
- [ ] **Step 2:** Run `go test ./internal/agents/ ./internal/market/ -run 'Market|Approve|Scope' -v`. Expect FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4:** Run the tests plus the integration subset. Expect PASS.
- [ ] **Step 5: Checkpoint.**

### Task 11: Admin controls

**Files:**
- Create: `max-core/internal/market/admin.go`, `admin_test.go`
- Modify: `handler.go`, `router.go` (under the existing `ops` group), `db/queries/market.sql`

**Interfaces:**
- Produces: `Admin*` service methods matching the §9 admin routes. Each takes `actor uuid.UUID` and a `reason string` (required, non-empty) and writes `audit_logs`.
  - `AdminReverse(ctx, actor, saleID, reason)` requires the `completed` status and the buyer still owning the name. It calls `maxids.TransferOwnership` back to the seller, with no destination preset (`DestinationWalletID = uuid.Nil` means none), and sets the sale to `reversed`.
  - `AdminApprove` sets a `pending_review` sale to `locked` and starts `lock_expires_at` now.

- [ ] **Step 1: Write failing tests:**
  - `TestAdmin_NonAdmin403` (integration).
  - `TestAdminApprove_StartsLock`.
  - `TestAdminDeny_ReleasesListing`.
  - `TestAdminDelist_ShowsReason`.
  - `TestAdminCancel_OnlyBeforeTx`.
  - `TestAdminReverse_RestoresSeller`.
  - `TestAdminReverse_RequiresReason`.
  - `TestRestriction_BanBlocksListOfferBuy`.
  - `TestRestriction_LiftRestores`.
- [ ] **Step 2:** Run `go test ./internal/market/ -run Admin -v`. Expect FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4:** Run the tests. Expect PASS. Then run the full `make test` and `go test -tags integration ./...`. Expect PASS.
- [ ] **Step 5:** Update `max-core/README.md` with the market section, env vars and routes.
- [ ] **Step 6: Checkpoint.**

### Task 12: SDK

**Files:**
- Create: `max-sdk/src/resources/market.ts`, `max-sdk/src/solana.ts`
- Modify: `max-sdk/src/client.ts` (`readonly market: MarketApi`), `src/types.ts` (Listing, Offer, Sale, SaleView, Quote, MarketInbox, NameMarketView; `ownerChangedAt?: string` on resolve/profile types), `src/errors.ts` (new codes), `package.json` (`exports["./solana"]`, optional peer deps `@solana/web3.js`, `@solana/spl-token`), `README.md`, `test/unit.test.ts`

**Interfaces:**
- Produces:
  - `MarketApi` methods, named as in spec §12, each mapping 1:1 to a §9 route.
  - `assertSaleInstructionsMatch(sale: Sale, ins: MarketInstructions): void`
  - `buildSolanaSaleTransaction(ins: MarketInstructions, payer: string, recentBlockhash: string): VersionedTransaction` from `@max/sdk/solana`

- [ ] **Step 1: Write failing unit tests:**
  - Each `MarketApi` method hits the right method, path and body. Use the existing fetch-mock pattern in `unit.test.ts`.
  - `assertSaleInstructionsMatch` throws on a wrong fee, wrong seller, or missing memo.
  - `buildSolanaSaleTransaction` produces instructions in the §5 order, with program ids for ATA, Token and Memo.
- [ ] **Step 2:** Run `cd max-sdk && npm test`. Expect FAIL.
- [ ] **Step 3: Implement.** The main entry must not import `@solana/*` (check with `grep -r "@solana" dist/index.js` → no match after build).
- [ ] **Step 4:** Run `npm test && npm run build`. Expect PASS, and the grep finds no match.
- [ ] **Step 5: Checkpoint.**

### Task 13: MCP tools

**Files:**
- Modify: `max-mcp/src/server.ts`, `max-mcp/test/server.test.ts`, `max-mcp/README.md` (if present)

**Interfaces:**
- Consumes: `client.market.*` (Task 12).
- Produces:
  - Tools `market_search`, `market_get_listing`, `market_list_name`, `market_delist`, `market_make_offer`, `market_accept_offer`, `market_buy`, `market_get_sale`, with zod schemas.
  - `market_buy` and `market_accept_offer` return `{status:"pending_approval", authorizationId}` when the API returns `PENDING_APPROVAL`.
  - `market_buy` returns the quote and instructions and does not sign. Signing stays with the agent's signer, as for transfers.
  - Add one paragraph to `INSTRUCTIONS` on market tools and the 10% fee.

- [ ] **Step 1: Write failing tests:** the tool list includes all 8, each tool calls the right SDK method, and pending approval is mapped.
- [ ] **Step 2:** Run `cd max-mcp && npm test`. Expect FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4:** Run `npm test && npm run build`. Expect PASS.
- [ ] **Step 5: Checkpoint.**

### Task 14: Website

**Files:**
- Create:
  - `max-frontend/src/app/market/page.tsx`
  - `src/components/market/ListingCard.tsx`, `BuyModal.tsx`, `OfferModal.tsx`, `SellForm.tsx`, `MarketInbox.tsx`, `AdminMarket.tsx`
  - `src/app/dashboard/admin/market/page.tsx`
- Modify:
  - `src/app/[handle]/ProfileView.tsx` (market panel and the "Owner changed on <date>" banner)
  - `src/app/dashboard/page.tsx` (Market section)
  - the claim page under `src/app/claim/` (gate message)
  - shared error copy

**Interfaces:**
- Consumes: SDK `client.market.*`, `buildSolanaSaleTransaction`, and `assertSaleInstructionsMatch` (always called before signing).

- [ ] **Step 1: Build `/market`** with search, price range and sort, using cursor pagination.
- [ ] **Step 2: Build `BuyModal`.** It shows the quote breakdown (price, network fee in SOL, total, seller receives) and a lock countdown. Sign with the connected wallet, submit, and poll `getSale` every 3s until it reaches a terminal status. Map the §9 errors to copy, with `INSUFFICIENT_FUNDS` showing need vs. have.
- [ ] **Step 3: Build `OfferModal`, `SellForm` and `MarketInbox`.** `SellForm` shows "You receive 90%: X USDC" and the ineligibility reason from `eligibility`. Add the profile panel and banner.
- [ ] **Step 4: Build the admin page.** It is hidden unless `/auth/me` reports admin, using the existing admin check used by reserved-claims UI.
- [ ] **Step 5: Verify.** Run `npm run build`, expecting success. Then run the dev server via `.claude/launch.json` and check with the browser pane, against a local max-core with `MARKET_ENABLED=true` and devnet assets: `/market` lists a seeded listing; the profile shows Buy now; the sell form shows 90%; the banner shows after a seeded transfer. Take a screenshot for proof.
- [ ] **Step 6: Checkpoint.**

### Task 15: Mobile app

**Files:**
- Create:
  - `max-app/src/app/(app)/(tabs)/market.tsx`
  - `src/app/(app)/market/[name].tsx`, `src/app/(app)/market/offers.tsx`
  - `src/app/(app)/sell.tsx`
- Modify:
  - `src/app/(app)/(tabs)/_layout.tsx` (Market tab)
  - `src/app/(app)/(tabs)/ids.tsx` (Sell action)
  - `src/services/chain.ts` (`signAndSendSale(ins, payer)` building a multi-instruction tx; keep the definite/ambiguous broadcast classification)
  - `src/hooks/queries.ts`, `src/app/(app)/review.tsx` (sale review variant), `src/app/(app)/claim.tsx` (gate message)

**Interfaces:**
- Consumes: SDK market API and `assertSaleInstructionsMatch`. The app builds the transaction with its own `@solana/web3.js` via `buildSolanaSaleTransaction`.

- [ ] **Step 1: Write the `chain.ts` test.** If the app has a test runner, add `signAndSendSale` with a fake connection asserting the instruction order. Otherwise verify in Step 4.
- [ ] **Step 2: Build the screens and the tab.** Don't edit app files with `sed -i` while Metro runs; it loses watched files.
- [ ] **Step 3:** Run `npx tsc --noEmit` in `max-app`. Expect no errors.
- [ ] **Step 4: Verify in the iOS Simulator** against local max-core on devnet: Market tab lists, the detail screen shows Buy now, the Sell form shows 90%, and review shows the quote breakdown. A full devnet settlement needs funded devnet USDC and SOL. If that's unavailable, record it, and rely on the Task 9 integration tests for server completion.
- [ ] **Step 5: Checkpoint.**

---

## Spec coverage

| Spec § | Task |
|---|---|
| §2 decisions, §10 config | 1 |
| §3 eligibility | 5 (rename/edit guards), 6 (`Listable`), 7 |
| §4 data model | 1 |
| §5 adapter, price feed, gate | 2, 3, 4 |
| §6 list, buy, complete | 5, 7, 9 |
| §6 offers | 8 |
| §6 expiry, late payment | 9 |
| §6 public read model | 5, 7 |
| §7 anti-fraud | 6, 7, 8, 11 |
| §8 agents | 10 |
| §9 API, errors | 7, 8, 10, 11 |
| §11 UI | 14, 15 |
| §12 SDK, MCP | 12, 13 |
| §13 testing | every task |
