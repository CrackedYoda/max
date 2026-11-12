**MAX security review — 11 November 2026**

Reviewed checkout: `fb2d77c09e5d42c17c8c7e98cfe38dcfb4feebd2`. Scope: Go API, database queries/migrations, web wallet, mobile wallet, SDK, MCP, and dependency manifests. Application source was not changed. This report and isolated reproductions are the deliverables.

**Assessment:** address the payment signing/retry and agent-budget findings before relying on this system for unattended real-money payments. Five targeted checks reproduced defects locally. Other findings below are supported by code inspection and identify where database, device, or deployment validation remains necessary. No live transactions or production attacks were performed.

| ID | Severity | Finding | Evidence |
| --- | --- | --- | --- |
| S01 | High | One agent approval can trigger multiple signatures/broadcasts | Local concurrent-call reproduction |
| S02 | High | Untrusted transaction submission can release an agent's spend reservation | Static end-to-end code path |
| S03 | High | Web retries can send funds again after an ambiguous wallet response | Static UI/signing path |
| S04 | Medium | Idempotency replay bypasses payment ownership checks | Local service reproduction |
| S05 | Medium | Public holdings cache grows without eviction | Local service reproduction |
| S06 | Medium | JSON request bodies have no application size limit | Local binder reproduction |
| S07 | Medium | Concurrent wallet removals can remove the last login wallet | Static transaction analysis |
| S08 | Medium | Concurrent claims bypass the per-account ID limit | Static transaction analysis |
| S09 | Medium | Mobile lock can be undone by a late unlock completion | Static async lifecycle analysis |
| S10 | Medium | Payment submission deadline depends on the background sweeper | Local service reproduction |
| S11 | Medium | Cross-chain replay does not compare all requested terms | Static comparison analysis |
| S12 | Conditional; up to High | Vulnerable dependency versions remain in the trees | npm audit and govulncheck |

**S01 — High: signing is not idempotent.**

[PolicySigner.execute](/Users/yoda/Desktop/max/max-sdk/src/signer.ts:31) obtains verified claims, calls the signing/broadcast callback, and only then submits the hash. There is no execution lock, durable attempt record, or reuse of previously signed bytes. Two concurrent calls both fetch an approved intent before either submits. Both can broadcast; the backend's one-hash constraint rejects the second hash only after funds have left. Different EVM nonces or Solana blockhashes produce distinct payments. A timeout before successful API submission also leaves a retry opportunity.

The saved `signer-concurrency.mjs` reproduces two signing callbacks for one payment using a fake client; it does not claim two real chain transactions were sent. The backend only issues claims while signable, but that does not close this concurrency window.

Fix: make the trusted signer durably single-use per payment, reserve the execution before signing, persist the signed bytes/hash before broadcasting, and retry the same bytes. Coordinate across processes, not just one instance. Bind nonce/blockhash and network identity where practical. Test concurrent execution, crash after signing, broadcast timeout, and API submit failure.

**S02 — High: spend budget can be released while an authorized payment still exists on-chain.**

[Spend accounting](/Users/yoda/Desktop/max/max-core/db/queries/agents.sql:60) excludes payments marked `FAILED` or `EXPIRED`. [Submit](/Users/yoda/Desktop/max/max-core/internal/payments/service.go:291) accepts a syntactically valid hash before checking its relationship to the payment. [Observation](/Users/yoda/Desktop/max/max-core/internal/payments/service.go:482) marks reverted or mismatching transactions failed. Agent credentials with `payments:submit` can invoke this directly.

An agent with that scope can arrange for a real approved payment to be signed/broadcast, then win the submit race with an unrelated transaction hash. The worker marks the unrelated transaction failed, and the reservation disappears even though the genuine payment can settle. The agent can then request additional approvals inside the apparent budget. Expiring an intent after a signed transaction was broadcast but never recorded creates a related accounting gap. This is a code-derived attack sequence, not a live-chain demonstration.

