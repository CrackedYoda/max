# Pasted Addresses and Receive-Only Chains Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let owners paste a confirmed, unverified receive address on any namespace, and add Bitcoin, Litecoin and Tron as resolve-only chains.

**Architecture:** Pasted addresses live on `max_id_destinations` (nullable `wallet_id` + `address` + `confirmed_at`, exactly-one CHECK). `internal/chains` gains the new namespaces, a `ReceiveOnly` chain flag and checksum validators. Every money path refuses receive-only chains. SDK, web and app add a paste-and-confirm flow and an "Unverified" badge.

**Tech Stack:** Go/Gin, pgx v5, sqlc, golang-migrate; TypeScript SDK; Next.js 16 web; Expo app.

**Spec:** `max-core/docs/2026-11-10-pasted-addresses-receive-only-chains-design.md`

## Global Constraints

- Only the hosted Railway `max` database. Never the `railway` database. Never run integration tests (they TRUNCATE) against it.
- Never print secrets from `.env`.
- Work on branch `feat/pasted-addresses` in worktree `.claude/worktrees/pasted-addresses`; never switch branches in the main checkout. Merge to `main` only when the user says so.
- Ports 3000, 3100, 8080, 8081 belong to other sessions; test API on :8095, web on :3200.
- Copy, verbatim: "Unverified"; "Pasted, not proven by a wallet signature."; "I control this address. Payments sent to it can't be reversed."; EVM warning "This address will receive on every EVM chain MAX supports (Ethereum, Base, Arbitrum, OP, Polygon). Exchange deposit addresses often accept only one chain."; resolve note "Receive-only on MAX: send from your own wallet."; profile "Receive-only: send from your own wallet"; payment error "MAX can't send on <Chain> yet."
- Error codes: `INVALID_ADDRESS` (400), `CONFIRMATION_REQUIRED` (400), `RECEIVE_ONLY_CHAIN` (422); existing `UNSUPPORTED_CHAIN` for walletId on a paste-only namespace.

## Review Focus

1. Whitespace around a pasted address ("  bc1q…\n") should be trimmed and accepted, not rejected. → Task 1 test.
2. Uppercase bech32 (`BC1Q…`, valid per BIP-173 for QR codes) should be accepted and stored lowercase; mixed case rejected. → Task 1 test.
3. A Litecoin `3…` address is byte-identical in format to a Bitcoin P2SH one; pasting it on either namespace must be accepted (it's the owner's confirmed choice). → Task 1 test.
4. A wallet link must still not overwrite a pasted address on a default ID (fill only empties the slot). → Task 2 integration test.
5. An in-app send whose recipient's settlement preference fallback would route to a receive-only chain must be refused, not quoted. → Task 3 test (settlement can't be set there; prepare also guards `dest`).

## Rulings

- Client-side checks are shape-only (prefix/charset/length) via one SDK helper `looksLikeAddress`; the server is authoritative with full checksums. Avoids three copies of bech32.
- Integration tests are written but run only with a throwaway `TEST_DATABASE_URL`; on this machine they are compile-checked (`go vet -tags integration`) and the same behaviours are exercised by the hosted e2e script in Task 7, which creates only new test accounts and deletes them afterwards.
- `max-mcp` has no destination tool, so it is untouched.

---

### Task 1: Chains — namespaces, receive-only chains, address validators

**Files:**
- Modify: `max-core/internal/chains/chains.go`, `verify.go`, `evm.go`
- Create: `max-core/internal/chains/base58check.go`, `bech32.go`, `utxo.go` (BTC/LTC), `tron.go`
- Test: `max-core/internal/chains/address_test.go`

