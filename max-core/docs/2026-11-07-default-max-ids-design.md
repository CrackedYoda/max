# Default MAX IDs — Design Spec

Date: 2026-11-07
Status: Implemented

## 1. Goal

Every account always has a MAX ID it can receive on, without the $100
registration gate. Each account gets one long, random **default MAX ID** at
creation. Chosen names (`max.alice`) keep the gate and every existing rule.

This is the first of three sub-projects. The other two, receive-only pasted
addresses and BTC support, get their own specs.

## 2. Decisions

| Topic | Decision |
|---|---|
| Format (v1) | Style A, `random`: 16 chars from `a-z0-9` minus `0 o 1 l i`, with ≥3 digits and ≥3 letters, e.g. `max.u7k3q9x2m4tab8zz` |
| Switchable | A `DefaultNamer` interface. `DEFAULT_ID_STYLE=random\|words` chooses the generator for new defaults. `words` (`adjective-noun-NNNN`) is added later. Existing defaults keep their names. |
| Reserved shapes | Users can never claim a name matching any style's shape (random or words), whichever style is active. |
| Count | Exactly one default per account (human and agent) |
| Created | Human accounts: in the first-login transaction. Agent accounts: in the agent-creation transaction. Existing accounts: startup backfill. |
| Destination | Human: the login wallet, for its namespace. Agent: none until it links a wallet. Backfill: the oldest active linked wallet per namespace, if any. Linking a wallet fills the default's destination for that namespace when it has none (never overwrites an existing one). |
| $100 gate | Not applied |
| 10-ID cap | Defaults don't count |
| Market | Never listable, buyable, offerable or transferable |
| Rename | Not allowed. To get a real name, claim one (gate applies). |
| Delete | Not allowed |
| Editable | Destinations, profile, settlement preference |
| Display | `primaryMaxId` = the oldest active claimed (non-default) ID by ID creation time, else the default. The default keeps resolving as an alias. |

## 3. Data model (migration `000012_default_max_ids`)

- `max_ids.is_default boolean NOT NULL DEFAULT false`
- `CREATE UNIQUE INDEX max_ids_one_default ON max_ids (owner_account_id) WHERE is_default`
- Down: drop the index and the column. It does not delete the default rows: they become ordinary IDs.
- Defaults are inserted with both `listable_at` and `rename_available_at` set to `9999-12-31`. The
  `is_default` checks are the real guard; the two dates are belt-and-braces, so a binary that predates
  `is_default` (after a rollback) still never lists or renames a default. The down migration keeps both
  timestamps on purpose: the former defaults stay unlistable and unrenameable as ordinary IDs.

## 4. Components

### `internal/maxids/defaultname.go`

```go
type DefaultNamer interface {
    Generate() (string, error)   // crypto/rand
    Owns(canonical string) bool  // shape test for this style
}
func NewDefaultNamer(style string) (DefaultNamer, error) // "random" (v1); "words" returns an error until built
func IsDefaultShape(canonical string) bool               // true if ANY style owns it
```

- **Random shape:** exactly 16 chars, all in the alphabet `23456789abcdefghjkmnpqrstuvwxyz`, ≥3 digits, ≥3 letters.
- **Words shape:** `^[a-z]{3,10}-[a-z]{3,10}-[0-9]{4}$`. It is reserved now so a later switch can't collide with a claimed name.

### Creating a default

```go
func CreateDefault(ctx context.Context, q *generated.Queries, namer DefaultNamer,
    owner uuid.UUID, typ Type, destWallet *WalletRef, source DefaultSource) (MaxID, error)
```

- Runs inside the caller's transaction.
- Generates a name and inserts it with `is_default = true`. On a unique-name conflict it retries, up to 5 attempts.
- Sets `listable_at` and `rename_available_at` to `9999-12-31` (see §3).
- When `destWallet` is given, inserts its destination.
- Writes an audit entry, `maxid.default_created`, with metadata `source` = `login`, `agent` or `backfill`.

