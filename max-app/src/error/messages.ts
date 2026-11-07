import { isMaxApiError, MaxNetworkError } from '@max/sdk';

/**
 * Friendly copy for API error codes. Anything unmapped falls back to the API's
 * own message, which is always safe to show.
 */
const COPY: Record<string, string> = {
  MAX_ID_TAKEN: 'Someone already claimed that name. Try another.',
  MAX_ID_RESERVED: 'That name is reserved and can’t be claimed.',
  MAX_ID_VERIFICATION_REQUIRED: 'Reserved — it can only be claimed by verifying the matching X account.',
  MAX_ID_LIMIT_REACHED: 'You’ve reached the limit of MAX IDs for this account.',
  MAX_ID_QUARANTINED: 'That name was released recently and is temporarily unavailable.',
  INVALID_MAX_ID: 'Use 3–32 lowercase letters, numbers or hyphens.',
  WALLET_ALREADY_LINKED: 'That wallet is already linked to a MAX account.',
  LAST_WALLET: 'You can’t remove your only wallet. Link another one first so you can still sign in.',
  CHALLENGE_INVALID: 'That sign-in request expired. Try again.',
  SIGNATURE_INVALID: 'The signature didn’t match this wallet. Nothing was signed in.',
  AGENT_LOGIN_NOT_ALLOWED: 'This wallet belongs to an agent. Agents sign in with API keys.',
  NO_COMPATIBLE_WALLET: 'They have no wallet on this network yet.',
  RESOLUTION_STALE: 'Their wallet setup changed since you reviewed it. Review the new address.',
  SELF_PAYMENT: 'That’s your own wallet.',
  INVALID_AMOUNT: 'Check the amount.',
  ASSET_UNAVAILABLE: 'That token couldn’t be verified on this network.',
  CHAIN_NOT_CONFIGURED: 'Payments on this network aren’t enabled yet.',
  RATE_LIMITED: 'Too many attempts. Wait a moment and try again.',
  UNAUTHORIZED: 'Please sign in again.',
  SESSION_INVALID: 'Your session ended. Please sign in again.',
  RENAMES_DISABLED: 'Renames aren’t available right now.',
  RENAME_COOLDOWN: 'This handle was renamed recently. Try again after the cooldown.',
  RENAME_PENDING: 'A rename is already waiting for its fee.',
  RENAME_CONFLICT: 'Someone else is claiming that name right now. Pick another.',
  RENAME_SAME_NAME: 'That’s already this handle’s name.',
  INVALID_FEE_OPTION: 'Pick one of the listed fee options.',
  X_VERIFICATION_DISABLED: 'X verification isn’t set up on this server yet.',
  X_VERIFICATION_REQUIRED: 'Verify the matching X account first.',
  X_VERIFICATION_STALE: 'Your X verification is too old to claim with. Verify again, then claim right away.',
  X_HANDLE_MISMATCH: 'This name is reserved for a different X account.',
  X_ACCOUNT_LINKED_ELSEWHERE: 'That X account is already verified by another MAX account.',
  OAUTH_DENIED: 'You cancelled on X. Nothing changed.',
  OAUTH_STATE_INVALID: 'That X sign-in expired. Start again.',
  OAUTH_SESSION_MISMATCH: 'Finish X verification on the same phone and session you started it on.',
  X_PROVIDER_ERROR: 'X didn’t respond. Try again in a moment.',
  NAME_NOT_RESERVED: 'That name isn’t reserved — claim it normally.',
  RESERVED_CLAIM_EXISTS: 'You’ve already claimed that name.',
  // Marketplace (spec §9) and the name guards it adds.
  MARKET_DISABLED: 'The marketplace isn’t open yet.',
  MARKET_PAUSED: 'Trading is paused right now. Sales already in progress still complete.',
  NOT_LISTABLE: 'This name can’t be sold right now.',
  LISTING_NOT_ACTIVE: 'This listing isn’t available any more. It may have sold, or the seller changed it.',
  NAME_LOCKED: 'A sale is in progress for this name, so it can’t be changed right now.',
  SELF_TRADE: 'You can’t trade with yourself or with an account that shares a wallet with yours.',
  MARKET_RESTRICTED: 'Your account can’t use the marketplace right now.',
  ASSET_NOT_ACCEPTED: 'That token isn’t accepted on the marketplace.',
  INSUFFICIENT_FUNDS: 'The paying wallet doesn’t hold enough to cover this.',
  OFFER_LIMIT: 'You already have an open offer on this name, or 20 open offers in total. Withdraw one first.',
  OFFER_BELOW_MINIMUM: 'That’s below the marketplace minimum.',
  OFFER_UNFUNDED: 'The buyer’s wallet doesn’t cover this offer right now, so it can’t be accepted.',
  SALE_EXPIRED: 'The payment window has closed. If you already sent the payment, MAX will still match it. Otherwise nothing was charged.',
  TX_MISMATCH: 'That transaction doesn’t match this sale’s terms. Nothing was recorded.',
  PENDING_APPROVAL: 'This is waiting for approval by the agent’s administrator.',
  INSUFFICIENT_REGISTRATION_BALANCE: 'Your linked wallets don’t hold enough value to claim a MAX ID.',
  PRICE_UNAVAILABLE: 'We couldn’t price your wallet balances just now. Try again in a minute.',
  NAME_LISTED: 'This name is listed for sale. Delist it before renaming.',
  OWNER_CHANGED: 'This name changed owners while you were working. Reload to see the latest.',
};

export function errorMessage(err: unknown): string {
  if (isMaxApiError(err)) return COPY[err.code] ?? err.message;
  if (err instanceof MaxNetworkError) return 'Can’t reach MAX right now. Check your connection and try again.';
  if (err instanceof Error && err.message) return err.message;
  return 'Something went wrong. Try again.';
}

/** The API's `error.details`, when it sent any. */
export function errorDetails(err: unknown): Record<string, unknown> | undefined {
  return isMaxApiError(err) ? err.details : undefined;
}

export function errorCode(err: unknown): string | undefined {
  return isMaxApiError(err) ? err.code : undefined;
}
