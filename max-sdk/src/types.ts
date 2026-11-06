// Wire types for the MAX API. Field names match the JSON the API returns.
// Amounts are always strings: `baseUnits` is an integer string in the asset's
// smallest unit; `display` is a trimmed decimal for humans.

export type Namespace = "evm" | "solana" | "bitcoin";
export type ISODate = string;

export interface Chain {
  key: string;
  name: string;
  namespace: Namespace;
  evmChainId?: number;
  testnet: boolean;
  nativeCoin: string;
  nativeDecimals: number;
  confirmations: number;
  explorerTxUrl: string;
}

export interface AssetRef {
  kind: "native" | "token";
  address?: string;
}

export interface Amount {
  display: string;
  baseUnits: string;
}

// ---- Identity ----

export type MaxIdType = "human" | "agent";

export interface Availability {
  name: string;
  handle: string;
  available: boolean;
  reason?: "taken" | "pending_rename" | "quarantined" | "reserved" | "invalid";
  detail?: string;
  claimPolicy?: "blocked" | "x_oauth" | "manual_review";
  xHandle?: string;
}

export interface Profile {
  maxId: string;
  name: string;
  handle: string;
  type: MaxIdType;
  displayName: string;
  description: string;
  avatarUrl: string;
  chains: { chain: Chain; reachable: boolean }[];
  settlement?: { chain: Chain; asset: AssetRef };
  /** Set only when ownership changed in the last 30 days (a marketplace sale). */
  ownerChangedAt?: string;
}

export interface Resolution {
  maxId: string;
  handle: string;
  type: MaxIdType;
  displayName: string;
  chain: Chain;
  asset: AssetRef;
  address: string;
  /** Pass to transfers.prepare so the sender signs exactly what they reviewed. */
  resolutionVersion: number;
  resolvedAt: ISODate;
  notes: string[];
  /** Set only when ownership changed in the last 30 days; show it before paying. */
  ownerChangedAt?: string;
}

export interface Destination {
  namespace: Namespace;
  walletId: string;
  address: string;
  updatedAt: ISODate;
}

export interface Settlement {
  chain: string;
  asset: string;
  enabled: boolean;
  updatedAt: ISODate;
}

export interface MaxId {
  id: string;
  name: string;
  handle: string;
  type: MaxIdType;
  status: "active" | "locked" | "quarantined";
  displayName: string;
  description: string;
  avatarUrl: string;
  resolutionVersion: number;
  renameAvailableAt: ISODate;
  createdAt: ISODate;
  destinations: Destination[];
  settlement: Settlement | null;
}

export interface Account {
  id: string;
  type: "human" | "agent" | "organization";
  status: "active" | "suspended";
  createdAt: ISODate;
}

export interface Wallet {
  id: string;
  namespace: Namespace;
  address: string;
  verifiedAt: ISODate;
  createdAt?: ISODate;
}

export interface Challenge {
  nonce: string;
  /** Sign exactly this text with the wallet. */
  message: string;
  expiresAt: ISODate;
}

export interface VerifyResult {
  purpose: "login" | "link";
  account: Account;
  wallet: Wallet;
  accountCreated: boolean;
  sessionExpiresAt?: ISODate;
  /** Only when requested with tokenDelivery "body" (native apps). */
  sessionToken?: string;
}

// ---- Payments ----

export type PaymentStatus =
  | "CREATED"
  | "QUOTED"
  | "AWAITING_SENDER_SIGNATURE"
  | "SUBMITTED"
  | "CONFIRMING"
  | "COMPLETED"
  | "EXPIRED"
  | "FAILED"
  | "REFUND_PENDING"
  | "REFUNDED"
  | "MANUAL_REVIEW";

/** Statuses after which a payment never changes again on its own. */
export const TERMINAL_STATUSES: readonly PaymentStatus[] = ["COMPLETED", "FAILED", "REFUNDED", "MANUAL_REVIEW"];

