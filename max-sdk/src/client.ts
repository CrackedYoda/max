import { verifyAuthorization, type VerifyOptions } from "./authorization.js";
import { HttpClient, enc, idempotencyKey, sleep, type ClientOptions } from "./http.js";
import { MarketApi } from "./market.js";
import type { AdminListing, AdminMarketFilter, AdminOffer, AdminSale, AgentActionResult, AgentMarketAuthorization, MarketRestriction, RestrictionKind } from "./market-types.js";
import {
  TERMINAL_STATUSES,
  type Me,
  type Agent,
  type AgentCredential,
  type AgentDetail,
  type AgentLimit,
  type AgentMarketPolicy,
  type AgentPolicy,
  type AgentScope,
  type AgentTransfer,
  type AuthorizationClaims,
  type AuthorizationKey,
  type Availability,
  type Chain,
  type Challenge,
  type Decision,
  type MaxId,
  type MaxIdType,
  type Namespace,
  type Holding,
  type DestinationTarget,
  type Payment,
  type PrepareTransferInput,
  type Profile,
  type ProposeTransferInput,
  type RenameEligibility,
  type RenameView,
  type ReservedClaim,
  type Resolution,
  type SignedAuthorization,
  type VerifyResult,
  type Wallet,
  type XVerification,
} from "./types.js";

/**
 * Anything that can prove control of a wallet by signing a text message.
 * EVM: return the 0x-hex `personal_sign` signature.
 * Solana: return the base58-encoded ed25519 signature over the UTF-8 bytes.
 */
export interface WalletSigner {
  namespace: Extract<Namespace, "evm" | "solana">;
  address: string;
  signMessage(message: string): Promise<string>;
}

export interface WaitOptions<T> {
  /** Poll interval. Default 2s. */
  intervalMs?: number;
  /** Give up after this long. Default 10 minutes. */
  timeoutMs?: number;
  signal?: AbortSignal;
  onUpdate?: (value: T) => void;
}

export class MaxClient {
  readonly http: HttpClient;
  readonly auth: AuthApi;
  readonly wallets: WalletsApi;
  readonly ids: IdsApi;
  readonly transfers: TransfersApi;
  readonly renames: RenamesApi;
  readonly verifications: VerificationsApi;
  readonly reservedClaims: ReservedClaimsApi;
  /** Manage agents you administer (signed-in human session). */
  readonly agents: AgentsAdminApi;
  /** Agent runtime (API key). */
  readonly agent: AgentRuntimeApi;
  /** Platform administration (accounts listed in ADMIN_ACCOUNT_IDS). */
  readonly admin: PlatformAdminApi;
  /** The name marketplace: search, list, buy, offers, sales. */
  readonly market: MarketApi;

  constructor(options: ClientOptions) {
    this.http = new HttpClient(options);
    this.auth = new AuthApi(this.http);
    this.wallets = new WalletsApi(this.http);
    this.ids = new IdsApi(this.http);
    this.transfers = new TransfersApi(this.http);
    this.renames = new RenamesApi(this.http);
    this.verifications = new VerificationsApi(this.http);
    this.reservedClaims = new ReservedClaimsApi(this.http);
    this.agents = new AgentsAdminApi(this.http);
    this.agent = new AgentRuntimeApi(this.http);
    this.admin = new PlatformAdminApi(this.http);
    this.market = new MarketApi(this.http);
  }

  /** Supported chains. Anything not listed is refused rather than guessed. */
  chains(): Promise<Chain[]> {
    return this.http.get("/v1/chains");
  }

  /** Every token a wallet holds on a chain MAX can send from, with USD prices where known. */
  holdings(chain: string, address: string): Promise<Holding[]> {
    return this.http.get("/v1/holdings", { query: { chain, address } });
  }

  /**
   * Resolve a MAX ID to the verified address for a chain and asset.
   * Fails with NO_COMPATIBLE_WALLET instead of guessing an address.
   */
  resolve(maxId: string, params: { chain: string; asset?: string }): Promise<Resolution> {
    return this.http.get(`/v1/resolve/${enc(maxId)}`, { query: { chain: params.chain, asset: params.asset } });
  }

