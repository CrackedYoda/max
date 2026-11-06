# @max/mcp

MCP server for **MAX**: lets an AI agent look up MAX IDs and **propose** payments. Per the MAX spec it exposes read-only resolution and payment preparation. **It never signs, submits, approves or executes a payment.**

## Tools

| Tool | What it does | Needs agent key |
|---|---|---|
| `max_resolve` | MAX ID + chain (+ asset) → verified address. Never guesses an address | no |
| `max_profile` | Human or agent, chains it receives on, settlement preference | no |
| `max_check_availability` | Can this name be registered, and why not | no |
| `max_list_chains` | Supported chains | no |
| `max_propose_transfer` | Propose a payment from the agent's wallet. MAX policy decides | yes |
| `max_get_transfer` | Decision and status of a proposal | yes |
| `max_list_transfers` | The agent's sent and received payments | yes |
| `market_search` | Search active MAX ID listings | no |
| `market_get_listing` | Public market view of one name | no |
| `market_list_name` | List a name for sale (needs the admin's `canSell` policy; may be held for approval) | yes |
| `market_delist` | Remove a listing | yes |
| `market_make_offer` | Offer on a name, from the operator's wallet. Never held: the spend policy applies when an accepted offer is paid | yes |
| `market_accept_offer` | Accept an offer on a name the agent owns (may be held) | yes |
| `market_buy` | Lock a listing; returns sale, quote and instructions. Never signs: verify with `assertSaleInstructionsMatch`, then sign with your own signer (may be held) | yes |
| `market_get_sale` | Status of a sale | yes |

Market actions held by policy return `{status:"pending_approval", authorizationId, reasons}` until the administrator approves. The marketplace fee is 10%, taken from the seller's proceeds; Solana USDC only for now.

Without `MAX_AGENT_KEY`, only the first four plus `market_search` and `market_get_listing` are registered.

## How it keeps the model out of the money path

- **A proposal is decided by MAX's deterministic spending policy, not by the model.**
  - Within the agent's per-asset limits, rolling 24h budget and recipient allowlist → `auto_approved`. Your separate signer, using `PolicySigner` from `@max/sdk` with its own key, then verifies MAX's authorization and executes.
  - Anything else → `pending_approval`, waiting for the human administrator.
  - The model's text is passed as `instruction` for audit only.
- **No signing material reaches the model.** Authorizations and signing instructions are stripped from tool results.
- **The sending wallet is set by the operator** (`MAX_AGENT_SENDER`). It isn't a tool argument, so the model can't choose whose funds move.
- **Retries don't double-pay.** A proposal without an `idempotencyKey` gets one derived from its arguments and a 10-minute window, so a model retrying the same call gets `replayed: true` instead of a second payment.
- **Owner-written text is fenced.** Display names and descriptions come back under `userSupplied`, marked as data rather than instructions.
- **Least privilege.** Give this server a key with only `payments:propose` and `payments:read`; it warns on startup if the key can submit or read authorizations. The signer gets a separate key.

## Setup

1. In the MAX dashboard (or with `@max/sdk`), create the agent, link its wallet, set its policy, and issue a key with scopes `payments:propose`, `payments:read`.
2. Add the server to your MCP client.

Claude Code:

```bash
claude mcp add max -e MAX_API_URL=https://api.max.example -e MAX_AGENT_KEY=maxk_… -e MAX_AGENT_SENDER=<agent wallet address> -- npx @max/mcp
```

Claude Desktop (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "max": {
      "command": "npx",
      "args": ["@max/mcp"],
      "env": {
        "MAX_API_URL": "https://api.max.example",
        "MAX_AGENT_KEY": "maxk_…",
        "MAX_AGENT_SENDER": "<agent wallet address>"
      }
    }
  }
}
```

For lookups only, set just `MAX_API_URL`.

Embedding the server in your own process instead:

```ts
import { createMaxMcpServer } from "@max/mcp";
const server = createMaxMcpServer({ client, agentClient, sender });
```

## Development

```bash
npm test                                            # MCP client ↔ server in memory, mocked API
MAX_API_URL=http://localhost:8090 npm run test:e2e  # spawns dist/bin.js over stdio against max-core
```
