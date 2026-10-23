# MAX — Universal Payment Identity & Infrastructure

**Status:** Product specification / hackathon foundation  
**Name:** MAX (working name)  
**Platforms:** Web application, developer API, AI-agent SDKs  
**Audience:** Web3 users, developers, autonomous AI agents

## 1. Vision

MAX makes sending cryptocurrency as easy as sending money to a username. Humans and AI agents create a MAX account, connect their existing wallets, and obtain a unique **MAX ID** such as `max.yoda` or `max.researchbot`. Senders enter that ID; MAX resolves the correct destination for the selected asset and chain. The recipient takes no action to receive.

MAX is **not initially a wallet**, does not require recipient payment requests, and does not hold users' private keys. It is an identity, wallet-resolution and payment-routing layer. Cross-chain conversion is available only when necessary and supported. A standalone MAX multichain wallet is a possible later product.

**Principle:** One identity. Many wallets. Humans and agents. Direct transfers by default.

## 2. Core product rules

1. Users register a MAX account and claim one or more MAX IDs subject to product rules.
2. Each MAX ID resolves to verified wallet addresses for supported networks. One account may manage multiple IDs, including agent IDs.
3. Recipients **never sign, approve or confirm an incoming payment**. They must verify wallet ownership only when first linking a wallet or changing its configuration.
4. Senders authorize outgoing transfers using their wallet or a previously granted, bounded agent authorization. MAX never bypasses sender authorization.
5. Same-chain native coins and tokens, **including memecoins**, transfer directly to the recipient's matching-chain wallet. No swaps or conversion.
6. Cross-chain conversion happens only if the sender requests it or the recipient's configured destination requires it, and a supported route is available. Show quotes and costs to the sender before authorization.
7. A MAX ID is an identifier, **not a deposit wallet**. No private keys are collected from connected wallets.
8. No mandatory payment-request creation, invoices or recipient acceptance flow.
9. MAX IDs are configurable, subject to security rules and change limits.
10. Design ID ownership and transfers to support a future marketplace without requiring one at launch.

## 3. Human onboarding and receiving

### Onboarding

1. Visit MAX and create an account by connecting a wallet.
2. Sign a domain-bound, nonce-based challenge to prove control of the wallet. This is **wallet linking**, not approval to receive each payment.
3. Choose an available MAX ID, e.g. `max.yoda`.
4. Optionally link more wallets: Phantom/Solana, MetaMask/EVM, native Bitcoin wallet, etc. Every address requires its own ownership proof.
5. Configure a default wallet per chain and, optionally, settlement preferences for supported cross-chain payments.

### Direct transfer example: BONK

1. Sender connects Phantom, enters `max.yoda`, selects BONK on Solana and amount.
2. MAX identifies the asset using **network + mint/contract address** (not symbol alone).
3. MAX resolves `max.yoda` to its verified Solana address and presents the destination and fee to the sender.
4. Sender signs the transfer in Phantom.
5. BONK arrives directly in the recipient's Solana wallet. **No conversion, no receiving signature, no recipient interaction.**

For Solana SPL tokens, the sender-side transaction may need to create the recipient's associated token account (ATA), with fees and account existence handled transparently. The recipient still need not sign.

### Direct transfer example: BTC

1. Sender selects BTC and enters `max.tunde`.
2. MAX resolves a verified Bitcoin-compatible receiving address.
3. Sender signs and broadcasts a normal Bitcoin transaction.
4. Tunde receives BTC in the linked wallet; no recipient action is required.

### Cross-chain example: BTC to Solana USDC

If the recipient has no BTC destination or has explicitly configured USDC on Solana as a preferred settlement option, MAX may offer a supported BTC → Solana USDC route through **NEAR Intents / 1Click**. The sender reviews the quote, expected output, fees and destination, then authorizes. MAX monitors the route and reports final settlement. Never imply all coins or chains are routable; unsupported routes must fail safely.

### Missing destination

If no matching-chain wallet exists for a direct transfer, **do not guess an address**. Offer an explicitly quoted supported conversion route, or block the transfer and explain that the recipient needs a compatible wallet. Do not automatically custody incoming funds or create wallets without the user's informed setup.

## 4. AI-agent payments

Agents can create/manage a MAX identity and link an authorized receiving wallet through a simple dashboard or developer API. An agent's ID may look like `max.researchbot`.

### Agent setup

