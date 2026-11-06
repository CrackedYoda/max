# @max/sdk

TypeScript SDK for **MAX**, the payment identity layer: resolve `max.yoda`, send payments, and run AI agents whose spending is bounded by policy.

- **Zero runtime dependencies.** It uses `fetch` and WebCrypto and runs on Node ≥ 18.17, browsers, Deno, Bun and edge runtimes.
- **Fully typed** against the API's response shapes. Errors carry stable codes (`MaxApiError.code`).
- **Retries only when safe.** GETs retry on network errors and 5xx. POSTs retry only where the API is idempotent by key (prepare, propose, rename), and the SDK generates that key for you.
- **Agent signer contract built in.** `verifyAuthorization` and `PolicySigner` make a prompt-injected agent unable to get a payment signed.

```bash
npm install @max/sdk
```

## Resolve and pay

```ts
import { MaxClient, isMaxApiError } from "@max/sdk";

const max = new MaxClient({ baseUrl: "https://api.max.example" }); // browser: session cookie is used automatically

const r = await max.resolve("max.yoda", { chain: "solana", asset: "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263" });
// show r.address to the user, then:
const payment = await max.transfers.prepare({
  recipient: "max.yoda",
  chain: "solana",
  asset: "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263", // BONK by mint, never by symbol
  amount: "1000000",
  senderAddress: wallet.publicKey.toBase58(),
  resolutionVersion: r.resolutionVersion, // refuse if the recipient changed wallets since review
});

const txHash = await signAndSend(payment.instructions!.solana!); // your wallet code
await max.transfers.submit(payment.id, txHash);
const final = await max.transfers.waitFor(payment.id, { onUpdate: (p) => console.log(p.status) });
```

Cross-chain (NEAR Intents) uses the same call with a destination:

```ts
const quote = await max.transfers.prepare({
  recipient: "max.yoda", chain: "base", asset: "native", amount: "0.01", senderAddress,
  destinationChain: "solana", destinationAsset: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
});
quote.crossChain!.minAmountOut.display; // show before signing
```

Branch on error codes, not messages:

```ts
try { await max.resolve("max.yoda", { chain: "base" }); }
catch (e) { if (isMaxApiError(e, "NO_COMPATIBLE_WALLET")) offerCrossChain(); else throw e; }
```

## Marketplace

Buy and sell MAX IDs. The core SDK has no Solana dependency; building the transaction lives in `@max/sdk/solana`, which needs the optional peers `@solana/web3.js` and `@solana/spl-token`.

```ts
import { MaxClient, isPendingApproval } from "@max/sdk";
import { buildVerifiedSolanaSaleTransaction } from "@max/sdk/solana";

const max = new MaxClient({ baseUrl: "https://api.max.example" });

const { listings } = await max.market.search({ q: "yoda", sort: "price_asc" });
const res = await max.market.buy(listings[0].listingId, { fromAddress: wallet.publicKey.toBase58() });
if (isPendingApproval(res)) throw new Error(`held for approval: ${res.authorizationId}`); // agents only

const { blockhash } = await connection.getLatestBlockhash();
// Checks the instructions against the sale (exact fee to the treasury, exact remainder
// to the seller, from the buyer, plus the sale memo) before building. Pass what you
// trust independently: the connected wallet, the asset and price the user accepted.
const tx = buildVerifiedSolanaSaleTransaction(res, wallet.publicKey.toBase58(), blockhash, {
  asset: listings[0].asset,
  priceBaseUnits: listings[0].priceBaseUnits,
});
const signed = await wallet.signTransaction(tx);
const signature = await connection.sendRawTransaction(signed.serialize());

await max.market.submit(res.sale.id, signature);
let view = await max.market.getSale(res.sale.id);
while (!["completed", "expired", "cancelled", "reversed"].includes(view.sale.status)) {
  await new Promise((r) => setTimeout(r, 2000));
  view = await max.market.getSale(res.sale.id);
}
```

`assertSaleInstructionsMatch` (called by `buildVerifiedSolanaSaleTransaction`) proves the instructions match the sale: it derives the token accounts from the sale's own addresses, so it does not catch a response that rewrites both. Always pass `expect` with values you trust independently (connected wallet as `buyer`, the accepted listing's `asset` and `priceBaseUnits`, a pinned `treasury`). The raw `buildSolanaSaleTransaction` is unverified. Agents' `buy`, `list`, `updateListing`, `offer`, `acceptOffer` and `payOffer` may return `{ status: "pending_approval", authorizationId, reasons }` instead of throwing; the administrator releases them with `max.agents.approveMarket(authorizationId)`.