Fix: bind an authenticated trusted signer attempt to the intent and its exact hash. A mismatch from an untrusted caller must not prove that authorized funds were never spent. Keep uncertain reservations until reconciliation conclusively establishes the outcome; use manual review for contradictory evidence. Test a legitimate payment racing an unrelated submitted hash, and delayed recording across expiry.

**S03 — High: ambiguous browser-wallet failures allow another transfer.**

[Browser transfer adapter](/Users/yoda/Desktop/max/max-frontend/src/services/transfer.ts:42) turns non-rejection send failures into a generic error. [SendFlow](/Users/yoda/Desktop/max/max-frontend/src/app/send/SendFlow.tsx:143) returns to review when this happens, keeping no transaction hash if the wallet never returned it. The next attempt calls `signAndSend` again. The [rename fee flow](/Users/yoda/Desktop/max/max-frontend/src/app/rename/[id]/RenameFlow.tsx:114) has the same pattern.

If the wallet/RPC broadcasts successfully and then loses the response, a retry can sign a fresh transaction and pay twice. Reusing the API idempotency key only reuses the server intent; it cannot deduplicate blockchain transfers. The marketplace has more recovery logic, but these ordinary transfer paths do not share it.

Fix: distinguish explicit user rejection from unknown broadcast outcome. Hold ambiguous attempts in a recovery state and reconcile before allowing fresh signing. Persist attempt identity across refreshes. Where the wallet supports it, sign first, persist hash/bytes, then broadcast. Test “broadcast succeeded, response lost” for both EVM and Solana.

**S04 — Medium: payment replay bypasses ownership.**

[replay](/Users/yoda/Desktop/max/max-core/internal/payments/service.go:217) validates some payment terms but never checks `Caller` against `SenderAccountID`. The lookup is keyed only by sender address and idempotency key. A caller who knows those values and the original terms can replay a signed-in account's request anonymously and receive payment details and signing instructions, although the ordinary GET endpoint denies access.

The targeted service test confirmed this exact discrepancy. Exploitation requires knowing or guessing the idempotency key and matching terms; random UUID keys reduce exposure but are not an ownership check. No direct fund theft was demonstrated.

Fix: scope idempotency to principal and operation, apply the same ownership rules on replay, and preserve intentionally anonymous behavior only for anonymous intents. Test anonymous, different-account, and different-operation replays.

**S05 — Medium: holdings cache never evicts entries.**

[Holdings List](/Users/yoda/Desktop/max/max-core/internal/holdings/holdings.go:78) checks a 20-second TTL for freshness but retains every address key forever. `/v1/holdings` is public. Repeated valid, distinct addresses consume RPC/indexer quota and permanently grow process memory; the per-IP request limiter slows this but does not bound retention.

The local test queried 1,000 addresses, advanced the clock by 24 hours, and queried another: all 1,001 entries remained. Fix with a size-bounded cache, actual expiry eviction, and single-flight fetches. Test high-cardinality traffic and simultaneous cache misses.

**S06 — Medium: unbounded JSON bodies permit resource exhaustion.**

[BindJSON](/Users/yoda/Desktop/max/max-core/internal/shared/bind.go:14) invokes the JSON decoder without `http.MaxBytesReader`; the server config caps headers and time but not body bytes. Field-length validation happens after parsing/allocation. An oversized body on a public challenge/prepare route can consume substantial memory before rejection. An external proxy may mitigate this, but no such limit was established in this review.