  profile(maxId: string): Promise<Profile> {
    return this.http.get(`/v1/profiles/${enc(maxId)}`);
  }

  availability(name: string): Promise<Availability> {
    return this.http.get("/v1/ids/availability", { query: { name } });
  }
}

class AuthApi {
  constructor(private readonly http: HttpClient) {}

  /** `name` is required for purpose "claim": the name the signature is for. */
  challenge(input: { purpose: "login" | "link" | "claim"; namespace: Namespace; address: string; name?: string }): Promise<Challenge> {
    return this.http.post("/v1/wallets/challenge", { body: input });
  }

  verify(input: { nonce: string; signature: string; tokenDelivery?: "cookie" | "body" }): Promise<VerifyResult> {
    return this.http.post("/v1/wallets/verify", { body: input });
  }

  /**
   * Sign in with a wallet: challenge → sign the exact message → verify.
   * Native apps pass `{ tokenDelivery: "body" }`: the result's `sessionToken`
   * is then also set on this client; store it in secure storage.
   */
  async login(wallet: WalletSigner, opts: { tokenDelivery?: "cookie" | "body" } = {}): Promise<VerifyResult> {
    const res = await this.prove("login", wallet, opts.tokenDelivery);
    if (res.sessionToken) this.http.setSessionToken(res.sessionToken);
    return res;
  }

  /** Link another wallet to the signed-in account. */
  async linkWallet(wallet: WalletSigner): Promise<VerifyResult> {
    return this.prove("link", wallet);
  }

  private async prove(purpose: "login" | "link", wallet: WalletSigner, tokenDelivery?: "cookie" | "body"): Promise<VerifyResult> {
    const ch = await this.challenge({ purpose, namespace: wallet.namespace, address: wallet.address });
    const signature = await wallet.signMessage(ch.message);
    return this.verify({ nonce: ch.nonce, signature, ...(tokenDelivery ? { tokenDelivery } : {}) });
  }

  me(): Promise<Me> {
    return this.http.get("/v1/auth/me");
  }

  async logout(): Promise<void> {
    await this.http.post("/v1/auth/logout");
    this.http.clearSession();
  }
}

class WalletsApi {
  constructor(private readonly http: HttpClient) {}
  list(): Promise<Wallet[]> {
    return this.http.get("/v1/wallets");
  }
  remove(walletId: string): Promise<void> {
    return this.http.delete(`/v1/wallets/${enc(walletId)}`);
  }
}

class IdsApi {
  constructor(private readonly http: HttpClient) {}

  /**
   * Claims a name. A signed-in person signs a claim message for the name with
   * one of their linked wallets first; agents using an API key don't.
   */
  async claim(input: { name: string; type?: MaxIdType; displayName?: string }, wallet?: WalletSigner): Promise<MaxId> {
    if (!wallet) return this.http.post("/v1/ids", { body: input });
    const ch: Challenge = await this.http.post("/v1/wallets/challenge", {
      body: { purpose: "claim", namespace: wallet.namespace, address: wallet.address, name: input.name },
    });
    const signature = await wallet.signMessage(ch.message);
    return this.http.post("/v1/ids", { body: { ...input, nonce: ch.nonce, signature } });
  }
  list(): Promise<MaxId[]> {
    return this.http.get("/v1/ids");
  }
  get(id: string): Promise<MaxId> {
    return this.http.get(`/v1/ids/${enc(id)}`);
  }
  updateProfile(id: string, input: { displayName?: string; description?: string; avatarUrl?: string }): Promise<MaxId> {
    return this.http.patch(`/v1/ids/${enc(id)}`, { body: input });
  }
  setDestination(id: string, namespace: Namespace, target: DestinationTarget): Promise<MaxId> {
    return this.http.put(`/v1/ids/${enc(id)}/destinations/${enc(namespace)}`, { body: target });
  }
  /** Send one EVM chain (e.g. "bnb") somewhere other than the EVM default. */
  setChainDestination(id: string, chain: string, target: DestinationTarget): Promise<MaxId> {
    return this.http.put(`/v1/ids/${enc(id)}/chain-destinations/${enc(chain)}`, { body: target });
  }
  /** Return one EVM chain to the EVM default. */
  removeChainDestination(id: string, chain: string): Promise<MaxId> {
    return this.http.delete(`/v1/ids/${enc(id)}/chain-destinations/${enc(chain)}`);
  }
  removeDestination(id: string, namespace: Namespace): Promise<MaxId> {
    return this.http.delete(`/v1/ids/${enc(id)}/destinations/${enc(namespace)}`);
  }
  /** Opt into quoted cross-chain conversion when senders pay on a chain you have no wallet for. */
  setSettlement(id: string, input: { chain: string; asset?: string; enabled?: boolean }): Promise<MaxId> {
    return this.http.put(`/v1/ids/${enc(id)}/settlement`, { body: input });
  }
  removeSettlement(id: string): Promise<MaxId> {
    return this.http.delete(`/v1/ids/${enc(id)}/settlement`);
  }
  history(id: string): Promise<{ oldHandle: string; newHandle: string; feePaymentId?: string; changedAt: string }[]> {
    return this.http.get(`/v1/ids/${enc(id)}/history`);
  }
}

