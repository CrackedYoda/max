# Per-Chain Destinations, BNB and Avalanche, Chain Logos — Design Spec

Date: 2026-11-10
Status: Approved design ("yes, build it")

## 1. Goal

Owners can send each EVM chain to a different wallet or address, while keeping
one EVM default for everything else. Add BNB Chain and Avalanche. Show chain
logos everywhere a chain or network appears, on the web and in the app.

Wallets stay tied to the user's account, never to the MAX ID: selling or
transferring an ID clears its default and every per-chain override.

## 2. Decisions

| Topic | Decision |
|---|---|
| Model | One default per namespace (as today) plus optional per-chain overrides, EVM only |
| Override target | A linked EVM wallet, or a pasted, confirmed address (same rules as pasted defaults) |
| Resolution | Override for the requested chain, else the namespace default, else no destination |
| Removing an override | The chain falls back to the default |
| Removing the default | Overrides stay |
| Unlinking a wallet | Removes every destination using it, defaults and overrides |
| Transfer / sale | Clears defaults and overrides; buyer starts clean |
| Default-ID fill on wallet link | Fills only an empty namespace default, never an override |
| New chains | `bnb` (BNB Chain, id 56, BNB, 18 dec, 15 confirmations, `https://bscscan.com/tx/`), `avalanche` (Avalanche C-Chain, id 43114, AVAX, 18 dec, 1 confirmation, `https://snowtrace.io/tx/`) |
| Cross-chain into BNB/AVAX | Not offered: 1Click marks native BNB and AVAX deprecated |
| Logos | Vendored MIT SVGs from `@web3icons/core` 4.0.59 (networks/branded), one per chain plus namespaces |

## 3. Data model (migration `000014_chain_destinations`)

- `max_id_destinations.chain_key text NULL`. NULL = namespace default.
- Drop `UNIQUE (max_id, chain_namespace)`; add
  `UNIQUE (max_id, chain_namespace) WHERE chain_key IS NULL` and
  `UNIQUE (max_id, chain_key) WHERE chain_key IS NOT NULL`.
- CHECK `chain_key IS NULL OR chain_namespace = 'evm'`.
- Down: delete override rows, drop the two indexes and the CHECK and column,
  restore the original unique constraint.

## 4. API

- Defaults unchanged: `PUT/DELETE /v1/ids/:id/destinations/:namespace`.
- Overrides: `PUT/DELETE /v1/ids/:id/chain-destinations/:chain`, body
  `{walletId}` or `{address, confirmed: true}`. The chain must be a supported
  EVM chain (`UNSUPPORTED_CHAIN` otherwise); a wallet must be an EVM wallet of
  the caller.
- Owner views: each destination gains `chain` (null for a default).
- Public resolve and profile: shape unchanged; values honour overrides.
- Audit: `maxid.destination_set` / `maxid.destination_removed` carry `chain`
  for overrides.

## 5. UI

- Web dashboard and app IDs tab: each namespace row shows the namespace logo.
  The EVM row keeps one line, plus a small "Customize per chain" link; with
  overrides set it reads "N chains customized".
- Clicking opens a dialog (web) or a sheet (app) listing every EVM chain with
  its logo: "Uses default", or the override address (with "Unverified" when
  pasted). Each chain offers "Use default", the user's EVM wallets, or "Paste
  an address" with the existing review-and-confirm step.
- Chain logos also appear on: public profile chips, send network picker,
  receive rows, the claim/rename fee pickers and market chain labels where a
  chain is named.

## 6. Testing

Go unit tests for service validation and resolution order; integration tests
(compile-checked only, see earlier rulings); hosted e2e on :8095 for set/remove
override, fallback, transfer clearing; web and app type checks and lint; a
browser check of the dialog.
