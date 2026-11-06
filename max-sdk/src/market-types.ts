// Marketplace types. They mirror the JSON the API emits (after the
// `{ data }` envelope), including its string-valued amounts.

import type { ISODate } from "./types.js";

export type ListingStatus = "active" | "locked" | "sold" | "cancelled" | "admin_delisted";
export type OfferStatus =
  | "live" | "unfunded" | "accepted" | "declined" | "superseded" | "expired" | "withdrawn" | "lapsed" | "completed";
export type SaleStatus =
  | "pending_review" | "locked" | "submitted" | "confirming" | "completed" | "expired" | "cancelled" | "manual_review" | "reversed";
export type RestrictionKind = "ban" | "offer_suspension";
export type MarketOp = "list" | "update_price" | "buy" | "accept" | "pay";

/** Error codes the marketplace adds (see `MaxApiError.code`). */
export type MarketErrorCode =
  | "MARKET_DISABLED" | "MARKET_PAUSED" | "NOT_LISTABLE" | "LISTING_NOT_ACTIVE" | "SELF_TRADE"
  | "MARKET_RESTRICTED" | "ASSET_NOT_ACCEPTED" | "INSUFFICIENT_FUNDS" | "OFFER_LIMIT"
  | "OFFER_BELOW_MINIMUM" | "OFFER_UNFUNDED" | "SALE_EXPIRED" | "TX_MISMATCH"
  | "NAME_LOCKED" | "INSUFFICIENT_REGISTRATION_BALANCE" | "PRICE_UNAVAILABLE";

/**
 * An agent action that MAX's policy holds for the agent's administrator. The
 * API answers 202 and nothing has happened yet; approve it with
 * `agents.approveMarket(authorizationId)`.
 */
export interface PendingApproval {
  status: "pending_approval";
  authorizationId: string;
  reasons: string[];
}

/** Either the finished result or a held agent action. Narrow with `isPendingApproval`. */
export type MarketResult<T> = T | PendingApproval;

export function isPendingApproval(v: unknown): v is PendingApproval {
  return typeof v === "object" && v !== null && (v as { status?: unknown }).status === "pending_approval";
}

export interface Listing {
  id: string;
  maxId: string;
  handle?: string;
  chain: string;
  /** Asset address (mint / contract) as MAX displays it. */
  asset: string;
  symbol: string;
  /** Decimal string of the asset. */
  price: string;
  priceBaseUnits: string;
  payoutAddress: string;
  status: ListingStatus;
  statusReason?: string;
  expiresAt?: ISODate;
  createdAt: ISODate;
  updatedAt: ISODate;
}

export interface ListingCard {
  listingId: string;
  maxId: string;
  handle: string;
  chain: string;
  asset: string;
  symbol: string;
  price: string;
  priceBaseUnits: string;
  expiresAt?: ISODate;
  createdAt: ISODate;
}

export interface SearchResult {
  listings: ListingCard[];
  nextCursor?: string;
}

export interface SearchParams {
  q?: string;
  minPrice?: string;
  maxPrice?: string;
  sort?: string;
  cursor?: string;
  limit?: number;
}

/** A listing as anyone sees it: no payout wallet. */
export interface PublicListing {
  id: string;
  chain: string;
  asset: string;
  symbol: string;
  price: string;
  status: ListingStatus;
  expiresAt?: ISODate;
  createdAt: ISODate;
}

export interface NameMarketView {
  maxId: string;
  handle: string;
  listable: boolean;
  reason?: string;
  listing?: PublicListing;
  liveOfferCount: number;
  lastSale?: { chain: string; asset: string; symbol: string; price: string; completedAt: ISODate };
}

export interface MarketAsset {
  chain: string;
  asset: string;
  symbol: string;
  decimals: number;
}

export interface MarketEligibility {
  maxId: string;
  listable: boolean;
  reason?: string;
  listableAt: ISODate;
  renameAvailableAt: ISODate;
  feeBps: number;
  minPrice: string;
  assets: MarketAsset[];
}