class TransfersApi {
  constructor(private readonly http: HttpClient) {}

  /**
   * Create a payment intent with the recipient snapshot locked. Retries are
   * safe: the same idempotency key returns the original intent.
   */
  prepare(input: PrepareTransferInput): Promise<Payment> {
    const body = { ...input, idempotencyKey: input.idempotencyKey ?? idempotencyKey() };
    return this.http.post("/v1/transfers/prepare", { body, retrySafe: true });
  }

  /** Record the sender-signed transaction. Idempotent for the same hash. */
  submit(paymentId: string, txHash: string): Promise<Payment> {
    return this.http.post(`/v1/transfers/${enc(paymentId)}/submit`, { body: { txHash }, retrySafe: true });
  }

  get(paymentId: string): Promise<Payment> {
    return this.http.get(`/v1/transfers/${enc(paymentId)}`);
  }

  list(params: { direction?: "sent" | "received"; limit?: number } = {}): Promise<Payment[]> {
    return this.http.get("/v1/transfers", { query: params });
  }

  /** Poll until the payment reaches a terminal status (or `until` returns true). */
  waitFor(paymentId: string, opts: WaitOptions<Payment> & { until?: (p: Payment) => boolean } = {}): Promise<Payment> {
    const done = opts.until ?? ((p: Payment) => TERMINAL_STATUSES.includes(p.status) || p.status === "EXPIRED");
    return poll(() => this.get(paymentId), done, opts);
  }
}

class RenamesApi {
  constructor(private readonly http: HttpClient) {}

  eligibility(maxId: string): Promise<RenameEligibility> {
    return this.http.get(`/v1/ids/${enc(maxId)}/rename`);
  }

  /** Reserve the new name and get a fee payment to sign. The rename applies once the fee is confirmed. */
  request(maxId: string, input: { newName: string; feeChain: string; feeAsset?: string; senderAddress: string; idempotencyKey?: string }): Promise<RenameView> {
    const body = { ...input, idempotencyKey: input.idempotencyKey ?? idempotencyKey() };
    return this.http.post(`/v1/ids/${enc(maxId)}/rename`, { body, retrySafe: true });
  }

  get(maxId: string, requestId: string): Promise<RenameView> {
    return this.http.get(`/v1/ids/${enc(maxId)}/rename/${enc(requestId)}`);
  }

  waitFor(maxId: string, requestId: string, opts: WaitOptions<RenameView> = {}): Promise<RenameView> {
    return poll(() => this.get(maxId, requestId), (v) => v.request.status !== "awaiting_payment", opts);
  }
}

class VerificationsApi {
  constructor(private readonly http: HttpClient) {}

