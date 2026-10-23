import { AuthorizationError } from "./errors.js";
import type { AuthorizationClaims, AuthorizationKey, SignedAuthorization } from "./types.js";

/** Prefix MAX signs over, so an authorization can't be replayed as anything else. */
export const AUTHORIZATION_DOMAIN = "max-agent-authorization/v1\n";

/** An ed25519 verifier. Defaults to WebCrypto; pass your own (e.g. @noble/curves) where unavailable. */
export type Ed25519Verify = (publicKey: Uint8Array, message: Uint8Array, signature: Uint8Array) => Promise<boolean>;

export interface VerifyOptions {
  /** Defaults to now. */
  now?: Date;
  /** Require the authorization to be for this payment. */
  paymentId?: string;
  /** Require the authorization to be for this sending wallet (your signer's address). */
  sender?: string;
  verify?: Ed25519Verify;
}

/**
 * Verify a MAX-signed agent authorization and return its claims.
 *
 * This is the check an agent's signer must run before signing anything: the
 * signature must be valid for one of MAX's published keys, unexpired, and
 * (optionally) for the expected payment and sender. Claims are parsed only
 * after the signature over the exact bytes has been verified.
 */
export async function verifyAuthorization(
  signed: SignedAuthorization,
  keys: AuthorizationKey[] | AuthorizationKey | Uint8Array,
  opts: VerifyOptions = {},
): Promise<AuthorizationClaims> {
  if (signed.algorithm !== "ed25519") throw new AuthorizationError(`unsupported algorithm ${signed.algorithm}`);
  const publicKey = selectKey(signed, keys);

  let payload: Uint8Array;
  let signature: Uint8Array;
  try {
    payload = fromBase64(signed.payload);
    signature = fromBase64(signed.signature);
  } catch {
    throw new AuthorizationError("authorization is not valid base64");
  }
  const message = concat(new TextEncoder().encode(AUTHORIZATION_DOMAIN), payload);
  const ok = await (opts.verify ?? webCryptoVerify)(publicKey, message, signature);
  if (!ok) throw new AuthorizationError("authorization signature is invalid");

  let claims: AuthorizationClaims;
  try {
    claims = JSON.parse(new TextDecoder().decode(payload));
  } catch {
    throw new AuthorizationError("authorization payload is not JSON");
  }
  if (claims.version !== 1) throw new AuthorizationError(`unsupported authorization version ${claims.version}`);
  if (!(Date.parse(claims.expiresAt) > (opts.now ?? new Date()).getTime())) {
    throw new AuthorizationError("authorization has expired");
  }
  if (claims.decision !== "auto_approved" && claims.decision !== "approved") {
    throw new AuthorizationError(`authorization decision is ${claims.decision}`);
  }
  if (opts.paymentId && claims.paymentId !== opts.paymentId) {
    throw new AuthorizationError("authorization is for a different payment");
  }
  if (opts.sender && !sameAddress(claims.sender, opts.sender)) {
    throw new AuthorizationError("authorization is for a different sending wallet");
  }
  return claims;
}

function selectKey(signed: SignedAuthorization, keys: AuthorizationKey[] | AuthorizationKey | Uint8Array): Uint8Array {
  if (keys instanceof Uint8Array) return keys;
  const list = Array.isArray(keys) ? keys : [keys];
  const key = list.find((k) => k.keyId === signed.keyId);
  if (!key) throw new AuthorizationError(`no MAX key with id ${signed.keyId}; refresh keys`);
  if (key.domain && key.domain !== AUTHORIZATION_DOMAIN) throw new AuthorizationError("unexpected authorization domain");
  return fromBase64(key.publicKey);
}