1. Developer registers an agent under an account, or creates an independent agent account with an appropriate recovery administrator.
2. Claim an available agent MAX ID.
3. Prove ownership of receiving wallets using a signed challenge or a supported smart-account verification mechanism.
4. Set default destinations per chain and optional cross-chain settlement preferences.
5. Create narrowly scoped API credentials for resolving IDs, observing payments and proposing transfers.

### Agent-to-human / agent-to-agent transfers

A human can instruct their agent: **“Send 0.1 SOL to max.yoda.”** Or one agent can pay `max.researchbot` for a service. The agent resolves the ID and prepares the payment. A trusted signer or policy-controlled wallet executes the payment after validating amount, chain, asset, recipient, spending cap and authorization. For an unapproved transaction, the agent requests the wallet owner's approval. Receiving agents do not approve incoming payments.

**Critical boundary:** Natural-language instructions and LLM output are **not** transaction authorization by themselves. Agents must not hold unrestricted master private keys. Use scoped session keys, smart-account policies, dedicated signers, per-transaction limits, allowlists, revocation and optional human approval. Prompt injection must not be able to override these controls.

### Developer interfaces

- `POST /v1/agents` — create an agent profile (authenticated)
- `POST /v1/ids` — claim an available MAX ID
- `POST /v1/wallets/challenge` and `POST /v1/wallets/verify` — prove wallet control
- `GET /v1/resolve/{maxId}?chain=solana&asset=<mint>` — resolve recipient
- `POST /v1/transfers/prepare` — produce direct transfer instructions or cross-chain quote
- `POST /v1/transfers/{id}/submit` — submit a sender-signed transaction reference
- `GET /v1/transfers/{id}` — check transaction/settlement state
- `POST /v1/agent-credentials` — create scoped credentials

SDKs: TypeScript first, Python second, Go third. MCP tools may expose **read-only resolution and payment preparation**; executing payments requires independent authorization. x402 integration is a later compatibility feature, not a prerequisite for basic transfers.

## 5. MAX ID system

### Naming and ownership

- Canonical display format: `max.yoda` (working syntax; exact namespace/domain to be finalized).
- Normalize case and Unicode consistently; initially restrict to a conservative lowercase ASCII character set to prevent homoglyph impersonation.
- Enforce uniqueness at the database level and reserve protected names.
- Separate **account owner**, **MAX ID owner**, **linked wallets**, **agent administrator**, and **payment credentials** in the data model.
- A user can configure which verified address each ID resolves to on each supported chain.
- An ID's wallet mappings can change without changing the ID itself.

### Rename policy

- A MAX ID can be renamed **at most once every 72 hours (three days)**.
- **Every rename requires payment**; fee amount, currency and payment provider are configurable by administrators.
- The rename is applied only after fee settlement is verified; a pending or failed payment must not consume the rename entitlement.
- Use an atomic database transaction to reserve the new name, verify eligibility, finalize the rename, release or quarantine the old name, and update the 72-hour timestamp.
- To reduce phishing, quarantine old names for a defined period rather than making them immediately claimable. Keep historical name records and notify the owner.
- A rename must not alter the destination of an already-authorized payment; each payment stores a locked resolution snapshot.

### Reserved and premium names

Some IDs (brands, notable handles, protected words, system names) are reserved. Certain reserved names may be claimed through **X (Twitter) OAuth** to establish control of a matching X account. OAuth proves control of that X account **at verification time**, not trademark rights or permanent identity; maintain re-verification, disputes, impersonation review, and revocation rules. High-risk brand names may require manual review beyond OAuth.

### Future transferability / marketplace readiness

Design now for later selling, buying, leasing or transferring MAX IDs, but **do not implement a marketplace in the hackathon**.

- Treat each ID as a separately owned registry asset, with a stable internal ID independent of its displayed name.
- Maintain ownership history, status, transfer locks, verified identity flags and audit logs.
- Support a future atomic ownership transfer with seller/buyer consent and payment settlement.
- **On sale or transfer, disconnect all prior wallet mappings, agent credentials, sessions and settlement preferences**; the buyer must verify their own wallets before receiving funds.
- Reserve a dispute/hold process for fraud, stolen accounts, trademarks and sanctioned names.
- Do not assume on-chain NFTs are necessary; a database registry is sufficient initially. If tokenization is introduced later, legal and security implications must be reassessed.

## 6. MAX ID configuration

Per-ID configurable settings:

