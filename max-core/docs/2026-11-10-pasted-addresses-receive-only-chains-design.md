# Pasted Addresses and Receive-Only Chains — Design Spec

Date: 2026-11-10
Status: Approved design, not implemented

## 1. Goal

Sub-projects 2 and 3 of the default-MAX-ID work (see
`2026-11-07-default-max-ids-design.md`). The user's scope: "all chains,
receive-only for btc litecoin tron".

1. **Pasted addresses.** A MAX ID owner can set a destination by pasting an
   address, without proving ownership with a wallet signature, on every
   supported namespace (EVM, Solana, and the new ones). The owner confirms the
   address first. The owner sees it labelled "Unverified".
2. **Receive-only chains.** Bitcoin, Litecoin and Tron (mainnet only). Names
   resolve on them to pasted addresses. MAX does no sending, payment tracking,
   cross-chain routing, settlement conversion or market settlement on them.

## 2. Decisions

| Topic | Decision |
|---|---|
| Storage | On `max_id_destinations` itself (approach A). A row has either `wallet_id` or a pasted `address`, never both. Pasted addresses are never rows in `wallets`. |
| Slots | Still one destination per (MAX ID, namespace). Pasting replaces a linked wallet in that slot and linking replaces a pasted address. |
| Namespaces | `evm`, `solana` (link or paste); `bitcoin`, `litecoin`, `tron` (paste only). |
| New chains | `bitcoin`, `litecoin`, `tron`, mainnet, `ReceiveOnly: true`. No testnets. |
| Assets on new chains | Native only. Tokens (e.g. TRC-20 USDT) return `INVALID_ASSET`. |
| Who can paste | The owner, on any of their IDs including the default; agents via the same endpoint with `ids:write`. |
| Sender view | Unchanged. Senders never see addresses in the app, and resolve/profile do not reveal verified vs pasted. In-app sends to a pasted EVM/Solana address work like any other destination. A recipient with no destination for the chain gets the existing "no destination" error. |
| In-app send on new chains | Refused with `RECEIVE_ONLY_CHAIN`. |
| Owner view | Each destination carries `source` (`wallet`/`pasted`) and `verified`. Pasted ones show an "Unverified" badge. |
| Confirmation | Required on every paste: the request carries `confirmed: true`; the UI shows the full address, the namespace, and the EVM multi-chain warning, behind an "I control this address" checkbox. |

## 3. Data model (migration `000013_pasted_destinations`)

- `max_id_destinations.wallet_id` becomes nullable.
- Add `address text` and `confirmed_at timestamptz`.
- `CHECK ((wallet_id IS NOT NULL AND address IS NULL AND confirmed_at IS NULL)
  OR (wallet_id IS NULL AND address IS NOT NULL AND confirmed_at IS NOT NULL))`.
- `UNIQUE (max_id, chain_namespace)` stays.
- `wallets.chain_namespace` CHECK is unchanged (linked wallets stay EVM/Solana
  in practice). No uniqueness on pasted addresses: two names, or two accounts,
  may paste the same address, and pasting never blocks anyone from linking it.
- Down: delete rows with `wallet_id IS NULL`, drop the CHECK and the two
  columns, restore `wallet_id NOT NULL`.
- All existing rows have a `wallet_id`, so the CHECK holds on live data.

Behaviour of existing paths:

- **Unlinking a wallet** deletes only destinations with that `wallet_id`
  (unchanged query). Pasted rows survive.
- **Transfer / market sale** clears all destinations (unchanged query), pasted
  included.
- **Default-ID fill on wallet link** only fills an empty slot (unchanged), so it
  never overwrites a pasted address.
- **Read queries** (`ListDestinations`, `ListPublicDestinations`) become
  `LEFT JOIN wallets` and return `COALESCE(w.address, d.address)`, keeping a row
  when it is pasted or its wallet is active.

## 4. Chains (`internal/chains`)

- Namespaces: add `NamespaceLitecoin = "litecoin"`, `NamespaceTron = "tron"`;
  `ParseNamespace` accepts all five.
- `Chain.ReceiveOnly bool` (`json:"receiveOnly"`). New entries:
  - `bitcoin`, "Bitcoin", BTC, 8 decimals, explorer `https://mempool.space/tx/`
  - `litecoin`, "Litecoin", LTC, 8 decimals, explorer `https://litecoinspace.org/tx/`
  - `tron`, "Tron", TRX, 6 decimals, explorer `https://tronscan.org/#/transaction/`
- `IsLinkable` stays EVM/Solana. New `IsPasteable(ns)`: all five.
- `NormalizeAddress` / `DisplayAddress` cover the new namespaces, with full
  checksum validation:
  - **Bitcoin:** base58check version `0x00` (P2PKH) or `0x05` (P2SH); bech32
    `bc` witness v0 (20 or 32 byte program); bech32m `bc` witness v1–16
    (P2TR = v1, 32 bytes). Bech32 stored lowercase; mixed case rejected.
  - **Litecoin:** base58check version `0x30` (L), `0x32` (M) or `0x05` (3);
    bech32/bech32m with HRP `ltc`, same witness rules.
  - **Tron:** base58check, 21-byte payload with version `0x41`, 34 chars.
  - **EVM:** existing rule plus: a mixed-case address must pass the EIP-55
    checksum. Stored lowercase, displayed checksummed (existing).
  - **Solana:** existing rule.

