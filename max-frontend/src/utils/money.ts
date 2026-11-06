import { formatUnits, parseUnits } from "@max/sdk";

/** Matches max-core MARKET_FEE_BPS when the server does not say otherwise. */
export const DEFAULT_FEE_BPS = 1000;

/** Parse a decimal amount the user typed; null when it is not a valid amount for `decimals`. */
export function toBaseUnits(amount: string, decimals: number): bigint | null {
  try {
    return parseUnits(amount.trim().replace(",", "."), decimals);
  } catch {
    return null;
  }
}

/** Base units → display string with thousands separators, no floats. */
export function displayUnits(base: bigint | string, decimals: number): string {
  const [whole, frac] = formatUnits(base, decimals).split(".");
  const grouped = (whole ?? "0").replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  return frac ? `${grouped}.${frac}` : grouped;
}

/** A decimal string from the API, grouped for display ("1250.5" → "1,250.5"). */
export function displayDecimal(amount: string): string {
  const [whole, frac] = amount.split(".");
  const grouped = (whole ?? "0").replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  return frac ? `${grouped}.${frac}` : grouped;
}

/** The marketplace fee in base units, rounded down exactly like max-core. */
export function feeOf(price: bigint, feeBps: number): bigint {
  return (price * BigInt(feeBps)) / BigInt(10000);
}

/** What the seller receives: price − floor(price × feeBps / 10000). */
export function sellerReceives(price: bigint, feeBps: number): bigint {
  return price - feeOf(price, feeBps);
}

/** "90" for 1000 bps; keeps a decimal only when needed ("97.5"). */
export function sellerPercent(feeBps: number): string {
  const bps = 10000 - feeBps;
  return bps % 100 === 0 ? String(bps / 100) : (bps / 100).toFixed(2).replace(/0+$/, "");
}

/**
 * The decimals that turn `price` into `priceBaseUnits`, when the API sends
 * both (search cards, listings, sales). Null when they don't agree.
 */
export function decimalsFrom(price: string, priceBaseUnits: string): number | null {
  for (let d = 0; d <= 18; d++) {
    const v = toBaseUnits(price, d);
    if (v !== null && v.toString() === priceBaseUnits) return d;
  }
  return null;
}
