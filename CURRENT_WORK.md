# Current work — MAX

A working log of where MAX stands, what changed and what is next. Newest entries first.
Product spec: `MAX_PRODUCT_SPEC.md`. Backend conventions: `BACKEND_BLUEPRINT.md` (identical to Noma's).

## Status (2026-10-23)

| Part | State |
|---|---|
| `max-core` (Go API) | Wallet-signature auth (EVM, Solana), MAX ID registry, per-chain destinations, resolver, direct EVM/Solana transfer preparation + on-chain verification worker, NEAR Intents 1Click cross-chain, agents + spending policies + scoped credentials, paid renames, X verification for reserved names. ~17k lines of Go; `go vet` clean, unit tests pass. |
| `max-sdk` | TypeScript SDK (zero runtime deps), `PolicySigner` for agents. |
| `max-mcp` | MCP server: read-only resolve + payment *proposals*; never signs or executes. |
| `max-frontend` | Being built in another session (Next.js scaffold, 2026-10-23). Only the brief `FRONTEND_PROMPT.md` is committed; commit the app when that work is done. |
| `max-mobile` | Empty. |

## Log

### 2026-10-23 — Put under version control; compared with Noma
- Nothing in `max/` was in git. Created one repository at the `max/` root (spec, blueprint, core, SDK, MCP and the
  frontend brief refer to each other by relative path). `.env`, `node_modules`, `dist` and `build-test` are
  ignored. `max-core/.env.example` was checked before committing: it holds only placeholders. No remote yet.
- `max-frontend` was mid-scaffold in another session at commit time, so only `FRONTEND_PROMPT.md` was committed;
  the rest of `max-frontend/` is left untracked.
- Recorded the comparison with Noma below.

## MAX vs Noma (2026-10-23)

Same core idea (pay a name, not an address), same stack and blueprint, **opposite architectures**.

| | Noma | MAX |
|---|---|---|
| Audience | Mainstream users; Nigeria first + limited global | Web3 users, developers, AI agents |
| Custody | **Custodial**: Noma ledger, escrow for non-users | **Non-custodial**: sender signs, funds go straight to the recipient's wallet |
| Sign-in | Phone/email OTP, Google, X | Wallet signature (MetaMask, Phantom) |
| Identity | `noma…` ID + `@name.noma` handle; phone/email/X | `max.yoda` registry; paid renames every 72 h; X for reserved names |
| Paying someone with no account | Yes (claims, support-led refunds) | No (`NO_COMPATIBLE_WALLET`; never guesses) |
| Real chains | Not yet (simulated ledger, testnets) | Yes: EVM/Solana prepare + on-chain verification |
| Cross-chain | Planned | NEAR Intents 1Click built |
| AI agents | None | Agents, policies, SDK, MCP |
| Fiat / KYC / staff ops | Core roadmap; staff console built | Out of scope |
| Regulatory weight | High (holds customer funds → licensing) | Lower (non-custodial) |

**Reading:** treat them as two layers, not competitors. MAX = non-custodial identity + routing for crypto users
and agents. Noma = consumer custodial app for Nigeria (naira, claims to people without accounts). Keep
separate codebases and likely separate legal entities.

**Reuse opportunities**
- Noma → from MAX: real-chain transfer preparation and verification, token-by-contract asset registry, NEAR
  Intents adapter (when Noma leaves the simulated ledger / adds swaps). MAX could also be Noma's "own your wallet"
  option (a Noma user's self-custody wallet as a MAX ID; Noma paying `max.x` names).
- MAX → from Noma: staff console + audit log, notification outbox, refund/state-machine patterns, KYC later.

**Watch:** two naming systems (`@x.noma` vs `max.x`) and two uses of X (Noma sign-in vs MAX reserved names) if
the products ever meet; splitting time across two products is the main risk.

## Next

- Create the GitHub repo and push (no remote is set).
- Build `max-frontend` from `FRONTEND_PROMPT.md`.
- Decide which product is the priority (see comparison).