export interface Offer {
  id: string;
  maxId: string;
  handle?: string;
  fromAddress: string;
  chain: string;
  asset: string;
  symbol: string;
  amount: string;
  amountBaseUnits: string;
  status: OfferStatus;
  declineNote?: string;
  expiresAt: ISODate;
  fundedCheckedAt?: ISODate;
  createdAt: ISODate;
}

/** A live offer as the name's owner sees it: terms, not the buyer's wallet. */
export interface ReceivedOffer {
  id: string;
  maxId: string;
  handle?: string;
  chain: string;
  asset: string;
  symbol: string;
  amount: string;
  amountBaseUnits: string;
  status: OfferStatus;
  expiresAt: ISODate;
  createdAt: ISODate;
}

export interface Sale {
  id: string;
  maxId: string;
  handle?: string;
  listingId?: string;
  offerId?: string;
  buyerAddress: string;
  sellerPayoutAddress: string;
  treasuryAddress: string;
  chain: string;
  /** Asset address (the mint on Solana). */
  asset: string;
  symbol: string;
  /** Decimal strings of the asset. */
  price: string;
  priceBaseUnits: string;
  feeBps: number;
  fee: string;
  sellerReceives: string;
  status: SaleStatus;
  statusReason?: string;
  lockExpiresAt?: ISODate;
  txHash?: string;
  confirmations: number;
  submittedAt?: ISODate;
  completedAt?: ISODate;
  createdAt: ISODate;
}

export interface Quote {
  price: string;
  fee: string;
  sellerReceives: string;
  networkFee: string;
  totalAsset: string;
  totalNative: string;
}

/** One Solana instruction the buyer must sign. Amounts are base units. */
export interface SolanaIx {
  kind: "create_ata_idempotent" | "transfer_checked" | "memo";
  accounts?: Record<string, string>;
  amount?: string;
  /** Omitted by the API when 0. */
  decimals?: number;
  memo?: string;
}

export interface MarketInstructions {
  solana?: SolanaIx[];
}

export interface SaleView {
  sale: Sale;
  quote?: Quote;
  instructions?: MarketInstructions;
}

export interface MarketInbox {
  listings: Listing[];
  sales: Sale[];
  offersReceived: ReceivedOffer[];
  offersMade: Offer[];
}

export interface AgentMarketAuthorization {
  id: string;
  agentId: string;
  action: string;
  decision: "auto_approved" | "pending_approval" | "approved" | "denied";
  reasons: string[];
  policyVersion: number;
  chain: string;
  asset: string;
  amountBaseUnits: string;
  saleId?: string;
  offerId?: string;
  listingId?: string;
  failure?: string;
  decidedAt?: ISODate;
  createdAt: ISODate;
  summary: {
    op: MarketOp;
    name?: string;
    listingId?: string;
    offerId?: string;
    saleId?: string;
    /** Base units (buy, list, update_price). */
    price?: string;
    /** Base units (accept, pay). */
    amount?: string;
  };
}

/** Result of approving or denying a held action. */
export interface AgentActionResult {
  authorization: AgentMarketAuthorization;
  listing?: Listing;
  sale?: SaleView;
}

export interface CreateListingInput {
  maxId: string;
  /** Decimal price in the asset, e.g. "250". */
  price: string;
  chain: string;
  asset: string;
  payoutAddress: string;
  expiresAt?: string;
}

export interface MakeOfferInput {
  name: string;
  amount: string;
  chain: string;
  asset: string;
  fromAddress: string;
  expiresAt?: string;
}

// ---- Admin views: full data, party accounts included ----

export type AdminListing = Listing & { sellerAccountId: string };
export type AdminOffer = Offer & { buyerAccountId: string; updatedAt: ISODate };
export type AdminSale = Sale & {
  sellerAccountId: string;
  buyerAccountId: string;
  quotedNetworkFee: string;
  nextCheckAt?: ISODate;
  updatedAt: ISODate;
};

export interface MarketRestriction {
  id: string;
  accountId: string;
  kind: RestrictionKind;
  until?: ISODate;
  reason: string;
  createdBy?: string;
  createdAt: ISODate;
  liftedAt?: ISODate;
  active: boolean;
}

export interface AdminMarketFilter {
  status?: string;
  accountId?: string;
  name?: string;
  /** At most 100. */
  limit?: number;
}