- Display name, avatar, description and human/agent classification.
- Verified wallets and primary destination per supported chain.
- Optional preferred settlement chain and asset for cross-chain routes.
- Public agent/service metadata and API discovery fields, if enabled.
- Owner-controlled security settings, alerts and recovery options.
- Optional visibility of linked addresses; public resolution still exposes the destination needed for a payment.

**Not required:** recipient payment requests, recurring approval to receive, or conversion of memecoins on a matching chain.

## 7. Payment engine design

### Route A — Direct payments (default)

`MAX ID → resolve asset chain → verified chain address → sender signs → on-chain transfer → confirmation`

- EVM native coins: send transaction to verified EVM address.
- ERC-20 tokens/memecoins: use token contract `transfer` to verified EVM address.
- Solana SOL: system transfer; SPL tokens/memecoins: token transfer to ATA.
- Bitcoin: native transfer to a verified compatible address.
- No bridge, swap, token conversion or MAX custody.

### Route B — Optional cross-chain payments

`MAX ID → resolve recipient preference → NEAR Intents quote → sender authorization → solver/settlement → recipient wallet`

- Integrate NEAR Intents / 1Click through a replaceable provider adapter.
- Validate chain, token identifiers, amount, route, expiry, destination and minimum output.
- Pin the resolved recipient and quote parameters to an immutable payment intent.
- Monitor deposits, solver progress, confirmations, refunds and finality.
- Never silently convert assets, substitute a recipient or retry a signed transfer in a way that could double-pay.

### Payment states

`CREATED → QUOTED (if cross-chain) → AWAITING_SENDER_SIGNATURE → SUBMITTED → CONFIRMING → COMPLETED`

Exceptional states: `EXPIRED`, `FAILED`, `REFUND_PENDING`, `REFUNDED`, `MANUAL_REVIEW`. State transitions must be idempotent and auditable. A direct transaction may omit the quote stage.

## 8. Security model

### Identity and wallet security

- EVM wallet login: SIWE/EIP-4361-style domain-bound, expiring challenges; Solana: signed challenge with nonce, origin and expiry; Bitcoin: compatible proof of address control where supported.
- Unique nonces, replay prevention, signature verification and rate limits.
- A connected wallet must be verified before it can receive payments under an ID.
- Sensitive changes require fresh authorization, session revocation options and notifications.
- Design recovery using verified alternate wallets and delayed recovery workflows; never allow an attacker with only a compromised email session to redirect payments.
- Secure reserved-name claiming and rename operations against race conditions and takeover.

### Payment security

- Immutable recipient resolution snapshot at sender authorization.
- Show resolved destination and network before the sender signs; wallet UI should corroborate the transfer.
- Validate token contract/mint and chain IDs; symbols are untrusted.
- Verify transactions server-side rather than trusting a user-supplied success flag.
- Signed provider callbacks where available, webhook replay protection and reconciliation.
- For cross-chain: allowlisted providers, slippage bounds, fee disclosure, refund-address checks, provider incident monitoring and an emergency pause.
- Avoid custody; document any temporary escrow/custody in external solver routes.

### AI-agent security

- Scoped API keys, policy-based signing, max per-transfer/day budgets, destination allowlists, asset restrictions, key rotation and instant revocation.
- Human approval for transfers beyond delegated authority.
- Separate agent prompts from deterministic transaction policy enforcement.
- Log agent instruction, authorization source, payment intent, signature and on-chain outcome without leaking secrets.

### Infrastructure

- HTTPS, HSTS, secure cookies, CSRF protection as appropriate, secrets manager, encrypted data at rest, least-privilege DB access, RBAC, request limits and audit logs.
- Independent security review before meaningful mainnet volume.
- Monitor fraud, malicious lookalike IDs, scam tokens, incorrect chain resolution and external provider incidents.

## 9. Recommended technology stack