## Sign in with a wallet

`WalletSigner` is anything that signs a text message. EVM wallets return the 0x `personal_sign` hex. Solana wallets return base58 of the ed25519 signature.

```ts
await max.auth.login({
  namespace: "solana",
  address: phantom.publicKey.toBase58(),
  signMessage: async (m) => bs58.encode((await phantom.signMessage(new TextEncoder().encode(m))).signature),
});
await max.ids.claim({ name: "max.yoda" });
```

For server-side scripts, use `new MaxClient({ baseUrl, persistSession: true })` so the login cookie is kept between calls.

## AI agents

An administrator (a signed-in human) creates the agent, links its receiving wallet, sets its policy and issues a scoped key:

```ts
const agent = await max.agents.create({ name: "Research Bot" });
await max.agents.linkWallet(agent.id, agentWalletSigner);
await max.agents.claimId(agent.id, { name: "researchbot" });
await max.agents.setPolicy(agent.id, {
  limits: [{ chain: "solana", asset: "native", maxPerTransfer: "0.2", maxPer24h: "0.5" }],
  allowedRecipients: ["max.yoda"],
});
const key = await max.agents.issueKey(agent.id, {
  scopes: ["payments:propose", "payments:read", "payments:submit", "authorizations:read"],
});
// key.secret is shown once
```

The agent runtime proposes transfers. MAX's deterministic policy decides; the agent's words don't. Your **signer**, kept separate from the LLM and holding the session key or policy wallet, acts only on authorizations that MAX signed:

```ts
import { MaxClient, PolicySigner, assertSolanaTransferMatches } from "@max/sdk";

const agentClient = new MaxClient({ baseUrl, apiKey: process.env.MAX_AGENT_KEY });
const signer = new PolicySigner({ client: agentClient, sender: agentWallet.address });

const { transfer, payment } = await signer.proposeAndExecute(
  { recipient: "max.yoda", chain: "solana", amount: "0.1", instruction: userMessage /* audit only */ },
  async (claims) => {
    // claims are verified: MAX's signature, unexpired, this payment, this sender.
    const s = claims.instructions.solana!;
    const ix = buildTransfer(s);        // build from the claims...
    assertSolanaTransferMatches(claims, ix); // ...check before signing
    return await sendWithSessionKey(ix);
  },
  { waitForApproval: true }, // over-limit proposals wait for the administrator
);
```

`PolicySigner` always runs in the same order: fetch the authorization, verify it against MAX's published keys, call your signing function, then submit the hash. If verification fails, your function is never called. On EVM, build the transaction with `evmTransactionFromClaims(claims)` and check it with `assertEvmTransactionMatches`.

If you build your own signer, call `verifyAuthorization(signed, await client.agent.keys(), { paymentId, sender })` before signing anything. Pass `{ verify }` with an ed25519 implementation (e.g. `@noble/curves`) where WebCrypto lacks Ed25519.

Administrators handle held proposals with `max.agents.transfers(agentId, { decision: "pending_approval" })`, `approve(paymentId)` and `deny(paymentId, reason)`. Keys are managed with `rotateKey` and `revokeKey`, and `pause` / `resume` turn the agent off and on.

## Also covered

- **Renames:** `max.renames.eligibility(id)`, `request(id, {...})`, `waitFor(id, requestId)`. Paid, with a 72h cooldown.
- **Reserved names:**
  - `max.verifications.startX()`: redirect the full page to `authorizeUrl`; never use a popup.
  - Native apps: `max.verifications.startX({ client: "native" })`, open `authorizeUrl` in an auth session, then pass the deep link's `state`/`code`/`error` to `max.verifications.completeX(...)`.
  - `max.reservedClaims.claim({ name })`: claim once verified.
- **Recipient settings:** `max.ids.setDestination`, `setSettlement` (cross-chain preference) and `updateProfile`.
- **Platform admin:** `max.admin.reservedClaims()`, `approveClaim`, `revokeClaim`.
- **Utilities:** `parseUnits` / `formatUnits` (BigInt, no floats), and `normalizeName` for instant UI validation.

## Development

```bash
npm test                 # unit tests (mocked fetch, real ed25519)
npm run test:e2e         # against a running max-core: MAX_API_URL=http://localhost:8090
npm run build
```
