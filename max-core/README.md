# max-core

Go API for MAX: wallet-proof auth, MAX ID registry, per-chain destinations and
public resolution. Follows `../BACKEND_BLUEPRINT.md`.

## Run locally

```bash
cp .env.example .env
make up          # postgres :5433, redis :6380
make migrate
make run         # http://localhost:8080
```

Tests:

```bash
make test                                                   # unit
TEST_DATABASE_URL=postgres://max:max@localhost:5433/max_test?sslmode=disable \
  go test -tags integration ./internal/server/              # end-to-end against a real DB
go test -tags live -run Live ./internal/payments/           # gateway against public testnet RPCs
go test -tags live -run Live ./internal/routing/nearintents/ # real (uncommitted) 1Click quote
```

## Conventions

- Success: `{ "data": ... }` · Error: `{ "error": { "code": "STRING", "message": "STRING" } }`
- Auth: HttpOnly `max_session` cookie (SameSite=Lax), or `Authorization: Bearer <token>`.
  Browsers must send `credentials: "include"`. Origin must be in `CORS_ORIGINS`.
- MAX IDs accept `max.yoda`, `MAX.Yoda` or `yoda`; canonical form is `yoda`, display is `max.yoda`.
  3–32 chars, `a-z 0-9 -`, no leading/trailing/double hyphens, ASCII only.
- Namespaces: `evm`, `solana` (bitcoin reserved, not linkable yet). One EVM wallet covers every EVM chain.
- Assets are identified by `native` or contract/mint address — never by symbol.

## Endpoints

### Public

| Method | Path | Notes |
|---|---|---|
| GET | `/healthz` | DB readiness |
| GET | `/v1/chains` | Supported chains `{key,name,namespace,evmChainId,testnet,nativeCoin}` |
| GET | `/v1/ids/availability?name=max.yoda` | `{name,handle,available,reason?,detail?,claimPolicy?}`; reason ∈ `taken`·`reserved`·`invalid` |
| GET | `/v1/profiles/:maxId` | Public profile + `chains[{chain,reachable}]` |
| GET | `/v1/resolve/:maxId?chain=solana&asset=<mint\|native>` | `{maxId,handle,type,displayName,chain,asset,address,resolutionVersion,resolvedAt,notes[]}`; `NO_COMPATIBLE_WALLET` if no wallet on that chain (never guesses) |

### Wallet proof (login + linking)

1. `POST /v1/wallets/challenge` `{purpose:"login"|"link", namespace:"evm"|"solana", address}` →
   `{nonce, message, expiresAt}`. `link` requires a session.
2. Have the wallet sign `message` exactly as returned:
   - EVM: `personal_sign` / wagmi `signMessage` → `0x…` 65-byte hex
   - Solana: Phantom `signMessage(new TextEncoder().encode(message))` → **base58** of the signature bytes
3. `POST /v1/wallets/verify` `{nonce, signature}` →
   `{purpose, account, wallet, accountCreated, sessionExpiresAt?}`. Login sets the session cookie.

Each nonce is single-use (a failed attempt burns it) and expires after `AUTH_CHALLENGE_TTL`.

### Authenticated

| Method | Path | Body / notes |
|---|---|---|
| GET | `/v1/auth/me` | Account |
| POST | `/v1/auth/logout` | 204, clears cookie |
| GET | `/v1/wallets` | Linked wallets |
| DELETE | `/v1/wallets/:id` | Clears destinations using it; `LAST_WALLET` if it is the only one |
| POST | `/v1/ids` | `{name, type?: "human"\|"agent", displayName?}` — auto-sets first wallet per namespace as destination |
| GET | `/v1/ids` | My MAX IDs with destinations |
| GET | `/v1/ids/:id` | 404 if not mine |
| PATCH | `/v1/ids/:id` | `{displayName, description, avatarUrl(https)}` |
| PUT | `/v1/ids/:id/destinations/:namespace` | `{walletId}` — wallet must be mine and in that namespace |
| DELETE | `/v1/ids/:id/destinations/:namespace` | |