| Layer | Recommendation | Reason |
|---|---|---|
| Web | **Next.js + TypeScript + Tailwind CSS** | Fast consumer and developer dashboard |
| UI components | shadcn/ui | Consistent, accessible components |
| EVM wallet UX | wagmi + viem + WalletConnect-compatible connectors | Signing, chain interactions, wallet support |
| Solana wallet UX | Solana Wallet Adapter + `@solana/web3.js` or maintained successor | Phantom and Solana transfers |
| Backend API | **Go + Gin** | Lightweight, concurrent, familiar infrastructure |
| Database | **PostgreSQL + sqlc + pgxpool** | Strong constraints and atomic ID ownership changes |
| Cache / async | **Redis + Asynq** | Nonces, rate limiting, settlement monitoring jobs |
| Cross-chain | **NEAR Intents / 1Click API** behind provider interface | Quotes, routing and settlement without building bridges |
| EVM chains | RPC providers + viem | Direct native/ERC-20 transfers |
| Solana | RPC provider + Solana SDK | Direct SOL/SPL transfers |
| Bitcoin | Reputable Bitcoin API/node provider; own node later | Address validation and transaction monitoring |
| Auth | Wallet challenge/signature + secure server sessions | Non-custodial wallet ownership proof |
| Agent SDK | TypeScript, then Python | Most common agent integration paths |
| Agent interoperability | REST, MCP tools; optional x402 | Programmatic identity resolution and payments |
| Observability | OpenTelemetry + Prometheus/Grafana + Sentry | Payments and failures must be traceable |
| Deployment | Docker + GitHub Actions; VPS initially or AWS ECS/RDS | Start lean, migrate without redesign |
| Secrets | Cloud secrets manager / KMS | Avoid raw signing secrets in app environment |

Use Go for the main API, identity registry, policies, jobs and NEAR adapter. For direct wallet signing, the user's browser wallet or a policy-restricted agent signer signs; the backend never signs on behalf of ordinary users.

## 10. Suggested services/modules

Start with a **modular monolith**, not microservices:

- `auth` — challenges, sessions, connected-wallet ownership.
- `accounts` — humans, organizations, agents, roles.
- `maxids` — name normalization, availability, registration, rename, reservations, ownership.
- `wallets` — verified address registry and chain-specific validation.
- `resolver` — MAX ID + chain/asset → destination snapshot.
- `payments` — direct transaction preparation, lifecycle, confirmation.
- `routing` — cross-chain provider interface and NEAR Intents implementation.
- `agent` — scoped API credentials and payment policies.
- `billing` — paid renames and fee receipts.
- `risk` — suspicious IDs, token warnings, route restrictions.
- `audit` — immutable activity records.

Possible Go layout:

```text
max/
  apps/web/
  services/api/
    cmd/api/
    cmd/worker/
    internal/
      auth/
      accounts/
      maxids/
      wallets/
      resolver/
      payments/
      routing/nearintents/
      agents/
      billing/
      risk/
      audit/
    migrations/
    sql/
  packages/sdk-typescript/
  packages/sdk-python/       # later
  infra/docker/
  docs/
```

## 11. Data model (high level)

- `accounts(id, type, status, created_at)`
- `account_members(id, account_id, principal_id, role)`
- `max_ids(id, canonical_name UNIQUE, owner_account_id, type, status, rename_available_at, created_at)`
- `max_id_history(id, max_id, old_name, new_name, changed_at, fee_payment_id)`
- `reserved_names(id, canonical_name UNIQUE, claim_policy, status)`
- `external_verifications(id, account_id, provider, provider_user_id, verified_at, expires_at)`
- `wallets(id, account_id, chain_namespace, address, proof_metadata, verified_at, status)`
- `max_id_destinations(id, max_id, chain_namespace, wallet_id, is_default, updated_at)`
- `settlement_preferences(id, max_id, destination_chain, destination_asset, enabled)`
- `payments(id, sender_account_id, max_id, resolved_destination, source_chain, source_asset, amount, route_type, status, idempotency_key, tx_hash, created_at)`
- `payment_events(id, payment_id, event_type, provider_ref, occurred_at)`
- `agent_credentials(id, agent_account_id, scopes, policy_id, revoked_at)`
- `agent_policies(id, agent_account_id, limits_json, allowed_destinations_json)`
- `rename_fee_payments(id, max_id, amount, asset, status, settled_at)`
- `max_id_ownership_events(id, max_id, previous_owner, next_owner, event_type, created_at)`
- `audit_logs(id, actor, action, resource, metadata, created_at)`

Ensure uniqueness of the active destination per MAX ID and chain, and maintain immutable payment snapshots. Use DB transactions and row locking for claims, renames and future ownership transfers.

## 12. API behavior and resolution safety

Resolution returns the chain, verified destination address, token compatibility metadata, MAX ID's stable internal ID, and a short-lived resolution version. It must not return wallet secrets. A payment preparation endpoint must validate the version, create an immutable snapshot and enforce policy. If an ID is renamed or transferred, previously authorized transfers use their original snapshot; new transfers use the current owner only after re-verification.

No token should be considered legitimate based on symbol or name alone. Never promise that every cryptocurrency is supported; supported chains, wallets and conversion routes must be explicit.