  /**
   * Start X verification. Send the browser to `authorizeUrl` with a full-page
   * redirect in the same session; MAX's callback redirects back to your app.
   *
   * Native apps pass `{ client: "native" }`: the callback then hands `state`
   * and `code` to the app's deep link, and the app finishes with `completeX`.
   */
  startX(opts: { client?: "web" | "native" } = {}): Promise<{ authorizeUrl: string; expiresAt: string }> {
    return this.http.post("/v1/verifications/x/start", opts.client ? { body: { client: opts.client } } : undefined);
  }
  /** Finish a native X flow with the query from the deep link (same session that started it). */
  completeX(input: { state: string; code?: string; error?: string }): Promise<{ verification: XVerification; renewedClaims: number }> {
    return this.http.post("/v1/verifications/x/complete", { body: input });
  }
  list(): Promise<XVerification[]> {
    return this.http.get("/v1/verifications");
  }
  revoke(id: string): Promise<void> {
    return this.http.delete(`/v1/verifications/${enc(id)}`);
  }
}

class ReservedClaimsApi {
  constructor(private readonly http: HttpClient) {}
  /** Claim a reserved name with a fresh X verification. `status` is "pending_review" for brand names. */
  claim(input: { name: string; displayName?: string }): Promise<ReservedClaim> {
    return this.http.post("/v1/reserved-claims", { body: input });
  }
  list(): Promise<ReservedClaim[]> {
    return this.http.get("/v1/reserved-claims");
  }
}

class AgentsAdminApi {
  constructor(private readonly http: HttpClient) {}

  create(input: { name: string; description?: string }): Promise<AgentDetail> {
    return this.http.post("/v1/agents", { body: input });
  }
  list(): Promise<Agent[]> {
    return this.http.get("/v1/agents");
  }
  get(agentId: string): Promise<AgentDetail> {
    return this.http.get(`/v1/agents/${enc(agentId)}`);
  }
  update(agentId: string, input: { name?: string; description?: string; status?: "active" | "paused" }): Promise<Agent> {
    return this.http.patch(`/v1/agents/${enc(agentId)}`, { body: input });
  }
  pause(agentId: string): Promise<Agent> {
    return this.update(agentId, { status: "paused" });
  }
  resume(agentId: string): Promise<Agent> {
    return this.update(agentId, { status: "active" });
  }
  /**
   * Replace the policy. Omit `market` to keep the current selling policy;
   * send it to replace it.
   */
  setPolicy(
    agentId: string,
    policy: { limits: Pick<AgentLimit, "chain" | "asset" | "maxPerTransfer" | "maxPer24h">[]; allowedRecipients?: string[]; allowAnyRecipient?: boolean; market?: AgentMarketPolicy },
  ): Promise<AgentPolicy> {
    return this.http.put(`/v1/agents/${enc(agentId)}/policy`, { body: policy });
  }
  /** The returned `secret` is shown once. */
  issueKey(agentId: string, input: { name?: string; scopes: AgentScope[]; expiresInDays?: number }): Promise<AgentCredential> {
    return this.http.post(`/v1/agents/${enc(agentId)}/credentials`, { body: input });
  }
  rotateKey(agentId: string, credentialId: string): Promise<AgentCredential> {
    return this.http.post(`/v1/agents/${enc(agentId)}/credentials/${enc(credentialId)}/rotate`);
  }
  revokeKey(agentId: string, credentialId: string): Promise<void> {
    return this.http.delete(`/v1/agents/${enc(agentId)}/credentials/${enc(credentialId)}`);
  }
  claimId(agentId: string, input: { name: string; displayName?: string }): Promise<{ id: string; name: string; handle: string; type: MaxIdType }> {
    return this.http.post(`/v1/agents/${enc(agentId)}/ids`, { body: input });
  }
  /** Link the agent's receiving wallet; `wallet` signs as the agent's wallet. */
  async linkWallet(agentId: string, wallet: WalletSigner): Promise<Wallet> {
    const ch: Challenge = await this.http.post(`/v1/agents/${enc(agentId)}/wallets/challenge`, {
      body: { namespace: wallet.namespace, address: wallet.address },
    });
    const signature = await wallet.signMessage(ch.message);
    return this.http.post(`/v1/agents/${enc(agentId)}/wallets/verify`, { body: { nonce: ch.nonce, signature } });
  }
  transfers(agentId: string, params: { decision?: Decision } = {}): Promise<AgentTransfer[]> {
    return this.http.get(`/v1/agents/${enc(agentId)}/transfers`, { query: params });
  }
  approve(paymentId: string): Promise<AgentTransfer> {
    return this.http.post(`/v1/agent-approvals/${enc(paymentId)}/approve`);
  }
  deny(paymentId: string, reason?: string): Promise<AgentTransfer> {
    return this.http.post(`/v1/agent-approvals/${enc(paymentId)}/deny`, { body: { reason } });
  }
  /** Market actions the agent's policy held (or decided), newest first. */
  marketAuthorizations(agentId: string, params: { decision?: Decision } = {}): Promise<AgentMarketAuthorization[]> {
    return this.http.get(`/v1/agents/${enc(agentId)}/market-authorizations`, { query: params });
  }
  /** Approve a held market action; it runs now and the result carries the listing or sale. */
  approveMarket(authorizationId: string): Promise<AgentActionResult> {
    return this.http.post(`/v1/agent-approvals/market/${enc(authorizationId)}/approve`);
  }
  denyMarket(authorizationId: string): Promise<AgentActionResult> {
    return this.http.post(`/v1/agent-approvals/market/${enc(authorizationId)}/deny`);
  }
}