### Transfers (direct, same-chain)

Signing in is optional for senders. If a signed-in sender prepares a transfer, only that account can see or submit it.

1. `POST /v1/transfers/prepare`
   `{recipient:"max.yoda", chain:"base", asset:"native"|<contract/mint>, amount:"25.5", senderAddress, idempotencyKey, resolutionVersion?}`
   → 201, or 200 with `replayed:true` when an earlier prepare had the same key and terms. The response carries the immutable recipient snapshot, the asset (`decimals`, `verified`, `symbol`), `amount{display,baseUnits}`, `warnings[]`, `expiresAt` (15 min), and `instructions`:
   - `evm: {chainId, from, to, value(hex wei), data}`. Pass it straight to `sendTransaction`. For ERC-20, `to` is the token contract and `data` is `transfer(recipient, amount)`.
   - `solana: {kind:"system_transfer", from, to, lamports}`, or
     `{kind:"spl_transfer_checked", mint, tokenProgram, decimals, amount, sourceTokenAccount, destinationTokenAccount, destinationTokenAccountExists}`.
     Always include `createAssociatedTokenAccountIdempotent` before `transferChecked`.
   - Pass the `resolutionVersion` from the resolve call you showed the user. If the recipient changed wallets since then, prepare fails with `RESOLUTION_STALE`.
2. The sender signs and broadcasts the transaction with their own wallet.
3. `POST /v1/transfers/:id/submit` `{txHash}` (EVM `0x…64 hex` or Solana base58 signature). Idempotent for the same hash.
4. Poll `GET /v1/transfers/:id`. Response includes `status`, `statusReason`, `confirmations/requiredConfirmations`, `explorerUrl` and `events[]`.

The background worker checks the transaction on-chain before marking it complete:
- EVM native: sender, recipient and value must match exactly.
- ERC-20: an exact `Transfer(sender → recipient, amount)` log from the expected token contract.
- Solana: a system or SPL transfer to the recipient's derived ATA, signed by the sender, for the exact amount.

In every case the transaction must also be newer than the intent. EVM transfers complete after the chain's confirmation depth; Solana transfers complete at `finalized`.