Receive-only chains are rejected at every money path:

| Path | Behaviour |
|---|---|
| Payment prepare (direct and cross-chain, origin or destination) | `RECEIVE_ONLY_CHAIN` (422), "MAX can't send on <Chain> yet." |
| Settlement preference | existing `ErrSettlementChain` |
| Market assets config | config error at startup if `MARKET_ASSETS` names one |
| Startup RPC wiring / payment workers / gate balance chains | skipped (no RPC client exists for them) |
| Wallet link challenge | existing `UNSUPPORTED_NAMESPACE` (not linkable) |

Resolve on a receive-only chain returns the pasted address with the note
"Receive-only on MAX: send from your own wallet." The profile's `reach` list
includes the new chains.

## 5. API

`PUT /v1/ids/:id/destinations/:namespace` takes exactly one of:

- `{ "walletId": "<uuid>" }`: unchanged; namespace must be linkable.
- `{ "address": "<string>", "confirmed": true }`: namespace must be pasteable.

| Case | Error |
|---|---|
| Both or neither field | `VALIDATION_ERROR` (400) |
| `confirmed` missing or false | `CONFIRMATION_REQUIRED` (400) |
| Address fails the namespace's format/checksum | `INVALID_ADDRESS` (400), message names the namespace |
| `walletId` on a receive-only namespace | `UNSUPPORTED_NAMESPACE` (existing) |

`DELETE /v1/ids/:id/destinations/:namespace` is unchanged.

Owner-facing destination shape (`/v1/ids`, `/v1/auth/me`, agent detail):

```json
{ "namespace": "bitcoin", "address": "bc1q…", "source": "pasted",
  "verified": false, "walletId": null, "updatedAt": "…" }
```

Public resolve and profile responses are unchanged apart from the new chains
and the receive-only note.

Audit: `maxid.destination_set` metadata gains `source` and, for pastes,
`confirmed: true`. Every set or remove bumps `resolution_version` (unchanged).

## 6. Interfaces

- **SDK (`max-sdk`):** `Namespace` adds `bitcoin | litecoin | tron`;
  destination type adds `source`, `verified`, optional `walletId`; chain type
  adds `receiveOnly`; `setDestination` accepts `{ walletId }` or
  `{ address, confirmed: true }`; error messages for the three new codes.
- **Web (`IdCard.tsx`):** five namespace rows. Picker = linked wallets
  (EVM/Solana) then "Paste an address"; BTC/LTC/Tron offer paste only. Paste
  flow: field with live validation → confirm step (full address, namespace,
  EVM warning) → checkbox "I control this address. Payments sent to it can't
  be reversed." → Save. "Unverified" badge with the hint "Pasted, not proven by
  a wallet signature."
  EVM warning text: "This address will receive on every EVM chain MAX supports
  (Ethereum, Base, Arbitrum, OP, Polygon). Exchange deposit addresses often
  accept only one chain."
- **Web public profile:** new chains appear in reach chips; their send action
  is replaced by "Receive-only: send from your own wallet". No addresses shown.
- **App (`max-app`):** `destination.tsx` sheet gets the same paste and confirm
  flow and five namespaces; IDs tab shows the badge; Receive tab shows BTC/LTC/
  Tron addresses with QR and a "Receive-only" note; Send flow offers no
  receive-only chains and maps `RECEIVE_ONLY_CHAIN` to its message.
- **MCP (`max-mcp`):** resolve works unchanged. If a destination-setting tool
  exists, it gains the paste body; otherwise untouched.

## 7. Safety

- Pasted addresses never authenticate, never count for wallet uniqueness,
  never serve as market payout or payer wallets, never fill a default ID.
- Checksums are mandatory for every format, so a single typo can't save.
- Pastes go through the existing ID-write rate limit and are audited.
- Migration is reversible; back up `max_id_destinations` before running it on
  the hosted `max` database.

## 8. Testing

- **Go unit:** address vectors per format (BTC P2PKH/P2SH/P2WPKH/P2WSH/P2TR;
  LTC L/M/3/ltc1; Tron; EVM mixed-case good and bad), each with a corrupted
  checksum case; `SetDestination` body selection and errors; resolve on
  receive-only chains (native ok, token rejected, note present); payment
  prepare returns `RECEIVE_ONLY_CHAIN`; settlement rejects the new chains.
- **Go integration (server tests):** paste → resolve → remove; link replaces
  paste and paste replaces link; unlinking a wallet keeps pasted rows;
  transfer clears them; agent can paste; default-ID fill does not overwrite a
  paste; pasting someone else's address doesn't stop them linking it; DB CHECK
  rejects both/neither rows.
- **Front ends:** web and app tsc, lint, existing tests, plus unit tests for
  the client-side address check helpers.
- **End-to-end on the hosted `max` DB** (API on :8095): paste on all five
  namespaces and resolve each; in-app prepare to a pasted Solana address
  succeeds; BTC prepare refused; browser paste flow and badge. Test data
  deleted afterwards.

## 9. Out of scope

Tron tokens, testnets for the new chains, payment tracking on the new chains,
an account-level address book reused across names, `max-mobile`.
