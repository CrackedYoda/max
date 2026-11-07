# Default MAX IDs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every account one long random default MAX ID that skips the $100 gate and can't be traded or renamed. Build it so that switching the name style later is a config change.

**Architecture:** The pieces are:
- a `DefaultNamer` interface in `maxids` (random now, words later);
- an `is_default` column with one default per account;
- `CreateDefault`, called inside the account-creation transactions (login and agent);
- an idempotent startup backfill;
- guards in claim, the cap, market eligibility, rename and transfer;
- new API fields that clients display.

**Tech Stack:** Go/Gin/pgx/sqlc/golang-migrate; TypeScript SDK; Next.js; Expo.

**Spec:** `max-core/docs/2026-11-07-default-max-ids-design.md`

## Global Constraints

- **Random style:** 16 chars from the alphabet `23456789abcdefghjkmnpqrstuvwxyz`, with ≥3 digits and ≥3 letters, generated with crypto/rand.
- **Words shape (reserved only):** `^[a-z]{3,10}-[a-z]{3,10}-[0-9]{4}$`.
- **Style config:** `DEFAULT_ID_STYLE` defaults to `random`. An unknown value fails boot. `words` currently fails boot with "not implemented".
- **One default per account.** It doesn't count toward `MaxIDsPerAccount`. It is never listable, buyable, offerable, renameable or transferable.
- **Integration tests** use `TEST_DATABASE_URL=postgres://max:max@localhost:5433/max_market_test?sslmode=disable`. Never use Railway. Never print `.env` secrets.
- **Migrations:** apply 000012 to the local dev and test DBs only. The hosted DB is migrated only when the user asks.
- **Commits:** commit only when the user has authorised it. "Checkpoint" means tests are green, `gofmt -l` is empty, and `go vet` passes.

## Review Focus

1. **Two logins for a brand-new wallet at once.** Only one account and one default should be created. The existing wallet-uniqueness behaviour must still hold. Covered by a test in Task 3.
2. **A claim racing the backfill.** An account must never end up with two defaults; the unique index is the guard. Covered by a test in Task 3.
3. **A user claiming `quiet-river-4821` before the words style exists.** The claim must be refused. Covered by a test in Task 4.
4. **A default ID as a market buyer's existing ID.** The buyer's cap counts only claimed IDs. Covered by a test in Task 4.
5. **Rolling back 000012.** Defaults must survive as ordinary IDs. Covered by a test in Task 2.

---

### Task 1: DefaultNamer

**Files:** Create `max-core/internal/maxids/defaultname.go` and `defaultname_test.go`.

**Interfaces (produces):**
- `type DefaultNamer interface { Generate() (string, error); Owns(canonical string) bool }`
- `func NewDefaultNamer(style string) (DefaultNamer, error)`
- `func IsDefaultShape(canonical string) bool`
- `const DefaultStyleRandom = "random"; const DefaultStyleWords = "words"`

- [ ] **Step 1: Write the failing tests.**
  - `TestRandomNamer_Shape`: 1,000 generated names each pass `Owns` and `maxids.Normalize`, and each is unique.
  - `TestIsDefaultShape`:
    - true for `u7k3q9x2m4tab8zz`-style names and for `quiet-river-4821`;
    - false for `alice`, `quiet-river-48`, a 16-char name with only 2 digits, and a 16-char name that contains `o`.
  - `TestNewDefaultNamer`: `random` succeeds; `words` and `bogus` return errors.
- [ ] **Step 2:** Run `cd max-core && go test ./internal/maxids/ -run 'Namer|DefaultShape' -v`. Expect FAIL.
- [ ] **Step 3: Implement.** Generate with rejection sampling until the digit and letter minimums are met.
- [ ] **Step 4:** Run the same command. Expect PASS.
- [ ] **Step 5:** Checkpoint.

### Task 2: Schema, CreateDefault, config

**Files:**
- Create: `db/migrations/000012_default_max_ids.{up,down}.sql`
- Modify: `db/queries/maxids.sql` (add `CreateDefaultMaxID`, `GetDefaultMaxID`, `AccountsWithoutDefault`; make `CountMaxIDsByOwner` exclude `is_default`), `internal/maxids/{transfer.go,repository.go,domain.go,error.go}`, `internal/config/config.go` (+ `DefaultIDStyle`)
- Create: `internal/maxids/default.go`

**Interfaces (produces):**
- `func CreateDefault(ctx, q *generated.Queries, namer DefaultNamer, owner uuid.UUID, typ Type, dest *WalletRef) (MaxID, error)`
  - `WalletRef{ID uuid.UUID; Namespace chains.Namespace}`
  - up to 5 retries on a unique violation
  - sets `listable_at = 'infinity'`
  - writes audit `maxid.default_created`
- `MaxID.IsDefault bool`
- `ErrDefaultNotTransferable` (code `DEFAULT_ID_LOCKED`, 409)
- `config.Config.DefaultIDStyle string`

- [ ] **Step 1: Write the migration** per spec §3. Run `make sqlc` with the new queries. Apply to the dev and test DBs.
- [ ] **Step 2: Write the failing integration tests.**
  - `TestCreateDefault_OnePerAccount`: a second call on the same owner errors on the unique index.
  - `TestCreateDefault_SetsDestination`.
  - `TestMigration012_DownKeepsRows`: inside a tx, migrate down, check rows still exist with no column, migrate up, roll back.
- [ ] **Step 3: Implement** `CreateDefault`, the `IsDefault` mapping in the repository, and the config field with a boot error for an unknown style.
- [ ] **Step 4:** Run the tests. Expect PASS. Then run `go test ./...`.
- [ ] **Step 5:** Checkpoint.

