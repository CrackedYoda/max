import { HttpClient, enc, idempotencyKey } from "./http.js";
import type {
  CreateListingInput,
  Listing,
  MakeOfferInput,
  MarketEligibility,
  MarketInbox,
  MarketResult,
  NameMarketView,
  Offer,
  SaleView,
  SearchParams,
  SearchResult,
} from "./market-types.js";

/**
 * The name marketplace. Each method is one API route.
 *
 * Methods an agent can call (buy, list, updateListing, acceptOffer, payOffer) may
 * return a `PendingApproval` instead of the result: the agent's policy held
 * the action for its administrator and nothing has happened yet. Narrow with
 * `isPendingApproval(result)`. Offers are never held: an offer spends nothing
 * until it is paid, so the policy checks `payOffer` instead.
 */
export class MarketApi {
  constructor(private readonly http: HttpClient) {}

  /** Public search of active listings. */
  search(params: SearchParams = {}): Promise<SearchResult> {
    return this.http.get("/v1/market/listings", { query: { ...params } });
  }

  /** Public market view of one name: listing, live offer count, last sale. */
  getName(name: string): Promise<NameMarketView> {
    return this.http.get(`/v1/market/names/${enc(name)}`);
  }

  /** Whether a MAX ID you own can be listed, and the assets, fee and minimum price. */
  eligibility(maxId: string): Promise<MarketEligibility> {
    return this.http.get(`/v1/market/eligibility/${enc(maxId)}`);
  }

  list(input: CreateListingInput): Promise<MarketResult<Listing>> {
    return this.http.post("/v1/market/listings", { body: input });
  }

  updateListing(listingId: string, price: string): Promise<MarketResult<Listing>> {
    return this.http.patch(`/v1/market/listings/${enc(listingId)}`, { body: { price } });
  }

  delist(listingId: string): Promise<void> {
    return this.http.delete(`/v1/market/listings/${enc(listingId)}`);
  }

  /**
   * Lock a listing for the buyer and get the sale with its quote and the
   * instructions to sign. Retries are safe: the idempotency key (generated
   * if omitted) makes a replay return the original sale.
   */
  buy(listingId: string, input: { fromAddress: string; idempotencyKey?: string }): Promise<MarketResult<SaleView>> {
    const body = { fromAddress: input.fromAddress, idempotencyKey: input.idempotencyKey ?? idempotencyKey() };
    return this.http.post(`/v1/market/listings/${enc(listingId)}/buy`, { body, retrySafe: true });
  }

  /**
   * Make an offer on a name, listed or not. Never held for approval (offers
   * spend nothing until paid); the `MarketResult` type is kept for
   * compatibility.
   */
  offer(input: MakeOfferInput): Promise<MarketResult<Offer>> {
    return this.http.post("/v1/market/offers", { body: input });
  }

  withdrawOffer(offerId: string): Promise<void> {
    return this.http.delete(`/v1/market/offers/${enc(offerId)}`);
  }

  /** Accept an offer on your name. `payoutAddress` is needed only when the name has no listing. */
  acceptOffer(offerId: string, input: { payoutAddress?: string } = {}): Promise<MarketResult<SaleView>> {
    return this.http.post(`/v1/market/offers/${enc(offerId)}/accept`, { body: input });
  }

  declineOffer(offerId: string, input: { note?: string } = {}): Promise<void> {
    return this.http.post(`/v1/market/offers/${enc(offerId)}/decline`, { body: input });
  }

  /** The buyer pays an accepted offer: returns the sale with the instructions to sign. */
  payOffer(offerId: string, input: { idempotencyKey?: string } = {}): Promise<MarketResult<SaleView>> {
    return this.http.post(`/v1/market/offers/${enc(offerId)}/pay`, { body: input });
  }

  /** Record the signed transaction's signature for a sale. */
  submit(saleId: string, txHash: string): Promise<SaleView> {
    return this.http.post(`/v1/market/sales/${enc(saleId)}/submit`, { body: { txHash } });
  }

  getSale(saleId: string): Promise<SaleView> {
    return this.http.get(`/v1/market/sales/${enc(saleId)}`);
  }

  /** Your listings, sales and offers. */
  inbox(): Promise<MarketInbox> {
    return this.http.get("/v1/market/me");
  }
}