const webCryptoVerify: Ed25519Verify = async (publicKey, message, signature) => {
  const subtle = globalThis.crypto?.subtle;
  if (!subtle) throw new AuthorizationError("WebCrypto unavailable; pass options.verify");
  let key: CryptoKey;
  try {
    key = await subtle.importKey("raw", publicKey as BufferSource, { name: "Ed25519" }, false, ["verify"]);
  } catch (err) {
    throw new AuthorizationError(`this runtime's WebCrypto lacks Ed25519; pass options.verify (${(err as Error).message})`);
  }
  return subtle.verify({ name: "Ed25519" }, key, signature as BufferSource, message as BufferSource);
};

// ---- Transaction checks ----

export interface EvmTransactionLike {
  to?: string | null;
  value?: bigint | number | string | null;
  data?: string | null;
  chainId?: number | bigint | null;
}

/** The exact EVM transaction an approved authorization permits. Build from this, then sign. */
export function evmTransactionFromClaims(claims: AuthorizationClaims): { to: `0x${string}`; value: bigint; data: `0x${string}`; chainId: number } {
  const e = claims.instructions.evm;
  if (!e) throw new AuthorizationError("authorization has no EVM instructions");
  return { to: e.to as `0x${string}`, value: BigInt(e.value), data: e.data as `0x${string}`, chainId: e.chainId };
}

/** Throw unless tx matches the authorized EVM instruction exactly. */
export function assertEvmTransactionMatches(claims: AuthorizationClaims, tx: EvmTransactionLike): void {
  const e = claims.instructions.evm;
  if (!e) throw new AuthorizationError("authorization has no EVM instructions");
  if (!tx.to || tx.to.toLowerCase() !== e.to.toLowerCase()) throw new AuthorizationError("transaction recipient differs from authorization");
  if (BigInt(tx.value ?? 0) !== BigInt(e.value)) throw new AuthorizationError("transaction value differs from authorization");
  if ((tx.data ?? "0x").toLowerCase() !== e.data.toLowerCase()) throw new AuthorizationError("transaction data differs from authorization");
  if (tx.chainId != null && Number(tx.chainId) !== e.chainId) throw new AuthorizationError("transaction chain differs from authorization");
}

export interface SolanaTransferLike {
  kind: "system_transfer" | "spl_transfer_checked";
  /** System transfer recipient. */
  to?: string;
  lamports?: bigint | number | string;
  mint?: string;
  tokenProgram?: string;
  amount?: bigint | number | string;
  decimals?: number;
  destinationTokenAccount?: string;
}

/** Throw unless the transfer instruction you built matches the authorized Solana instruction. */
export function assertSolanaTransferMatches(claims: AuthorizationClaims, t: SolanaTransferLike): void {
  const s = claims.instructions.solana;
  if (!s) throw new AuthorizationError("authorization has no Solana instructions");
  if (t.kind !== s.kind) throw new AuthorizationError("transfer kind differs from authorization");
  if (s.kind === "system_transfer") {
    if (t.to !== s.to) throw new AuthorizationError("transfer recipient differs from authorization");
    if (BigInt(t.lamports ?? -1) !== BigInt(s.lamports ?? -2)) throw new AuthorizationError("lamports differ from authorization");
    return;
  }
  if (t.mint !== s.mint) throw new AuthorizationError("mint differs from authorization");
  if (t.tokenProgram !== undefined && t.tokenProgram !== s.tokenProgram) throw new AuthorizationError("token program differs from authorization");
  if (t.destinationTokenAccount !== s.destinationTokenAccount) throw new AuthorizationError("destination token account differs from authorization");
  if (BigInt(t.amount ?? -1) !== BigInt(s.amount ?? -2)) throw new AuthorizationError("amount differs from authorization");
  if (t.decimals !== undefined && t.decimals !== s.decimals) throw new AuthorizationError("decimals differ from authorization");
}

// ---- bytes ----

function sameAddress(a: string, b: string): boolean {
  return a.startsWith("0x") ? a.toLowerCase() === b.toLowerCase() : a === b;
}

function concat(a: Uint8Array, b: Uint8Array): Uint8Array {
  const out = new Uint8Array(a.length + b.length);
  out.set(a);
  out.set(b, a.length);
  return out;
}

export function fromBase64(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}
