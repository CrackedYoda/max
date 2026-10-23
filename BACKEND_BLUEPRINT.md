# Go Backend Blueprint

A portable blueprint for building production Go backends. Copy this into any new
project and follow it. It is opinionated on purpose — consistency across
projects is the point.

## 1. Philosophy

- **Modular monolith.** One deployable binary, clean internal boundaries,
  structured so a module could later be extracted into its own service.
- **Package by feature, not by layer.** Each feature (auth, user, order…) is a
  self-contained package that owns its whole vertical slice. You should be able
  to understand or delete a feature by looking at one directory.
- **Dependencies point inward.** `transport → service → repository → domain`.
  The domain knows nothing about frameworks or drivers. Nothing imports the
  transport layer.
- **Boring over clever.** A new engineer should understand any file without a
  guided tour. No abstraction without a concrete, present need.

## 2. Directory layout

```
cmd/api/main.go            Composition root: load config, open pools, wire
                           feature -> service -> handler, start server.

internal/
  config/                  Env loading + fail-fast validation. One Config struct.

  <feature>/               One package per feature. Contains:
    domain.go              Entities + typed enums. No framework/driver imports.
    interfaces.go          Repository / Service / dependency contracts (domain
                           types only). Defined here for mocking.
    repository.go          DB access. Maps sqlc-generated types <-> domain.
    service.go             Business rules. The only place logic lives.
    handler.go             Thin HTTP handlers: bind -> call service -> respond.
    dto.go                 Request/response shapes + validation tags.
    error.go               Feature-specific sentinel errors.
    service_test.go        Table-driven unit tests against mocked interfaces.

  shared/                  Cross-cutting kernel: AppError, response helpers,
                           logger, redaction. Imported by everything.

  middleware/              HTTP middleware: request id, logging, recovery, cors,
                           auth, rbac, per-resource guards.

  server/                  Router (route table + Deps) and server lifecycle
                           (graceful shutdown).

  database/  redis/  sms/  Infrastructure adapters. Each: Connect() + a small
                           typed client. Concrete types satisfy feature
                           interfaces structurally.

db/
  migrations/              golang-migrate up/down pairs. THE schema source of truth.
  queries/                 Handwritten .sql — input to sqlc.
  generated/               sqlc output. Committed. NEVER hand-edited.

docs/                      This blueprint + project/AI guides.
sqlc.yaml  .env.example  go.mod
```

## 3. The layers, precisely

- **domain** — plain structs and typed string enums. Zero imports beyond stdlib
  + `uuid`. This is the vocabulary every other layer speaks.
- **repository** — wraps sqlc. Converts generated types to/from domain types so
  `generated.*` never escapes the package. Maps `pgx.ErrNoRows` to a sentinel.
  Owns transactions.
- **service** — all business rules, validation beyond shape, ownership checks,
  orchestration. Depends on interfaces, never concrete infra. Returns domain
  types and `AppError`s.
- **handler** — bind + validate the DTO, call one service method, map the
  result to a response. ~10 lines. No branching on business state.
- **interfaces** — declared on the consumer side (in the feature package) using
  domain types. This is what makes services unit-testable and infra swappable.

## 4. Error handling

One error type carries everything the edge needs:

```go
type AppError struct {
    Code    string   // stable machine code, e.g. "USER_NOT_FOUND"
    Status  int       // HTTP status
    Message string    // safe to show the client
    Err     error     // wrapped cause (logged, never sent)
}
```

Rules:

- **Sentinels are defined once** in `shared/error.go` (global) and
  `<feature>/error.go` (feature-specific). Every layer references them.
- **Attach causes with `.WithError(err)`** — it clones the sentinel and keeps
  the code/status. `AppError.Is` matches by `Code`, so
  `errors.Is(sentinel.WithError(x), sentinel)` is true.
- **One responder** maps any error to the wire shape and logs 5xx as error,
  4xx as warn:
  ```json
  { "error": { "code": "STRING", "message": "STRING" } }
  ```
- **Internal errors never leak.** Log the full cause server-side with the
  request id; return a generic 500 to the client.
- Success uses a matching envelope: `{ "data": <payload> }`.

## 5. sqlc workflow

1. Write/modify a migration in `db/migrations/` (schema is defined here only).
2. Write the query in `db/queries/<feature>.sql`.
3. `sqlc generate` → typed Go in `db/generated/`.
4. Call generated code **only from the repository wrapper**, never from a
   service or handler.
5. Commit `db/generated/`. Never edit it by hand.

## 6. Config

- Load from env (seed from `.env` in dev). One `Config` struct, grouped by area.
- **Validate at startup and fail fast**: missing DB URL, secret shorter than 32
  chars, unknown enum values, wildcard CORS in production → refuse to boot.
- Never hardcode secrets. Ship a `.env.example` documenting every variable.

## 7. Logging & observability

- Structured logging with `zap`. No `fmt.Println` / `log.Println`.
- A **request-id** middleware sets an id; a **request-logger** middleware puts a
  request-scoped logger on the context. Pull it with a `GetLogger(c)` helper so
  every log line correlates.
- **Redact** secrets and PII: never log full tokens, OTP codes, or phone
  numbers. Provide `RedactToken` / `RedactPhone` helpers.

## 8. HTTP

- Middleware order: `request-id → request-logger → recovery → cors → routes`.
- `/healthz` pings every critical dependency (DB, cache) — readiness, not just
  liveness.
- Group routes by role; apply `Auth` then `RequireRole` (then resource guards)
  per group. Authorization is enforced server-side, never trusted from input.
- **Ownership checks live in the service.** Return 404 (not 403) for resources
  that belong to someone else, so existence isn't leaked.
- Graceful shutdown: on SIGINT/SIGTERM stop accepting, drain in-flight with a
  timeout, then close pools.

## 9. Security defaults

- `crypto/rand` for anything secret (codes, tokens). Never `math/rand`.
- Compare secrets with `crypto/subtle.ConstantTimeCompare`.
- Store only hashes of tokens/codes. Fast hash (SHA-256/HMAC) for high-entropy
  values; slow hash (argon2/bcrypt) only for human passwords.
- Rotate refresh tokens with family-based **reuse detection**.
- Rate-limit sensitive endpoints independently by the axes that matter
  (per-identity AND per-IP).

## 10. Testing

- Table-driven unit tests per service, with dependencies **mocked via the
  feature's interfaces** (in-memory fakes, no DB).
- Test the rules, not the framework: success + every rejection path.
- One integration test per critical flow against a real (separate) test DB.

## 11. Comment policy

Code should read without narration. See a project's AI guide for the exact
house rule — the default here is: **comments explain a non-obvious _why_, never
restate _what_ the code already says.** Delete comments that duplicate the
identifier name.

## 12. Adding a new feature module (checklist)

1. `db/migrations/*` for the schema, then `db/queries/<feature>.sql`, then
   `sqlc generate`.
2. `internal/<feature>/domain.go` — entities + enums.
3. `internal/<feature>/interfaces.go` — Repository/Service contracts.
4. `internal/<feature>/repository.go` — map generated ↔ domain.
5. `internal/<feature>/service.go` — rules; `_ Service = (*svc)(nil)`.
6. `internal/<feature>/dto.go` + `handler.go`.
7. `internal/<feature>/error.go` — sentinels.
8. Wire into `server/router.go` (`Deps` + routes) and `cmd/api/main.go`.
9. `service_test.go` — table-driven, mocked.
10. `gofmt -w`, `go vet ./...`, `go test ./...` before done.
```