States: `AWAITING_SENDER_SIGNATURE → SUBMITTED → CONFIRMING → COMPLETED`, plus `EXPIRED`, `FAILED` (reverted, wrong transfer, or a Solana tx that never landed) and `MANUAL_REVIEW` (EVM tx unseen after 30 min). A tx signed just after expiry can still be submitted for up to 1 h. Each tx hash can settle only one payment.

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/transfers?direction=sent\|received` | Auth. `received` = transfers to my MAX IDs |

Payments on a chain need `RPC_URL_<CHAIN>` set (see `.env.example`); otherwise prepare returns `CHAIN_NOT_CONFIGURED`. At first use, the gateway checks that each EVM RPC's chain id matches the chain it's configured for.

### Cross-chain (NEAR Intents 1Click)

Mainnet only. Routes exist only for assets 1Click lists (ETH, SOL, POL, USDC, USDT and some memecoins such as WIF). Anything else gets `ROUTE_UNAVAILABLE`; MAX never implies every coin converts.

**When a route is used:**
- The sender passes `destinationChain` (+ `destinationAsset`) that differs from the source, or
- The recipient has no wallet on the sender's chain but has enabled a settlement preference.

Otherwise the payment is direct.

**Prepare** takes extra optional fields: `destinationChain`, `destinationAsset`, `amountType: "input"|"output"` (output = the recipient receives exactly `amount`) and `slippageBps` (default 100, max 300). The response:
- `status` is `QUOTED`, with `route: "cross_chain"`.
- `crossChain` contains `depositAddress`, `refundTo`, `amountIn`, `expectedAmountOut`, `minAmountOut`, the USD values, `timeEstimateSeconds` and `quoteDeadline`.
- `instructions` pay the provider's deposit address exactly `amountIn`. Sign and submit exactly like a direct transfer.

**Safety checks before the sender sees a quote:**
- The provider must echo back the exact assets, amount, recipient and refund address MAX sent.
- `minAmountOut` must respect the agreed slippage.
- The deadline must outlive the intent.
- Memo deposits are refused.
- Source-token decimals must match on-chain; destination tokens are checked against the verified list.
- The USD cap applies (`CROSS_CHAIN_MAX_USD`).
- The quote and full signed provider response are stored and locked by a DB trigger.

**Settlement is two-phase:**
1. MAX verifies the deposit on the origin chain itself, then marks `deposit_confirmed`.
2. MAX follows 1Click until `SUCCESS`, which completes the payment only if the delivered amount is at least `minAmountOut`, or `REFUNDED`.

`INCOMPLETE_DEPOSIT` → `REFUND_PENDING`. A provider failure, delivery below the minimum, or a provider silent for 2h past the refund deadline → `MANUAL_REVIEW`. `CROSS_CHAIN_ENABLED=false` stops new quotes while in-flight routes keep being tracked.

| Method | Path | Body / notes |
|---|---|---|
| PUT | `/v1/ids/:id/settlement` | `{chain, asset: "native"\|<addr>, enabled?}` — mainnet only; needs a destination wallet on that chain |
| DELETE | `/v1/ids/:id/settlement` | |

The public profile (`GET /v1/profiles/:maxId`) includes `settlement` when it is enabled.

### AI agents

Model: an agent is its own `agent` account with a human **administrator**. Agents never get browser sessions (wallet login for an agent's wallet returns `AGENT_LOGIN_NOT_ALLOWED`). They authenticate with scoped API keys: `Authorization: Bearer maxk_<prefix>_<secret>` or `X-API-Key`. Keys are HMAC-hashed at rest, shown once, and can expire, rotate or be revoked instantly. Pausing an agent cuts off all of its keys.

**Administrator endpoints** (signed-in human only; another admin's agent returns 404):

| Method | Path | Body / notes |
|---|---|---|
| POST | `/v1/agents` | `{name, description}`. The default policy delegates nothing, so every transfer needs approval |
| GET | `/v1/agents` · `/v1/agents/:agentId` | Detail includes policy, credentials (no secrets), wallets and IDs |
| PATCH | `/v1/agents/:agentId` | `{name?, description?, status: "active"\|"paused"}` |
| PUT | `/v1/agents/:agentId/policy` | `{limits:[{chain, asset:"native"\|<verified token>, maxPerTransfer:"0.2", maxPer24h:"0.5"}], allowedRecipients:["max.yoda"], allowAnyRecipient}` |
| POST | `/v1/agents/:agentId/credentials` | `{name, scopes[], expiresInDays?}` → `secret` (shown once) |
| POST | `/v1/agents/:agentId/credentials/:credentialId/rotate` | New secret; the old key dies atomically |
| DELETE | `/v1/agents/:agentId/credentials/:credentialId` | Revoke |
| POST | `/v1/agents/:agentId/wallets/challenge` · `/verify` | Link the agent's receiving wallet: the admin's browser connects the agent's wallet and signs |
| POST | `/v1/agents/:agentId/ids` | `{name}` → agent-type MAX ID (e.g. `max.researchbot`) owned by the agent |
| GET | `/v1/agents/:agentId/transfers?decision=pending_approval` | |
| POST | `/v1/agent-approvals/:paymentId/approve` · `/deny` | `{reason?}` |

**Scopes:** `payments:propose`, `payments:read`, `payments:submit`, `authorizations:read` (for the signer), `wallets:link`, `ids:write`. A key with `ids:write` can only create agent-type IDs.

**Agent runtime endpoints** (API key only):

| Method | Path | Scope |
|---|---|---|
| GET | `/v1/agent/me` | — |
| POST | `/v1/agent/transfers` | `payments:propose`. Same body as prepare, plus `instruction` (audit only) |
| GET | `/v1/agent/transfers/:id` | `payments:read` |
| GET | `/v1/agent/transfers/:id/authorization` | `authorizations:read` |
| POST | `/v1/transfers/:id/submit` | `payments:submit` |
| GET | `/v1/agent-authorizations/keys` | public: MAX's ed25519 verification key |

**How a proposal is decided** — deterministically, from the resolved intent and never from the `instruction` text:
- **Denied:** the agent is paused, or the sender is not one of its verified wallets.
- **`pending_approval`:** the asset has no delegated limit, the transfer is over `maxPerTransfer`, the rolling 24h spend would pass `maxPer24h`, or the recipient is not on the allowlist. The intent is held in `CREATED` with no signing instructions until the administrator approves or denies it.
- **`auto_approved`:** everything else. The response includes signing instructions and a `signedAuthorization`.

Budget checks run under a per-agent database lock, so concurrent proposals cannot overspend. Expired, failed and refunded payments don't count toward the budget. The generic `/v1/transfers/prepare` refuses agent keys.

**Signer contract:** MAX never signs transactions. The agent's signer (a session key, smart-account policy, or dedicated signing service, never the LLM) must check the `signedAuthorization` before signing:
```ts
import { ed25519 } from "@noble/curves/ed25519";
const DOMAIN = new TextEncoder().encode("max-agent-authorization/v1\n");
const payload = Uint8Array.from(atob(sa.payload), c => c.charCodeAt(0));   // exact signed bytes
const msg = new Uint8Array([...DOMAIN, ...payload]);
if (!ed25519.verify(b64(sa.signature), msg, maxPublicKey)) throw new Error("not authorized by MAX");
const claims = JSON.parse(new TextDecoder().decode(payload));
// then: claims.expiresAt in the future, and the tx you build matches claims.instructions exactly
```
Set `AGENT_AUTHZ_SIGNING_SEED` (`openssl rand -base64 32`); production refuses to boot without it.

### Paid renames (72h cooldown)

A rename costs a fee paid to MAX's treasury. The fee is an ordinary payment intent (`purpose: "rename_fee"`), verified on-chain by the payments worker. The rename is applied only once that payment is `COMPLETED`.

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/ids/:id/rename` | `{eligible, reason?, renameAvailableAt, cooldownHours: 72, fees:[{chain, asset, symbol, amount}], pending?}` |
| POST | `/v1/ids/:id/rename` | `{newName, feeChain, feeAsset, senderAddress, idempotencyKey}` → `{request, feePayment, instructions}`. Sign, then `POST /v1/transfers/:feePaymentId/submit` |
| GET | `/v1/ids/:id/rename/:requestId` | `awaiting_payment` → `completed` \| `failed` \| `expired` |
| GET | `/v1/ids/:id/history` | Previous names, each with its fee payment id |