**Interfaces — Produces:**
- `NamespaceLitecoin = "litecoin"`, `NamespaceTron = "tron"`; `ParseNamespace` accepts all five.
- `Chain.ReceiveOnly bool \`json:"receiveOnly"\``; `Supported` gains `bitcoin` (BTC, 8, `https://mempool.space/tx/`), `litecoin` (LTC, 8, `https://litecoinspace.org/tx/`), `tron` (TRX, 6, `https://tronscan.org/#/transaction/`), all `ReceiveOnly: true`, mainnet.
- `func IsPasteable(ns Namespace) bool` — true for all five. `IsLinkable` unchanged.
- `NormalizeAddress(ns, addr)` handles all five (trims whitespace first); returns canonical form. `DisplayAddress` unchanged in shape (EVM checksum, else as stored).
- `NormalizeEVMAddress`: mixed-case input must satisfy EIP-55, else error.

- [ ] **Step 1: Write failing tests** `TestNormalizeAddress_Valid` / `_Invalid` (table-driven). Valid vectors: BTC `1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2`, `3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy`, `bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq`, `bc1qrp33g0q5c5txsp9arysrx4k6zdkfs4nce4xj0gdcccefvpysxf3qccfmv3`, `bc1p5d7rjq7g6rdk2yhzks9smlaqtedr4dekq08ge8ztwac72sfr9rusxg3297`, upper-case `BC1QAR0SRRR7XFKVY5L643LYDNW9RE59GTZZWF5MDQ` → lower; LTC `LaMT348PWRnrqeeWArpwQPbuanpXDZGEUz`, `MGxNPPB7eBoWPUaprtX9v9CXJZoD2465zN`, `3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy`, `ltc1qg82nvlvq4f3r6ndxcnww2m95fcz7z6dljhr6ar`; Tron `TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t`; EVM `0x52908400098527886E0F7030069857D2E4169EE7` (valid EIP-55) and all-lower; `"  bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq\n"`. Invalid: each valid vector with its last char changed; mixed-case bech32; `bc1` with LTC namespace; `tb1…` testnet; Tron with version ≠ 0x41; EVM `0x52908400098527886e0F7030069857D2E4169EE7` (bad checksum); empty.
- [ ] **Step 2:** `cd max-core && go test ./internal/chains/` → FAIL.
- [ ] **Step 3: Implement.** Base58check = base58 decode + double-SHA256 4-byte checksum. Bech32 per BIP-173/350: witness v0 uses bech32 const 1 with program 20 or 32 bytes; v1–16 use bech32m const `0x2bc830a3`, program 2–40 bytes (v1 = 32). BTC base58 versions `0x00`,`0x05`, HRP `bc`; LTC versions `0x30`,`0x32`,`0x05`, HRP `ltc`; Tron base58check payload 21 bytes, first byte `0x41`.
- [ ] **Step 4:** `go test ./internal/chains/ && go build ./...` → PASS (build catches exhaustive switches).
- [ ] **Step 5:** Commit `feat(chains): litecoin and tron namespaces, receive-only chains, address validators`.

### Task 2: Pasted destinations — migration, queries, repo, service, API

**Files:**
- Create: `max-core/db/migrations/000013_pasted_destinations.up.sql`, `.down.sql`
- Modify: `max-core/db/queries/maxids.sql`, `resolver.sql`; regenerate `db/generated` (`sqlc generate`)
- Modify: `max-core/internal/maxids/{domain,dto,handler,service,repository,interfaces,error}.go`, `internal/resolver/repository.go`
- Test: `max-core/internal/maxids/service_test.go`, `max-core/internal/server/pasted_destinations_integration_test.go`

**Interfaces:**
- Consumes: `chains.IsPasteable`, `chains.NormalizeAddress` (Task 1).
- Produces: `maxids.Destination{Namespace, WalletID *uuid.UUID, Address string, Pasted bool, UpdatedAt}`; `Service.PasteDestination(ctx, caller, id uuid.UUID, namespace, address string, confirmed bool) (MaxID, error)`; `Repository.SetPastedDestination(ctx, id, owner uuid.UUID, ns chains.Namespace, address string) (MaxID, error)`; errors `ErrInvalidAddress` (`INVALID_ADDRESS`, 400, "address is not valid for <ns>" via WithMessage), `ErrConfirmationRequired` (`CONFIRMATION_REQUIRED`, 400, "confirm you control this address").
- Destination JSON: `{namespace, address, source: "wallet"|"pasted", verified, walletId: string|null, updatedAt}`.

