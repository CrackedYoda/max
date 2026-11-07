export { MaxClient, type WalletSigner, type WaitOptions } from "./client.js";
export type { ClientOptions, RequestOptions } from "./http.js";
export { MaxApiError, MaxNetworkError, AuthorizationError, isMaxApiError } from "./errors.js";
export {
  AUTHORIZATION_DOMAIN,
  verifyAuthorization,
  evmTransactionFromClaims,
  assertEvmTransactionMatches,
  assertSolanaTransferMatches,
  type Ed25519Verify,
  type VerifyOptions,
  type EvmTransactionLike,
  type SolanaTransferLike,
} from "./authorization.js";
export { PolicySigner, type PolicySignerOptions, type SignFromClaims } from "./signer.js";
export { formatUnits, parseUnits, displayHandle, normalizeName } from "./units.js";
export { MarketApi } from "./market.js";
export { assertSaleInstructionsMatch, type SaleExpectations } from "./sale-instructions.js";
export * from "./market-types.js";
export * from "./types.js";