Rename endpoints need a signed-in owner; agent keys are refused, because a rename fee would bypass agent spend policy.

**Rules:**
- **Reservation:** requesting a rename reserves the new name; claims see it as `pending_rename`. An ID can have only one pending rename.
- **Fee lifecycle:** the fee intent is created on hold and released only after the reservation succeeds, so a fee for a rename that couldn't be reserved can never be paid.
- **Finalize, in one transaction:** swap the name, quarantine the old name (`RENAME_QUARANTINE`, default 30 days; other users get `MAX_ID_QUARANTINED`), write history, bump `resolutionVersion`, and set `renameAvailableAt = now + 72h`.
- **Failure:** a fee that fails, is refunded, or expires past the 1h late-submit grace releases the name without consuming the cooldown.
- **Locking:** claims, reservations and finalizes take one per-name lock, so a claim and a rename can't both win the same name.
- **No redirects:** in-flight payments keep their locked recipient snapshot. Senders holding an older `resolutionVersion` get `RESOLUTION_STALE`, and the old handle stops resolving immediately.

Configure with `RENAME_FEES`, `RENAME_TREASURY_EVM`, `RENAME_TREASURY_SOLANA` and `RENAME_QUARANTINE` (see `.env.example`). Only native coins and verified tokens are accepted; a misconfigured fee refuses to boot.