- [ ] **Step 1: Migration.** Up per spec §3 (nullable `wallet_id`, `address text`, `confirmed_at timestamptz`, CHECK named `max_id_destinations_one_target`). Down: `DELETE … WHERE wallet_id IS NULL`, drop CHECK + columns, `SET NOT NULL`.
- [ ] **Step 2: Queries.** `UpsertDestination` sets `address = NULL, confirmed_at = NULL` on conflict. New `UpsertPastedDestination` (`wallet_id = NULL, address = $3, confirmed_at = now()`). `ListDestinations` and `ListPublicDestinations`: `LEFT JOIN wallets w`, `COALESCE(w.address, d.address)::text AS address`, `WHERE d.max_id = $1 AND (d.wallet_id IS NULL OR w.status = 'active')`; `ListDestinations` also returns `d.wallet_id` (nullable). Run `sqlc generate`; fix compile errors from `WalletID` becoming `*uuid.UUID`.
- [ ] **Step 3: Failing unit tests** in `service_test.go`: `TestPasteDestination_RequiresConfirmation` (→ `ErrConfirmationRequired`), `_InvalidAddress` (BTC address on `evm` → `ErrInvalidAddress`), `_UnknownNamespace` (→ `ErrUnsupportedNamespace`), `_Normalizes` (EVM checksummed input → repo receives lowercase), `TestSetDestination_WalletOnPasteOnly` (`bitcoin` + walletId → `ErrUnsupportedNamespace`).
- [ ] **Step 4: Implement** `PasteDestination` (parse ns → `IsPasteable` → confirmed → normalize → repo); repo mirrors `SetDestination` (lock, `ensureNoOpenSale`, active check, upsert, bump, audit `maxid.destination_set` with `{"namespace", "source":"pasted", "confirmed": true, "resolution_version"}`; wallet path adds `"source":"wallet"`). Handler: `destinationRequest{WalletID string \`json:"walletId" binding:"omitempty,uuid"\`; Address string \`json:"address" binding:"max=128"\`; Confirmed bool \`json:"confirmed"\`}` — exactly one of walletId/address else `shared.ErrValidation`. DTO emits `source`, `verified` (= !Pasted), `walletId` (null when pasted). Fix `default.go` for the pointer type.
- [ ] **Step 5:** `go test ./internal/maxids/ ./internal/resolver/ && go build ./...` → PASS.
- [ ] **Step 6: Integration tests** (`//go:build integration`, existing `newEnv` helpers): `TestPaste_ResolveRemove` (paste BTC → `GET /v1/resolve/<h>?chain=bitcoin` returns it → DELETE → 404 NO_DESTINATION); `TestPaste_LinkReplacesAndBack`; `TestPaste_UnlinkWalletKeepsPasted`; `TestPaste_TransferClears`; `TestPaste_AgentCanPaste`; `TestPaste_DefaultFillDoesNotOverwrite`; `TestPaste_OthersAddressStillLinkable`; `TestPaste_CheckRejectsBothAndNeither` (raw INSERTs fail). Verify compile: `go vet -tags integration ./internal/server/` → no errors. Do not run against the hosted DB.
- [ ] **Step 7:** Commit `feat(core): pasted receive addresses on MAX ID destinations`.

### Task 3: Receive-only guards and resolve behaviour

**Files:**
- Modify: `max-core/internal/payments/{service.go,crosschain.go,error.go}`, `internal/maxids/service.go` (settlement), `internal/market/config.go`, `internal/config/config.go`, `cmd/api/main.go`, `internal/resolver/service.go`
- Test: `internal/payments/service_test.go` (or existing test file with a stub resolver), `internal/resolver/service_test.go`, `internal/market/config_test.go`, `internal/maxids/service_test.go`

