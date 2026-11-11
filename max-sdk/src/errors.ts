/** An error response from the MAX API, with its stable machine code. */
export class MaxApiError extends Error {
  override readonly name = "MaxApiError";
  constructor(
    /** Stable code such as "MAX_ID_TAKEN" or "RESOLUTION_STALE". Branch on this, not on message. */
    readonly code: string,
    message: string,
    readonly status: number,
    readonly requestId?: string,
    /** Machine-readable context from the API (e.g. INSUFFICIENT_FUNDS shortfalls). */
    readonly details?: Record<string, unknown>,
  ) {
    super(message);
  }

  /** True for the given code(s). */
  is(...codes: string[]): boolean {
    return codes.includes(this.code);
  }
}

/** The request never produced an API response (network failure, timeout). */
export class MaxNetworkError extends Error {
  override readonly name = "MaxNetworkError";
  constructor(message: string, override readonly cause?: unknown) {
    super(message);
  }
}

/** A signed agent authorization failed verification or did not match a transaction. */
export class AuthorizationError extends Error {
  override readonly name = "AuthorizationError";
}

/**
 * A payment has a signing attempt with no recorded result: a signer crashed
 * or failed mid-way, or another process sharing the attempt store is signing
 * it now. Signing again could pay twice, so PolicySigner refuses. Check the
 * chain for a transaction from the sender before releasing the attempt.
 */
export class SigningOutcomeUnknownError extends Error {
  override readonly name = "SigningOutcomeUnknownError";
  constructor(readonly paymentId: string) {
    super(`payment ${paymentId} has a signing attempt with no recorded result; check the chain before releasing it`);
  }
}

export function isMaxApiError(err: unknown, ...codes: string[]): err is MaxApiError {
  return err instanceof MaxApiError && (codes.length === 0 || codes.includes(err.code));
}