### Task 3: Creation at login and agent create; backfill

**Files:**
- Modify:
  - `internal/auth/repository.go`: after CreateWallet in the new-account branch, call `CreateDefault` with dest = the new wallet.
  - `internal/agents/repository.go` `CreateAgent`: call `CreateDefault` with dest nil and typ agent.
  - both constructors take a `maxids.DefaultNamer`.
  - `cmd/api/main.go`
- Create: `internal/maxids/backfill.go`

**Interfaces (produces):** `func BackfillDefaults(ctx, pool *pgxpool.Pool, namer DefaultNamer, batch int, log *zap.Logger) (int, error)`.

- [ ] **Step 1: Write the failing integration tests.**
  - `TestLogin_NewAccountGetsDefault`: exactly 1 default, with the destination being the login wallet.
  - `TestLogin_ConcurrentNewWallet`: two goroutines log in with the same new wallet; the result is 1 account and 1 default.
  - `TestAgentCreate_GetsDefault`.
  - `TestBackfill_Idempotent`:
    - insert 3 accounts without defaults via raw SQL, one with an active wallet;
    - run the backfill twice;
    - expect 3 defaults with correct destinations, and the second run returns 0.
  - `TestBackfill_RacesCreate`: the backfill runs concurrently with `CreateDefault` on the same account; the account ends with 1 default.
- [ ] **Step 2: Implement.**
  - Update the existing integration test env construction to pass the namer.
  - `main` builds the namer from config and runs `BackfillDefaults` once in a goroutine at startup, with batch 100.
- [ ] **Step 3:** Run all of `go test ./...` and the integration suite. Expect PASS.
- [ ] **Step 4:** Checkpoint.

### Task 4: Guards

**Files:**
- `internal/maxids/service.go` (Claim)
- `internal/agents/service.go` (ClaimID path, if it validates separately)
- `internal/market/{eligibility.go,domain.go,repository.go}`
- `db/queries/market.sql` (`GetMarketNameFacts` + `is_default`; buyer-cap count excludes defaults)
- `internal/renames/service.go`
- `internal/maxids/transfer.go`

**Interfaces:**
- `NameFacts.IsDefault`
- `ReasonDefaultID = "default_id"`, publicly masked to `unavailable`
- renames error `ErrDefaultNotRenameable` (code `DEFAULT_ID_LOCKED`)

- [ ] **Step 1: Write the failing tests.**
  - Unit:
    - `TestListable_DefaultID`;
    - `TestPublicReason_DefaultMasked`.
  - Integration:
    - `TestClaim_RefusesDefaultShapes`: a random-shape name and `quiet-river-4821` both give INVALID_NAME with "reserved format".
    - `TestCap_IgnoresDefault`: an account with a default plus 10 claimed IDs can't claim an 11th, but can hold all 10 claimed.
    - `TestMarket_DefaultNotListable`: list gives NOT_LISTABLE; offer on a default gives NOT_LISTABLE.
    - `TestMarket_BuyerCapIgnoresDefault`.
    - `TestRename_DefaultRefused`.
    - `TestTransferOwnership_DefaultRefused`.
- [ ] **Step 2: Implement.**
- [ ] **Step 3:** Run all suites. Expect PASS.
- [ ] **Step 4:** Checkpoint.

### Task 5: API fields

**Files:**
- `internal/maxids/dto.go`
- `internal/resolver/*` (`isDefault` on profile/resolve)
- `internal/auth/{service.go,dto.go,handler.go}`: `Me` returns `defaultMaxId` and `primaryMaxId` as `{id, handle}`. Primary is the oldest non-default active ID, else the default.
- `db/queries/auth.sql` or `maxids.sql`, for the lookup

- [ ] **Step 1: Write the failing integration tests.**
  - `TestMe_DefaultAndPrimary`: new account → primary == default; after claiming `alice` → primary == alice.
  - `TestIDs_IsDefaultField`.
  - `TestResolve_DefaultWorks`.
- [ ] **Step 2: Implement.**
- [ ] **Step 3:** Run the suites. Expect PASS. Update `max-core/README.md` (Endpoints and env `DEFAULT_ID_STYLE`) and `.env.example`.
- [ ] **Step 4:** Checkpoint.

### Task 6: SDK, web, app

**Files:**
- SDK: `max-sdk/src/types.ts` (`isDefault`; `Me.defaultMaxId`, `Me.primaryMaxId`), plus `test/unit.test.ts` and README.
- Web:
  - `max-frontend/src/components/identity/IdCard.tsx`: "Default ID" badge; hide Sell and Rename when `isDefault`.
  - `src/components/Header.tsx`: use `primaryMaxId`.
  - claim page: the line "You already have max.… for receiving".
- App:
  - `max-app/src/app/(app)/(tabs)/ids.tsx`: badge; hide Sell and Rename.
  - `(tabs)/home.tsx`: use `primaryMaxId`.

- [ ] **Step 1:** Update the SDK types and tests. Run `cd max-sdk && npm test && npm run build`. Expect PASS.
- [ ] **Step 2:** Web changes. Run `npx tsc --noEmit && npm run lint && npm run build`. Expect clean.
- [ ] **Step 3:** App changes. Run `npx tsc --noEmit && npx expo lint`. Expect clean. Do not touch Metro or the Simulator.
- [ ] **Step 4:** Checkpoint.

## Spec coverage

| Spec | Task |
|---|---|
| §2 format, switchable, reserved shapes | 1 |
| §3 data, config | 2 |
| §4 creation, backfill | 3 |
| §4 rules | 4 |
| §4 API | 5 |
| §5 clients | 6 |
| §6 tests | each task |