**Interfaces — Produces:** `payments.ErrReceiveOnlyChain` (`RECEIVE_ONLY_CHAIN`, 422), message via `WithMessage("MAX can't send on " + chain.Name + " yet.")`.

- [ ] **Step 1: Failing tests:** `TestPrepare_ReceiveOnlyOrigin` (chain `bitcoin` → `RECEIVE_ONLY_CHAIN`, message "MAX can't send on Bitcoin yet."); `TestPrepare_ReceiveOnlyDestination` (destinationChain `tron` → same code); `TestResolve_ReceiveOnly` (bitcoin native → address + note "Receive-only on MAX: send from your own wallet."; token asset → `ErrInvalidAsset`); `TestSetSettlement_ReceiveOnly` (`litecoin` → `ErrSettlementChain`); market config with `MARKET_ASSETS=bitcoin:native` → error.
- [ ] **Step 2:** Run → FAIL.
- [ ] **Step 3: Implement:** in `Prepare` right after `chains.Lookup(in.Chain)` and after `destChain` lookup; in `prepareCrossChain` on `dest` (covers settlement fallback); `SetSettlement` condition `|| chain.ReceiveOnly`; market asset parse rejects receive-only; `config.go` RPC env loop and both `cmd/api/main.go` `chains.Supported` loops `continue` on `ReceiveOnly`; resolver `parseAsset` on receive-only namespace accepts only native, and `Resolve` appends the note.
- [ ] **Step 4:** `go test ./... && go vet ./...` → PASS.
- [ ] **Step 5:** Commit `feat(core): refuse money movement on receive-only chains`.

### Task 4: SDK

**Files:** Modify `max-sdk/src/{types.ts,client.ts,errors.ts,index.ts}`; Create `max-sdk/src/address-shape.ts`; Test `max-sdk/test/unit.test.ts`.

**Interfaces — Produces:**
- `Namespace = "evm" | "solana" | "bitcoin" | "litecoin" | "tron"`; `Chain.receiveOnly: boolean`; `Destination { namespace; address; source: "wallet" | "pasted"; verified: boolean; walletId: string | null; updatedAt }`.
- `setDestination(id, namespace, target: { walletId: string } | { address: string; confirmed: true }): Promise<MaxId>` (breaking signature; update all callers in web/app in Tasks 5–6).
- `looksLikeAddress(ns: Namespace, s: string): boolean` — trimmed shape check: evm `^0x[0-9a-fA-F]{40}$`; solana base58 32–44; bitcoin `^(1|3)[base58]{25,34}$` or `^bc1[02-9ac-hj-np-z]{11,71}$` (case-insensitive); litecoin `^[LM3][base58]{26,33}$` or `^ltc1…`; tron `^T[base58]{33}$`.
- Error messages for the three new codes.

- [ ] **Step 1:** Failing tests: `looksLikeAddress` true for one vector per format from Task 1, false for cross-namespace and junk; `setDestination` sends `{address, confirmed:true}` body (use existing fetch-stub pattern in unit tests).
- [ ] **Step 2:** `cd max-sdk && npm test` → FAIL. **Step 3:** implement. **Step 4:** `npm test && npm run build` → PASS; copy `dist` into the e2e web copy later.
- [ ] **Step 5:** Commit `feat(sdk): pasted destinations, receive-only chains, address shape helper`.

### Task 5: Web

**Files:** Modify `max-frontend/src/components/identity/IdCard.tsx`, `src/hooks/queries.ts` (`useSetDestination` input), `src/utils/format.ts` (`namespaceLabel`: EVM, Solana, Bitcoin, Litecoin, Tron), `src/app/[handle]/ProfileView.tsx`, web send page chain list (filter `!c.receiveOnly`), `src/error/messages.ts`; any other `setDestination` caller (`ClaimForm.tsx`, `RenameFlow.tsx`).