Callers:
- `auth/repository.go`, right after `CreateAccount` and `CreateWallet`.
- `agents/repository.go`, right after the agent account is created.

### Filling a missing destination

`auth/repository.go` `LinkWallet` (used by both human links and the agent
wallet-verify path) calls `maxids.FillDefaultDestination` in the same
transaction. When the account's active default has no destination in the new
wallet's namespace, the wallet becomes it, `resolution_version` is bumped and
`maxid.destination_set` is audited, as `SetDestination` does. An existing
destination is never overwritten.

### Backfill

`maxids.BackfillDefaults(ctx, pool, namer, batch int, log *zap.Logger) (int, error)`:
- Walks accounts with no default (`NOT EXISTS`) by keyset paging on `(created_at, id)`, locking each
  page `FOR NO KEY UPDATE SKIP LOCKED` (so foreign-key inserts such as sessions and wallets are not
  blocked), in batches of 100, one transaction per page.
- Each account runs under its own savepoint: one that fails is logged, rolled back and passed over,
  and the failures come back as one aggregate error alongside the count. An account that gained a
  default concurrently is skipped.
- For each, picks the oldest active linked wallet per namespace for destinations (at most one per namespace).
- `main` runs it once at startup in a goroutine and logs the count.
- It is idempotent: the unique index guards against running twice.

### Rules enforced

| Where | Rule |
|---|---|
| `maxids.Claim` and agent `ClaimID` | Refuse `IsDefaultShape(name)` → `ErrInvalidName` (code `INVALID_MAX_ID`, message "this name uses a reserved format") |
| `maxids.CheckAvailability` | `IsDefaultShape(name)` → `{available:false, reason:"invalid", detail:"this name uses a reserved format"}` |
| `CountByOwner` (10-cap) and market buyer-cap counting | Exclude `is_default` |
| `market.Listable` / NameFacts | New reason `default_id` (masked publicly as `unavailable`), so list, buy and offer all refuse |
| `renames.Eligibility` / `Request` | Refuse with "default IDs can't be renamed; claim a name instead" |
| `maxids.TransferOwnership` | Backstop: refuse `is_default` → `ErrDefaultNotTransferable` |
| Delete | No delete-ID endpoint exists today. Any future one must refuse defaults (noted in `maxids/service.go`). |

### API

- ID DTOs (`/v1/ids`, `/v1/ids/:id`, profile, resolve, and the `ids` of `GET /v1/agents/:id`) gain `isDefault`.
- `GET /v1/auth/me` gains `defaultMaxId` and `primaryMaxId`, each as `{id, handle}`.
- Public profile/resolve of a default ID works like any other ID.

### Config

`DEFAULT_ID_STYLE` (default `random`). Startup fails on an unknown style.

## 5. Clients

- **SDK:** `isDefault` on ID types; `defaultMaxId` and `primaryMaxId` on `me`.
- **Web:**
  - The dashboard ID cards show a "Default ID" badge and hide Sell and Rename on it.
  - The header uses `primaryMaxId`.
  - The claim page adds "You already have max.… for receiving".
- **App:** the same badge and hidden actions on the IDs tab. Home uses `primaryMaxId`.
- **MCP:** no change. Resolve already works.

## 6. Testing

- **Unit:**
  - Generator: alphabet, length, digit/letter minimums, randomness smoke check.
  - `Owns` and `IsDefaultShape` for both styles, plus negatives such as `alice` and `quiet-river-48`.
  - Unknown style is rejected.
- **Integration** (local test DB):
  - First login creates exactly one default with the login wallet as its destination.
  - Agent creation creates one default.
  - Backfill gives defaults to pre-existing accounts and is idempotent when run twice.
  - Claiming a default-shaped name is refused.
  - The 10-cap ignores defaults.
  - List, buy and offer of a default are refused.
  - Rename is refused.
  - `TransferOwnership` of a default is refused.
  - `/auth/me` returns both fields.

## 7. Out of scope

Receive-only pasted addresses, BTC, and the `words` generator itself (only its shape is reserved).