class AgentRuntimeApi {
  private keyCache?: { keys: AuthorizationKey[]; at: number };

  constructor(private readonly http: HttpClient) {}

  me(): Promise<{ agentId: string; credentialId: string; scopes: AgentScope[] }> {
    return this.http.get("/v1/agent/me");
  }

  /**
   * Propose a transfer. MAX's policy decides: `auto_approved` comes with a
   * signed authorization, `pending_approval` waits for your administrator.
   * The `instruction` text is audit-only and never grants authority.
   */
  propose(input: ProposeTransferInput): Promise<AgentTransfer> {
    const body = { ...input, idempotencyKey: input.idempotencyKey ?? idempotencyKey() };
    return this.http.post("/v1/agent/transfers", { body, retrySafe: true });
  }

  get(paymentId: string): Promise<AgentTransfer> {
    return this.http.get(`/v1/agent/transfers/${enc(paymentId)}`);
  }

  authorization(paymentId: string): Promise<SignedAuthorization> {
    return this.http.get(`/v1/agent/transfers/${enc(paymentId)}/authorization`);
  }

  submit(paymentId: string, txHash: string): Promise<Payment> {
    return this.http.post(`/v1/transfers/${enc(paymentId)}/submit`, { body: { txHash }, retrySafe: true });
  }

  /** Poll until a held proposal is approved or denied by the administrator. */
  waitForDecision(paymentId: string, opts: WaitOptions<AgentTransfer> = {}): Promise<AgentTransfer> {
    return poll(
      () => this.get(paymentId),
      (t) => t.authorization.decision !== "pending_approval" || t.payment.status === "EXPIRED" || t.payment.status === "FAILED",
      opts,
    );
  }

  /** MAX's public authorization keys (cached for 5 minutes). */
  async keys(force = false): Promise<AuthorizationKey[]> {
    if (!force && this.keyCache && Date.now() - this.keyCache.at < 5 * 60_000) return this.keyCache.keys;
    const keys: AuthorizationKey[] = await this.http.get("/v1/agent-authorizations/keys");
    this.keyCache = { keys, at: Date.now() };
    return keys;
  }

  /** Fetch and verify the authorization for a payment, returning its claims. */
  async verifiedClaims(paymentId: string, opts: Omit<VerifyOptions, "paymentId"> = {}): Promise<AuthorizationClaims> {
    const signed = await this.authorization(paymentId);
    let keys = await this.keys();
    if (!keys.some((k) => k.keyId === signed.keyId)) keys = await this.keys(true); // key rotated
    return verifyAuthorization(signed, keys, { ...opts, paymentId });
  }
}