### Reserved names via X verification

Reserved names have a `claimPolicy`, which availability also returns, with `xHandle`:
- `blocked`: never claimable.
- `x_oauth`: claimable by whoever proves control of the matching X account.
- `manual_review`: X proof, then an administrator decides.

OAuth proves control of the X account **at verification time only**; it is not a trademark right.

1. `POST /v1/verifications/x/start` (signed-in session) → `{authorizeUrl}`. Send the browser there.
2. X redirects to `GET /v1/verifications/x/callback`. The callback then redirects to `X_RESULT_REDIRECT?x=verified&username=…&renewed=N`, or to `?x=error&code=…`.

Native apps can't send their Bearer token from the OAuth browser sheet, so they use a two-step variant. Start with `POST /v1/verifications/x/start {"client":"native"}`. The callback then completes nothing: it forwards `state`, `code` and `error` to `X_NATIVE_RESULT_REDIRECT` (default `max://verify/x`). The app finishes with `POST /v1/verifications/x/complete {"state","code","error"}` from the session that started the flow. That call returns `{verification, renewedClaims}`. Native states carry an `n.` prefix, which is covered by the stored hash. Web flows can't be completed through `/complete`, and native flows never complete on the callback.
3. `POST /v1/reserved-claims` `{name, displayName?}` → `201 {status:"active"}`, or `202 {status:"pending_review"}` for `manual_review`.

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/verifications` | My X verifications |
| DELETE | `/v1/verifications/:id` | Unlink |
| GET | `/v1/reserved-claims` | My claims, with `verifiedUntil` |
| GET | `/v1/admin/reserved-claims?status=pending_review` | Admin (`ADMIN_ACCOUNT_IDS`); others get 404 |
| POST | `/v1/admin/reserved-claims/:id/approve` · `/deny` · `/revoke` | `{note}`. Revoke locks the ID (disputes, trademark, compromised X account) |
| POST | `/v1/admin/verifications/:id/revoke` | `{note}` |

**Rules:**
- **Flow security:** Authorization Code + PKCE (S256). The state is single-use, hashed at rest and expires after 10 minutes. The callback must arrive **in the same browser session that started the flow**, or it fails with `OAUTH_SESSION_MISMATCH`; this stops an attacker from getting a victim who is signed in to X as @brand to approve the attacker's flow. MAX requests only `tweet.read users.read`, uses the access token once, revokes it, and never stores it.
- **Claiming:**
  - The verified handle must match the reserved `x_handle` (case-insensitive) and be at most `X_CLAIM_FRESHNESS` old (default 15 min).
  - The claim pins the X **user id**, so a handle that later changes hands can't take over.
  - An X account backs at most one MAX account at a time (`X_ACCOUNT_LINKED_ELSEWHERE`).
- **Re-verification:** a claim is valid for `X_REVERIFY_INTERVAL` (180 days). After a further `X_REVERIFY_GRACE` (14 days), the claim lapses and the ID is locked: it stops resolving and can't be configured. Verifying again with the same X user id renews every claim it backs and unlocks them.

Set `X_CLIENT_ID`, `X_CLIENT_SECRET`, `X_REDIRECT_URI` (register it on your X app) and `X_RESULT_REDIRECT`. Set `X_NATIVE_RESULT_REDIRECT` to change the app's deep link.

### Error codes worth handling in UI

`MAX_ID_TAKEN`, `MAX_ID_RESERVED`, `MAX_ID_VERIFICATION_REQUIRED`, `MAX_ID_LIMIT_REACHED`,
`INVALID_MAX_ID`, `WALLET_ALREADY_LINKED`, `CHALLENGE_INVALID`, `SIGNATURE_INVALID`,
`NO_COMPATIBLE_WALLET`, `NAMESPACE_MISMATCH`, `LAST_WALLET`, `UNAUTHORIZED`, `RATE_LIMITED`,
`INVALID_AMOUNT`, `RESOLUTION_STALE`, `SELF_PAYMENT`, `ASSET_UNAVAILABLE`, `CHAIN_NOT_CONFIGURED`,
`IDEMPOTENCY_CONFLICT`, `TX_ALREADY_USED`, `TX_HASH_MISMATCH`, `INVALID_TX_HASH`, `INVALID_PAYMENT_STATE`,
`ROUTE_UNAVAILABLE`, `QUOTE_REJECTED` (message has the provider's reason, e.g. amount too low), `QUOTE_INVALID`,
`ROUTING_PROVIDER_ERROR`, `ROUTE_LIMIT_EXCEEDED`, `INVALID_SLIPPAGE`, `CROSS_CHAIN_DISABLED`,
`SETTLEMENT_NO_WALLET`, `INVALID_SETTLEMENT_CHAIN`, `INVALID_SETTLEMENT_ASSET`,
`AGENT_NOT_FOUND`, `AGENT_PAUSED`, `AGENT_LOGIN_NOT_ALLOWED`, `CREDENTIAL_INVALID`, `INVALID_SCOPE`, `INVALID_POLICY`,
`SENDER_NOT_AGENT_WALLET`, `POLICY_DENIED`, `APPROVAL_NOT_PENDING`, `TRANSFER_NOT_AUTHORIZED`,
`RENAME_COOLDOWN`, `RENAME_PENDING`, `RENAME_SAME_NAME`, `INVALID_FEE_OPTION`, `RENAMES_DISABLED`, `MAX_ID_QUARANTINED`,
`X_VERIFICATION_REQUIRED`, `X_VERIFICATION_STALE`, `X_HANDLE_MISMATCH`, `X_ACCOUNT_LINKED_ELSEWHERE`, `RESERVED_CLAIM_EXISTS`,
`NAME_NOT_RESERVED`, `OAUTH_SESSION_MISMATCH`, `OAUTH_STATE_INVALID`, `OAUTH_DENIED`, `X_VERIFICATION_DISABLED`.

## Built so far vs. next

Done: config, logging, errors, middleware (request id, CORS/CSRF origin check, rate limit,
security headers), wallet challenge auth (SIWE + SIWS), sessions, wallet linking/removal,
MAX ID claim/availability/reserved names/profile/destinations with resolution versioning,
public resolver, audit log, unit + integration tests.

Also done: `payments` (direct EVM/ERC-20/SOL/SPL transfers, immutable snapshot enforced by a DB
trigger, idempotent prepare/submit, on-chain verification worker with leasing).

Also done: `routing` + `routing/nearintents` (cross-chain quotes, settlement preferences,
two-phase settlement tracking).

Also done: `agents` (scoped API keys, deterministic spend policy with race-free rolling budgets,
human approval queue, MAX-signed authorizations for agent signers).

Also done: `renames` (paid renames with a 72h cooldown, name reservation, quarantine, history).

Also done: `verifications` (X OAuth 2.0 + PKCE, reserved-name claims, manual review, re-verification,
admin revocation).

TypeScript SDK: see `../max-sdk` (typed client, safe retries, agent `PolicySigner`).
MCP server: see `../max-mcp` (read-only resolution + policy-bound proposals; never signs).

Next: (1Click quotes), `agents` (scoped credentials +
spend policies), rename with 72h cooldown + paid fee, X OAuth reserved-name claims,
Redis-backed rate limiting.
