import { isMaxApiError, MaxNetworkError } from "@max/sdk";

/**
 * Friendly copy for API error codes. Anything unmapped falls back to the API's
 * own message, which is always safe to show.
 */
const COPY: Record<string, string> = {
  MAX_ID_TAKEN: "Someone already claimed that name. Try another.",
  MAX_ID_RESERVED: "That name is reserved and can’t be claimed.",
  MAX_ID_VERIFICATION_REQUIRED: "Reserved — it can only be claimed by verifying the matching X account.",
  MAX_ID_LIMIT_REACHED: "You’ve reached the limit of MAX IDs for this account.",
  MAX_ID_QUARANTINED: "That name was released recently and is temporarily unavailable.",
  INVALID_MAX_ID: "Use 3–32 lowercase letters, numbers or hyphens.",
  WALLET_ALREADY_LINKED: "That wallet is already linked to a MAX account.",
  LAST_WALLET: "You can’t remove your only wallet. Link another one first so you can still sign in.",
  CHALLENGE_INVALID: "That sign-in request expired. Try again.",
  SIGNATURE_INVALID: "The signature didn’t match this wallet. Nothing was signed in.",
  AGENT_LOGIN_NOT_ALLOWED: "This wallet belongs to an agent. Agents sign in with API keys.",
  NO_COMPATIBLE_WALLET: "They have no wallet on this network yet.",
  RESOLUTION_STALE: "Their wallet setup changed since you reviewed it. Review the new address.",
  SELF_PAYMENT: "That’s your own wallet.",
  INVALID_AMOUNT: "Check the amount.",
  ASSET_UNAVAILABLE: "That token couldn’t be verified on this network.",
  CHAIN_NOT_CONFIGURED: "Payments on this network aren’t enabled yet.",
  RATE_LIMITED: "Too many attempts. Wait a moment and try again.",
  UNAUTHORIZED: "Please sign in again.",
  SESSION_INVALID: "Your session ended. Please sign in again.",
  AGENT_NOT_FOUND: "That agent doesn’t exist or isn’t yours.",
  AGENT_PAUSED: "This agent is paused. Resume it first.",
  APPROVAL_NOT_PENDING: "That request was already decided or has expired.",
  INVALID_POLICY: "Check the limits: amounts must be positive and the per-transfer limit can’t exceed the daily one.",
  INVALID_SCOPE: "Pick at least one valid permission.",
  CREDENTIAL_NOT_FOUND: "That key no longer exists.",
  SENDER_NOT_AGENT_WALLET: "Pay from a wallet linked to this agent.",
  RENAMES_DISABLED: "Renames aren’t available right now.",
  RENAME_COOLDOWN: "This handle was renamed recently. Try again after the cooldown.",
  RENAME_PENDING: "A rename is already waiting for its fee.",
  RENAME_CONFLICT: "Someone else is claiming that name right now. Pick another.",
  RENAME_SAME_NAME: "That’s already this handle’s name.",
  INVALID_FEE_OPTION: "Pick one of the listed fee options.",
  X_VERIFICATION_DISABLED: "X verification isn’t set up on this server yet.",
  X_VERIFICATION_REQUIRED: "Verify the matching X account first.",
  X_VERIFICATION_STALE: "Your X verification is too old to claim with. Verify again, then claim right away.",
  X_HANDLE_MISMATCH: "This name is reserved for a different X account.",
  X_ACCOUNT_LINKED_ELSEWHERE: "That X account is already verified by another MAX account.",
  OAUTH_DENIED: "You cancelled on X. Nothing changed.",
  OAUTH_STATE_INVALID: "That X sign-in expired. Start again.",
  OAUTH_SESSION_MISMATCH: "Finish X verification in the same browser you started it in.",
  X_PROVIDER_ERROR: "X didn’t respond. Try again in a moment.",
  NAME_NOT_RESERVED: "That name isn’t reserved — claim it normally.",
  RESERVED_CLAIM_EXISTS: "You’ve already claimed that name.",
};

export function errorMessage(err: unknown): string {
  if (isMaxApiError(err)) return COPY[err.code] ?? err.message;
  if (err instanceof MaxNetworkError) return "Can’t reach MAX right now. Check your connection and try again.";
  if (err instanceof Error && err.message) return err.message;
  return "Something went wrong. Try again.";
}

export function errorCode(err: unknown): string | undefined {
  return isMaxApiError(err) ? err.code : undefined;
}

/** Copy for a bare error code (e.g. one carried in a redirect's query string). */
export function copyForCode(code: string | null | undefined): string {
  return (code && COPY[code]) || "Something went wrong. Try again.";
}