class PlatformAdminApi {
  constructor(private readonly http: HttpClient) {}
  reservedClaims(params: { status?: ReservedClaim["status"] } = {}): Promise<ReservedClaim[]> {
    return this.http.get("/v1/admin/reserved-claims", { query: params });
  }
  approveClaim(id: string, note?: string): Promise<ReservedClaim> {
    return this.http.post(`/v1/admin/reserved-claims/${enc(id)}/approve`, { body: { note } });
  }
  denyClaim(id: string, note?: string): Promise<ReservedClaim> {
    return this.http.post(`/v1/admin/reserved-claims/${enc(id)}/deny`, { body: { note } });
  }
  revokeClaim(id: string, note?: string): Promise<ReservedClaim> {
    return this.http.post(`/v1/admin/reserved-claims/${enc(id)}/revoke`, { body: { note } });
  }
  revokeVerification(id: string, note?: string): Promise<void> {
    return this.http.post(`/v1/admin/verifications/${enc(id)}/revoke`, { body: { note } });
  }

  // ---- Marketplace administration (/v1/admin/market) ----

  marketListings(params: AdminMarketFilter = {}): Promise<AdminListing[]> {
    return this.http.get("/v1/admin/market/listings", { query: { ...params } });
  }
  marketOffers(params: AdminMarketFilter = {}): Promise<AdminOffer[]> {
    return this.http.get("/v1/admin/market/offers", { query: { ...params } });
  }
  marketSales(params: AdminMarketFilter = {}): Promise<AdminSale[]> {
    return this.http.get("/v1/admin/market/sales", { query: { ...params } });
  }
  marketDelist(listingId: string, reason?: string): Promise<AdminListing> {
    return this.http.post(`/v1/admin/market/listings/${enc(listingId)}/delist`, { body: { reason } });
  }
  marketApproveSale(saleId: string, reason?: string): Promise<AdminSale> {
    return this.http.post(`/v1/admin/market/sales/${enc(saleId)}/approve`, { body: { reason } });
  }
  marketDenySale(saleId: string, reason?: string): Promise<AdminSale> {
    return this.http.post(`/v1/admin/market/sales/${enc(saleId)}/deny`, { body: { reason } });
  }
  marketCancelSale(saleId: string, reason?: string): Promise<AdminSale> {
    return this.http.post(`/v1/admin/market/sales/${enc(saleId)}/cancel`, { body: { reason } });
  }
  marketReverseSale(saleId: string, reason?: string): Promise<AdminSale> {
    return this.http.post(`/v1/admin/market/sales/${enc(saleId)}/reverse`, { body: { reason } });
  }
  /** Active restrictions, or all of them with `all: true`. */
  marketRestrictions(params: { accountId?: string; all?: boolean; limit?: number } = {}): Promise<MarketRestriction[]> {
    return this.http.get("/v1/admin/market/restrictions", { query: { ...params } });
  }
  marketRestrict(input: { accountId: string; kind: RestrictionKind; until?: string; reason: string }): Promise<MarketRestriction> {
    return this.http.post("/v1/admin/market/restrictions", { body: input });
  }
  marketLiftRestriction(restrictionId: string, reason?: string): Promise<MarketRestriction> {
    return this.http.delete(`/v1/admin/market/restrictions/${enc(restrictionId)}`, { body: { reason } });
  }
}

async function poll<T>(fetchOnce: () => Promise<T>, done: (v: T) => boolean, opts: WaitOptions<T>): Promise<T> {
  const deadline = Date.now() + (opts.timeoutMs ?? 10 * 60_000);
  for (;;) {
    const v = await fetchOnce();
    opts.onUpdate?.(v);
    if (done(v)) return v;
    if (Date.now() >= deadline) throw new Error("MaxClient: timed out waiting");
    await sleep(opts.intervalMs ?? 2000, opts.signal);
  }
}