## 13. Hackathon scope

### Must build

1. Account login through Phantom and MetaMask.
2. MAX ID registration and unique-name lookup.
3. Verified Solana and EVM wallet linking.
4. Configurable per-chain destination mapping.
5. Direct SOL, SPL token/memecoin, ETH and ERC-20 transfers to a MAX ID.
6. A working optional cross-chain route using NEAR Intents on supported assets.
7. Sender-only signing; no recipient approval.
8. Basic AI-agent registration, ID resolution and a policy-guarded agent payment demo.
9. Rename cooldown (72 hours) and paid rename transaction verification, if hackathon time allows.
10. Reserved-name records and X OAuth verification prototype, if time allows.

### Defer

- Native MAX wallet.
- Full ID marketplace, NFT tokenization or leasing.
- Support for every blockchain and arbitrary memecoin conversion.
- Fully autonomous unlimited agent spending.
- Complex fiat on/off-ramps.
- Full brand/trademark arbitration workflow.

### Demo script

1. User A connects Phantom and MetaMask and registers `max.yoda`.
2. User B enters `max.yoda` and sends BONK on Solana; MAX resolves the correct wallet; User A does nothing.
3. User B sends an ERC-20 on Base; MAX resolves User A's EVM wallet.
4. Demonstrate a supported cross-chain asset conversion through NEAR Intents.
5. Agent `max.researchbot` receives a transfer automatically.
6. User instructs a policy-authorized agent to send a bounded amount to `max.yoda`; the agent resolves and prepares/executes according to its delegated permissions.
7. Show rename eligibility, fee requirement and reserved-name protection.

## 14. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| ID takeover / payment redirection | Critical | Fresh wallet proof, recovery controls, change alerts, locked payment snapshots |
| Name impersonation / lookalikes | High | Conservative naming, reserved names, verification badges, disputes |
| Reserved name claimed via compromised X | High | OAuth re-verification, review, revocation, trademark policy |
| Rename phishing / old-name reuse | High | Old-name quarantine, name history, notifications |
| ID resale with old wallet mappings | Critical | Atomic transfer + forced destination/credential reset |
| Agent prompt injection | Critical | Independent policy signer, spending caps, approval thresholds |
| Wrong chain or fake token | Critical | Chain + mint/contract verification, trusted token metadata |
| Cross-chain bridge/solver incident | Critical | Provider isolation, limits, refund logic, emergency stop |
| Stale quote / slippage | High | Quote expiry, minimum received, settlement monitoring |
| Token-account creation or gas failures | Medium | Preflight validation, accurate fee display |
| Duplicate execution / retry | Critical | Idempotency, unique provider references, state machine |
| Privacy leakage from public ID | Medium | Minimal profile data, optional address display, transparency about on-chain linkability |
| Compliance/custody exposure | High | Legal review, sanctions/AML analysis, non-custodial default, provider diligence |
| Single routing provider dependency | High | Adapter interface, supported-route fallback, graceful failure |

## 15. Commercial model (optional)

- Paid MAX ID renames (required by product rules).
- Premium or reserved ID claims where legally appropriate.
- Transparent optional service fee on supported cross-chain routes.
- Developer/agent API tiers at scale.
- Future MAX ID marketplace transfer fee.

Direct same-chain transfers should remain simple and low-friction. Do not charge hidden fees or intercept funds; disclose any MAX fee before the sender signs.

## 16. Key open decisions

- Final MAX ID syntax and ownership model: centralized registry first, on-chain portability later?
- Exact rename price and accepted fee assets.
- Old-name quarantine duration after rename.
- Whether accounts may hold unlimited IDs or need tiered limits.
- Rules for reserved names, X OAuth claims and trademark disputes.
- Initial chain list and supported cross-chain NEAR Intents routes.
- Recovery mechanism when the only linked wallet is lost.
- Whether agent identifiers are transferable independently from the developer's account.

## 17. Product summary

**MAX is a universal payment identity and routing layer for people and AI agents.** Users connect their own wallets, claim a MAX ID, and receive direct transfers of supported native assets and tokens—including memecoins—on the matching chain, without approving incoming payments. Optional cross-chain settlement uses NEAR Intents. Agents can register IDs and make policy-authorized payments to human or agent IDs. MAX IDs are configurable, subject to paid 72-hour renaming rules, can be reserved for verified X identities, and are designed for future ownership transfer and trading.