A local binder test accepted an 8 MiB string. This was a bounded reproduction, not an OOM attempt. Add a global body limit before decoding, return 413, and set narrower endpoint-specific limits where useful. Cover chunked requests as well as Content-Length. This follows [OWASP REST request-size guidance](https://cheatsheetseries.owasp.org/cheatsheets/REST_Security_Cheat_Sheet.html).

**S07 — Medium: last-wallet protection is vulnerable to write skew.**

[Remove](/Users/yoda/Desktop/max/max-core/internal/wallets/repository.go:47) locks the wallet being removed, then counts all active wallets. Two requests removing different wallets lock different rows; both can count two, pass the last-wallet check, and remove their own row. There is no common account lock in this path. Accounts with no overlapping destination-row updates provide a straightforward interleaving.

This can leave the account with no login wallet after existing sessions expire. Re-proving a removed wallet may create a new account rather than recover the original identity. This finding was not reproduced against PostgreSQL because a disposable database was unavailable. [PostgreSQL's Read Committed behavior](https://www.postgresql.org/docs/current/transaction-iso.html) permits the described interleaving.

Fix: lock the account before counting/removing, use the same lock for related wallet mutations, and add a two-transaction integration test asserting at least one active wallet remains.

**S08 — Medium: ID limit check and insertion are not atomic.**

[Claim](/Users/yoda/Desktop/max/max-core/internal/maxids/service.go:99) counts IDs before entering the insertion transaction. [CreateInTx](/Users/yoda/Desktop/max/max-core/internal/maxids/repository.go:131) locks the requested name, not the owner. Concurrent requests for different names can all observe a count below ten, then all create names. The claim route also lacks a specific write-rate limiter.

Impact: account quotas and name-allocation controls can be bypassed, enabling excess registration and resource use. Static finding; PostgreSQL reproduction remains outstanding. Fix: account-level serialization with the count inside the transaction. Explicitly define whether purchases/reserved claims count toward the same quota. Test many simultaneous distinct-name claims at the boundary.

**S09 — Medium: late mobile unlock completion can undo a background lock.**

[afterUnlock](/Users/yoda/Desktop/max/max-app/src/services/session.ts:42) awaits vault loading then unconditionally sets the state to unlocked. [lock](/Users/yoda/Desktop/max/max-app/src/services/session.ts:50) returns immediately unless already unlocked. If backgrounding occurs while passcode verification/key derivation is pending, the lock call does not invalidate the operation; its later completion loads keys and marks the backgrounded app unlocked. The app-state hook only locks when the background event occurs, not when this late result arrives.

This is a local device/lifecycle risk, not a remote authentication bypass. Device reproduction is still needed. Fix: invalidate every pending unlock on background/lock, use a generation token captured before verification, and check foreground state before committing. Ensure vault.load itself cannot retain derived keys after cancellation. Test backgrounding during verification, storage reads, and key derivation.

**S10 — Medium: submission expiry depends on sweeper timing.**

[Submit's status switch](/Users/yoda/Desktop/max/max-core/internal/payments/service.go:313) checks `lateUntil` only for rows already marked EXPIRED. A row still AWAITING_SENDER_SIGNATURE or QUOTED is accepted regardless of elapsed time. The local test submitted an unswept intent 24 hours after expiry successfully.

An outage or disabled worker changes API authorization behavior and can admit stale cross-chain deposits after the provider quote deadline. Fix: enforce the absolute deadline independently of the row's status, then retain appropriate reconciliation for money already sent. Test all signable statuses before/after both grace and quote deadlines with the sweeper stopped.

**S11 — Medium: cross-chain idempotency omits destination asset and slippage.**

[replay](/Users/yoda/Desktop/max/max-core/internal/payments/service.go:227) compares source terms, destination chain in some cases, and amount mode, but does not compare `DestinationAsset` or `SlippageBps`. A retry reusing a key with a different requested output asset can silently return the previous quote instead of conflict. The same comparison also does not enforce a new resolution version.

A caller must reuse a key, so this is primarily an integrity/retry edge case, rather than an arbitrary remote rewrite of an existing payment. Fix: persist a canonical fingerprint of every economic and routing input, including effective defaults and operation purpose. Reject semantic differences. Test destination token, slippage, exact-input/output, and resolution changes independently.

**S12 — dependency findings and reachability limits.**

Live registry scans completed successfully after the initial sandbox DNS failure. Counts below represent vulnerable package entries, including affected parent packages; they are not counts of independent exploitable vulnerabilities. Trees overlap and must not be summed as unique issues.

| Project | High | Moderate | Low | Total |
| --- | ---: | ---: | ---: | ---: |
| max-app | 6 | 18 | 1 | 25 |
| max-frontend | 3 | 13 | 0 | 16 |
| max-sdk | 3 | 6 | 0 | 9 |
| max-mcp | 0 | 0 | 0 | 0 |

The Solana dependency tree includes `bigint-buffer` with [GHSA-3gc7-fjrx-p6mg](https://github.com/advisories/GHSA-3gc7-fjrx-p6mg), a native conversion buffer-overflow advisory. Browser/mobile JS fallback behavior and exact attacker-controlled input reachability need separate validation; this report does not claim a reachable browser native-memory exploit. npm proposes a disruptive SPL-token downgrade for this tree—do not apply `audit fix --force` blindly. Use a maintained compatible dependency path and rerun signing/serialization tests.

The mobile tree also flags `node-forge` and Expo's nested `zod`; the direct application Zod version is newer than the affected range reported for that nested package. Distinguish build/CLI exposure from shipped app exposure. Raw advisory URLs, paths, ranges, and suggested fixes are preserved in the JSON evidence.

`govulncheck` reported two symbol-level findings:

- [GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809): `crypto/x509` in Go 1.27.2; fixed in Go 1.27.3. The advisory concerns distrust of compromised intermediates, with exposure depending on trust-store/platform circumstances. Upgrade the toolchain and rebuild the binary.
- [GO-2026-5676](https://pkg.go.dev/vuln/GO-2026-5676): `quic-go` 0.59.0; fixed in 0.59.1. This is HTTP/3 QPACK memory exhaustion. The inspected server uses `http.Server.ListenAndServe`, not an explicit HTTP/3 listener; scanner call-graph output is not proof this endpoint is remotely exploitable in this deployment. Upgrade the dependency and confirm HTTP/3 usage.

The scanner also reported three module-only findings without apparent calls. Full scanner output is saved in evidence.

**Additional edge cases and hardening work**

- **Mobile pending sends:** lock clears in-memory send/rename drafts, including signed transaction recovery state. A crash/background during broadcast or API recording can lose the known hash. Persist non-secret payment attempt metadata and signed transaction recovery securely before broadcast, and restore it after unlock. Test this alongside S03.
- **Secret screens on lock:** screens remain mounted; recovery phrase component state and the one-time step-up gate are not reset on generation changes. Clearing vault references does not clear those copies. Explicitly erase secrets and revoke the gate on lock, blur, and unmount. Also test rendering export-key after vault unload.
- **Secure deletion:** `safeDelete` suppresses every storage deletion error. removeWallet can report success even if keys remain. Distinguish missing-item errors from actual storage failure, verify deletion, and surface partial failure before presenting a clean state.
- **Local vault threat boundary:** vault entries use OS secure storage without `requireAuthentication`; biometric authentication protects a separate sentinel. The app passcode gates JS access rather than cryptographically encrypting the vault. Assess this against the intended compromised-device/in-process-code threat model. The SHA-256 PIN verifier should not be represented as offline brute-force resistance.
- **Agent policy races:** policy and status are loaded before the budget lock, so a concurrent restrictive policy update/pause may not invalidate an in-flight decision. Existing signed authorizations also remain usable until expiry. Define revocation guarantees; reload authoritative policy under the shared lock and enforce stronger signer-side freshness if immediate revocation is required.
- **Budget timing:** transfer rolling spend uses authorization creation time, whereas human approval can occur later. Test pending approvals crossing the 24-hour boundary and long configured intent TTLs. Multi-step approval/release writes should also recover after a crash between commits.
- **Solana network identity:** EVM RPC endpoints are checked against chain ID; the Solana path has no equivalent genesis-hash check. A misconfigured devnet endpoint must not verify mainnet settlement. Test network mismatch at startup and in settlement adapters.
- **Transaction verification:** test fee-on-transfer/rebasing tokens, Token-2022 extensions, unusual authority/delegate relationships, missing block timestamps, and reorgs. Matching an instruction/log does not always prove net receipt of the requested amount.
- **Ownership-history privacy:** ListPaymentsReceived joins the current MAX-ID owner. A buyer of a name may see payments sent to its previous owner. Decide whether history should follow identity ownership or the historical recipient, then enforce that policy explicitly.
- **Proxy/rate limits:** trusted proxies are disabled, so behind a reverse proxy all clients may share its IP bucket. Multi-instance process-local limits can also multiply the allowed rate. Configure narrowly trusted proxies and shared account/key/IP controls; verify behavior under NAT and multiple replicas.
- **Retention/abuse:** no cleanup of expired wallet challenges/sessions was found in the inspected query/worker paths. Test sustained anonymous challenge creation and define retention. Authenticated agent/credential creation also needs quotas.
- **Web headers and transport:** Next config does not define CSP/frame restrictions; API headers do not automatically protect the web origin. Inspect the actual CDN responses and add a tested policy. Validate HTTPS-only production API/RPC configuration and client cache handling for private endpoints.
- **MCP:** tools do not sign payments, and user-written profile text is marked untrusted. Preserve that separation; test malicious profile/token/provider strings and time-window idempotency boundaries. Model-facing warnings alone are not an authorization boundary.

**Controls that are already present**

Wallet challenges are consumed with a conditional single-use database update; sessions and agent credentials are checked for expiry/revocation; agent-owned wallets cannot obtain unrestricted human sessions. OAuth verification binds its state to the initiating account/session and uses PKCE. API CORS rejects disallowed origins for state-changing requests, and session cookies are HttpOnly/SameSite=Lax. Payment snapshots have database immutability guards and a unique chain/hash index. Agent budget evaluation uses a shared advisory lock, and marketplace settlement includes exact instruction/memo checks. These controls reduce several common risks, but do not prevent the distinct replay, concurrency, and recovery defects above.

**Checks completed and limits**

- `go test ./...`: passed; several results cached. PostgreSQL integration suites are behind `integration` build tags and were not executed by this command.
- `go vet ./...`: passed.
- `go test -race ./internal/agents ./internal/payments ./internal/market/... ./internal/auth ./internal/maxids`: passed.
- SDK tests: 96 passed, 1 end-to-end test skipped.
- MCP tests: 24 passed, 1 end-to-end test skipped.
- Mobile `npm run lint` and `npm run typecheck`: passed. Web `npm run lint`: passed.
- Four temporary Go reproduction tests and one standalone SDK reproduction passed, confirming the vulnerable behavior described above. They are saved outside the normal test suites; a passing reproduction is not a security fix.
- npm audit: all four Node projects scanned. Official Go scanner completed with findings.
- A narrow tracked-file scan found no matches for private-key PEM headers, AWS access-key IDs, GitHub classic token patterns, or Stripe live-key patterns. Actual `.env` files are not tracked; example env files are. This is not a full secret-history/entropy scan, and secret values were not printed.

Docker was not running and no disposable PostgreSQL test environment was used. The integration harness truncates tables, so it must run only on a dedicated throwaway database. No production ingress, TLS deployment, database roles/backups, real wallet provider, physical mobile device, live chain settlement, or deployed bundle was assessed. Database concurrency and device lifecycle findings remain code-supported rather than integration-proven.

**Remediation order:** first S01–S03 and durable client recovery; next account-bound replay, request/cache limits, and atomic account invariants; then mobile lifecycle cancellation, strict expiry/replay fingerprints, and dependency upgrades. Turn each reproduction into a regression that asserts the safe behavior, and add PostgreSQL/device tests for findings that unit tests cannot exercise.