export interface EvmInstruction {
  chainId: number;
  from: string;
  to: string;
  /** Hex-encoded wei. */
  value: string;
  data: string;
}

export interface SolanaInstruction {
  kind: "system_transfer" | "spl_transfer_checked";
  from: string;
  to: string;
  lamports?: string;
  mint?: string;
  tokenProgram?: string;
  decimals?: number;
  amount?: string;
  sourceTokenAccount?: string;
  destinationTokenAccount?: string;
  destinationTokenAccountExists?: boolean;
}

export interface Instructions {
  evm?: EvmInstruction;
  solana?: SolanaInstruction;
}

export interface CrossChainRoute {
  provider: string;
  swapType: "EXACT_INPUT" | "EXACT_OUTPUT";
  destinationChain: Chain;
  destinationAsset: PaymentAsset;
  depositAddress: string;
  refundTo: string;
  amountIn: Amount;
  expectedAmountOut: Amount;
  minAmountOut: Amount;
  amountInUsd: string;
  amountOutUsd: string;
  slippageBps: number;
  timeEstimateSeconds: number;
  quoteDeadline: ISODate;
  depositConfirmed: boolean;
  providerStatus?: string;
  amountOut?: Amount;
  refundedAmount?: Amount;
  refundReason?: string;
  destinationTxs: { hash: string; explorerUrl: string }[];
}

export interface PaymentAsset {
  kind: "native" | "token";
  address?: string;
  symbol?: string;
  decimals: number;
  verified: boolean;
  tokenProgram?: string;
}

export interface PaymentEvent {
  type: string;
  fromStatus?: PaymentStatus;
  toStatus: PaymentStatus;
  detail?: Record<string, unknown>;
  createdAt: ISODate;
}

export interface Payment {
  id: string;
  purpose: "transfer" | "rename_fee";
  status: PaymentStatus;
  statusReason?: string;
  route: "direct" | "cross_chain";
  recipient: { maxId?: string; handle: string; address: string; resolutionVersion: number };
  crossChain?: CrossChainRoute;
  senderAddress: string;
  chain: Chain;
  asset: PaymentAsset;
  amount: Amount;
  txHash?: string;
  explorerUrl?: string;
  confirmations: number;
  requiredConfirmations: number;
  expiresAt: ISODate;
  submittedAt?: ISODate;
  completedAt?: ISODate;
  createdAt: ISODate;
  /** Present while the payment is waiting for the sender to sign. */
  instructions?: Instructions;
  warnings?: string[];
  replayed?: boolean;
  events?: PaymentEvent[];
}

export interface PrepareTransferInput {
  recipient: string;
  chain: string;
  /** "native" or a contract/mint address. Never a symbol. */
  asset?: string;
  amount: string;
  senderAddress: string;
  /** Generated for you if omitted; reuse it when retrying the same payment. */
  idempotencyKey?: string;
  resolutionVersion?: number;
  destinationChain?: string;
  destinationAsset?: string;
  amountType?: "input" | "output";
  slippageBps?: number;
}

// ---- Agents ----

export type AgentScope =
  | "payments:read"
  | "payments:propose"
  | "payments:submit"
  | "authorizations:read"
  | "wallets:link"
  | "ids:write"
  | "market:read"
  | "market:sell"
  | "market:buy";

export type Decision = "auto_approved" | "pending_approval" | "approved" | "denied";

export interface SignedAuthorization {
  algorithm: "ed25519";
  keyId: string;
  /** Base64 of the exact signed bytes. Verify before parsing. */
  payload: string;
  signature: string;
}