- [ ] **Step 1:** IdCard rows for all five namespaces; picker = wallets of that namespace then "Paste an address"; paste panel: input with `looksLikeAddress` live state → confirm view (full address mono, namespace label, EVM warning for `evm`) → checkbox with the exact copy → Save calls `setDestination(id, ns, {address, confirmed: true})`. Current destination shows "Unverified" badge + hint when `source === "pasted"`. Empty-state text uses `namespaceLabel`.
- [ ] **Step 2:** ProfileView: reachable receive-only chains render the chip plus "Receive-only: send from your own wallet"; the Send button stays (it sends on other chains) unless every reachable chain is receive-only, in which case it is hidden.
- [ ] **Step 3:** error messages: `INVALID_ADDRESS` "That address isn't valid for this network.", `CONFIRMATION_REQUIRED` "Confirm you control this address.", `RECEIVE_ONLY_CHAIN` server message.
- [ ] **Step 4:** `cd max-frontend && npx tsc --noEmit && npm run lint` → clean.
- [ ] **Step 5:** Commit `feat(web): paste receive addresses, receive-only chains`.

### Task 6: App

**Files:** Modify `max-app/src/app/(app)/destination.tsx`, `(tabs)/ids.tsx`, `(tabs)/receive.tsx`, `src/hooks/queries.ts` (`useSetDestination`, `useUsableChains` also filters `!c.receiveOnly`), `src/utils/format.ts` (`namespaceLabel`), `src/error/messages.ts`; route param type `ns` widens to `Namespace`; other `setDestination` callers.

- [ ] **Step 1:** destination sheet: wallet options (if any) + "Paste an address" → TextInput with `looksLikeAddress` → confirm block (address, namespace, EVM warning) → checkbox Pressable with the exact copy → Save. ids tab lists five namespaces and shows "Unverified" badge for pasted.
- [ ] **Step 2:** receive tab shows BTC/LTC/Tron destinations with QR and "Receive-only" note.
- [ ] **Step 3:** error messages as Task 5.
- [ ] **Step 4:** `cd max-app && npx tsc --noEmit && npm run lint && npm run test:market` → clean.
- [ ] **Step 5:** Commit `feat(app): paste receive addresses, receive-only chains`.

### Task 7: Hosted migration and end-to-end

- [ ] **Step 1:** Back up `max_id_destinations` from hosted `max` to scratchpad CSV (read-only tool). Run `go run ./cmd/migrate up` from the worktree's `max-core` with the real `.env`; check `version` = 13; check CHECK holds (`SELECT count(*) … WHERE wallet_id IS NULL` = 0). Verify down/up on hosted only if no pasted rows exist yet (`down 1` then `up`), then confirm version 13.
- [ ] **Step 2:** Start the branch API on :8095 (scratch `run-api-main.sh`). Script `e2e/paste.mjs`: new user; paste on all five namespaces (one valid vector each) → each `resolve` returns it (bitcoin/litecoin/tron with the note); owner view shows `source:"pasted", verified:false`; missing `confirmed` → 400 `CONFIRMATION_REQUIRED`; bad checksum → 400 `INVALID_ADDRESS`; second user prepares a Solana payment to the pasted Solana address (prepare succeeds or fails only for `CHAIN_NOT_CONFIGURED`, never `NO_DESTINATION`); prepare on `bitcoin` → 422 `RECEIVE_ONLY_CHAIN`; link EVM wallet on a name with pasted EVM keeps the paste on non-default ID and default fill doesn't overwrite; another user can still link-login with an address someone pasted.
- [ ] **Step 3:** Browser on :3200 against :8095: paste flow on dashboard (validation, EVM warning, checkbox gating, badge), public profile receive-only line.
- [ ] **Step 4:** Delete the e2e accounts with the guarded cleanup tool (ids from the run, created today). Stop servers.
- [ ] **Step 5:** Report; merge to `main` only on the user's say-so.
