import { formatUnits, parseUnits } from '@max/sdk';

/**
 * Marketplace money, in integer base units only (no floats), matching max-core.
 * Pure: no React Native or app imports, so it can be checked under plain Node.
 */

/** max-core MARKET_FEE_BPS when the server does not say otherwise. */
export const DEFAULT_FEE_BPS = 1000;

/** A decimal amount the user typed → base units; null when it isn't valid for `decimals`. */
export function toBaseUnits(amount: string, decimals: number): bigint | null {
  try {
    return parseUnits(amount.trim().replace(',', '.'), decimals);
  } catch {
    return null;
  }
}

const group = (whole: string) => whole.replace(/\B(?=(\d{3})+(?!\d))/g, ',');

/** Base units → "1,250.5". */
export function displayUnits(base: bigint | string, decimals: number): string {
  const [whole, frac] = formatUnits(base, decimals).split('.');
  return frac ? `${group(whole ?? '0')}.${frac}` : group(whole ?? '0');
}

/** A decimal string from the API, grouped for display ("1250.5" → "1,250.5"). */
export function displayDecimal(amount: string): string {
  const [whole, frac] = amount.split('.');
  return frac ? `${group(whole ?? '0')}.${frac}` : group(whole ?? '0');
}

/** The marketplace fee: floor(price × feeBps / 10000), like max-core. */
export function feeOf(price: bigint, feeBps: number): bigint {
  return (price * BigInt(feeBps)) / 10000n;
}

/** What the seller receives: price − floor(price × feeBps / 10000). */
export function sellerReceives(price: bigint, feeBps: number): bigint {
  return price - feeOf(price, feeBps);
}

/** "90" for 1000 bps; keeps decimals only when needed ("97.5"). */
export function sellerPercent(feeBps: number): string {
  const bps = 10000 - feeBps;
  return bps % 100 === 0 ? String(bps / 100) : (bps / 100).toFixed(2).replace(/0+$/, '');
}

/** The decimals that turn `price` into `priceBaseUnits`; null when they don't agree. Display only. */
export function decimalsFrom(price: string, priceBaseUnits: string): number | null {
  for (let d = 0; d <= 18; d++) {
    const v = toBaseUnits(price, d);
    if (v !== null && v.toString() === priceBaseUnits) return d;
  }
  return null;
}

/**
 * The base units a sale is pinned to before signing: the price the user saw,
 * converted with decimals from a trusted source (env pin / chain), never the
 * API's. If the API also sent base units for that price and they disagree,
 * the response is inconsistent and nothing may be signed.
 */
export function pinnedPriceBase(shownPrice: string, trustedDecimals: number, serverBase?: string): { base: string } | { error: string } {
  const base = toBaseUnits(shownPrice, trustedDecimals);
  if (base === null || base <= 0n) return { error: 'The price shown isn’t a valid amount for this token, so nothing can be signed.' };
  if (serverBase !== undefined && serverBase !== base.toString()) {
    return { error: 'The amount MAX sent doesn’t match the price shown, so nothing was signed. Reload and try again.' };
  }
  return { base: base.toString() };
}

/**
 * Token decimals from trusted sources only: the env pin for a configured
 * market asset, cross-checked with the mint read through this app's own RPC,
 * or the chain alone. A disagreement blocks signing.
 */
export function trustedDecimals(pinned: number | undefined, onChain: number | undefined): { decimals: number | null; error: string | null } {
  if (pinned != null && onChain != null && pinned !== onChain) {
    return { decimals: null, error: 'This token’s decimals on chain don’t match the app’s configuration, so nothing can be signed.' };
  }
  return { decimals: pinned ?? onChain ?? null, error: null };
}

export type AssetUnits = {
  /** Token decimals (6 for USDC). Unknown → inferred from `amount` when possible. */
  decimals?: number | null;
  symbol: string;
  nativeSymbol?: string;
  nativeDecimals?: number;
  /** The decimal amount the user agreed to. */
  amount?: string;
};

const digits = (v: unknown): string | null => (typeof v === 'string' && /^\d+$/.test(v) ? v : typeof v === 'number' && Number.isInteger(v) ? String(v) : null);

/** INSUFFICIENT_FUNDS details (base units) → "needs X USDC but holds Y USDC". Null when they can't be shown. */
export function insufficientFundsCopy(details: Record<string, unknown> | undefined, u: AssetUnits): string | null {
  if (!details) return null;
  const nativeSym = u.nativeSymbol ?? 'SOL';
  const nativeDec = u.nativeDecimals ?? 9;
  const needA = digits(details.needAsset);
  const haveA = digits(details.haveAsset);
  const needN = digits(details.needNative);
  const haveN = digits(details.haveNative);
  const dec = u.decimals ?? (needA && u.amount ? decimalsFrom(u.amount, needA) : null);
  const parts: string[] = [];
  if (needA && haveA && dec != null && BigInt(haveA) < BigInt(needA)) {
    parts.push(`needs ${displayUnits(needA, dec)} ${u.symbol} but holds ${displayUnits(haveA, dec)} ${u.symbol}`);
  }
  if (needN && haveN && BigInt(haveN) < BigInt(needN)) {
    parts.push(`needs ${displayUnits(needN, nativeDec)} ${nativeSym} for network fees but holds ${displayUnits(haveN, nativeDec)} ${nativeSym}`);
  }
  if (!parts.length && needA && haveA && needN && haveN && dec != null) {
    parts.push(
      `needs ${displayUnits(needA, dec)} ${u.symbol} and ${displayUnits(needN, nativeDec)} ${nativeSym}; it holds ${displayUnits(haveA, dec)} ${u.symbol} and ${displayUnits(haveN, nativeDec)} ${nativeSym}`,
    );
  }
  return parts.length ? `Not enough funds: the paying wallet ${parts.join(', and ')}.` : null;
}

/** "$1,234.5" for a USD number from the API; null when it isn't one. */
export function usd(v: unknown): string | null {
  if (typeof v !== 'number' || !Number.isFinite(v)) return null;
  const [whole, frac = ''] = v.toFixed(2).split('.');
  const f = frac.replace(/0+$/, '');
  return `$${group(whole ?? '0')}${f ? `.${frac}` : ''}`;
}