export interface AuthorizationClaims {
  version: number;
  paymentId: string;
  agentId: string;
  credentialId: string;
  decision: Decision;
  policyVersion: number;
  chain: string;
  sender: string;
  payTo: string;
  recipient: string;
  recipientAddress: string;
  route: "direct" | "cross_chain";
  asset: { kind: "native" | "token"; address?: string; decimals: number };
  amountBaseUnits: string;
  instructions: {
    evm?: { chainId: number; to: string; value: string; data: string };
    solana?: Omit<SolanaInstruction, "from" | "destinationTokenAccountExists">;
  };
  issuedAt: ISODate;
  expiresAt: ISODate;
}

export interface AgentTransfer {
  payment: Payment;
  authorization: {
    decision: Decision;
    reasons: string[];
    policyVersion: number;
    instruction?: string;
    decidedAt?: ISODate;
    createdAt: ISODate;
  };
  instructions?: Instructions;
  signedAuthorization?: SignedAuthorization;
  warnings?: string[];
  replayed?: boolean;
}

export interface ProposeTransferInput extends Omit<PrepareTransferInput, "resolutionVersion" | "slippageBps"> {
  /** The natural-language request. Logged for audit; it never authorizes anything. */
  instruction?: string;
}

export interface AuthorizationKey {
  keyId: string;
  algorithm: "ed25519";
  publicKey: string;
  domain: string;
}

export interface Agent {
  id: string;
  name: string;
  description: string;
  status: "active" | "paused";
  createdAt: ISODate;
}

export interface AgentLimit {
  chain: string;
  asset: string;
  maxPerTransfer: string;
  maxPer24h: string;
  maxPerTransferBaseUnits?: string;
  maxPer24hBaseUnits?: string;
}

export interface AgentPolicy {
  version: number;
  limits: AgentLimit[];
  allowedRecipients: string[];
  allowAnyRecipient: boolean;
  /** Selling policy for marketplace actions. */
  market: AgentMarketPolicy;
  updatedAt: ISODate;
}

export interface AgentMarketPolicy {
  canSell: boolean;
  /** Per-asset floors an agent may sell a name for; `amount` is in base units. */
  minSalePrices: { chain: string; asset: string; amount: string }[];
}

export interface AgentCredential {
  id: string;
  name: string;
  prefix: string;
  scopes: AgentScope[];
  expiresAt?: ISODate;
  lastUsedAt?: ISODate;
  revokedAt?: ISODate;
  createdAt: ISODate;
  /** Only present when a key is issued or rotated. Store it now; it is never shown again. */
  secret?: string;
}

export interface AgentDetail extends Agent {
  policy: AgentPolicy;
  credentials: AgentCredential[];
  wallets: Wallet[];
  ids: { id: string; handle: string; type: MaxIdType; status: string }[];
}

// ---- Renames ----

export interface FeeOption {
  chain: Chain;
  asset: string;
  symbol: string;
  amount: string;
  baseUnits: string;
}

export interface RenameRequest {
  id: string;
  status: "awaiting_payment" | "completed" | "failed" | "expired";
  oldHandle: string;
  newHandle: string;
  failureReason?: string;
  createdAt: ISODate;
  completedAt?: ISODate;
}

export interface RenameEligibility {
  handle: string;
  eligible: boolean;
  reason?: string;
  renameAvailableAt: ISODate;
  cooldownHours: number;
  fees: FeeOption[];
  pending?: RenameRequest;
}

export interface RenameView {
  request: RenameRequest;
  feePayment: Payment;
  instructions?: Instructions;
  warnings?: string[];
}

// ---- Reserved names / X verification ----

export interface XVerification {
  id: string;
  provider: "x";
  username: string;
  displayName: string;
  verifiedAt: ISODate;
  revokedAt?: ISODate;
  revokeReason?: string;
}

export interface ReservedClaim {
  id: string;
  handle: string;
  status: "pending_review" | "active" | "lapsed" | "denied" | "revoked";
  xUsername: string;
  maxId?: string;
  verifiedUntil?: ISODate;
  reviewNote?: string;
  decidedAt?: ISODate;
  createdAt: ISODate;
}
