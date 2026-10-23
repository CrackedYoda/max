/** Convert a decimal string to integer base units without floating point. */
export function parseUnits(amount: string, decimals: number): bigint {
  const s = amount.trim();
  if (!/^\d*\.?\d*$/.test(s) || s === "" || s === ".") throw new Error(`invalid amount "${amount}"`);
  const [whole = "", frac = ""] = s.split(".");
  const trimmed = frac.replace(/0+$/, "");
  if (trimmed.length > decimals) throw new Error(`"${amount}" has more than ${decimals} decimal places`);
  return BigInt((whole || "0") + trimmed.padEnd(decimals, "0"));
}

/** Render integer base units as a trimmed decimal string. */
export function formatUnits(value: bigint | string, decimals: number): string {
  const v = BigInt(value);
  const neg = v < 0n;
  const s = (neg ? -v : v).toString().padStart(decimals + 1, "0");
  const whole = s.slice(0, s.length - decimals) || "0";
  const frac = decimals ? s.slice(s.length - decimals).replace(/0+$/, "") : "";
  return (neg ? "-" : "") + whole + (frac ? `.${frac}` : "");
}

/**
 * Canonicalize a MAX ID the way the API does ("MAX.Yoda" → "yoda"), or
 * return null if it can never be valid. Useful for instant UI feedback; the
 * API remains the source of truth.
 */
export function normalizeName(input: string): string | null {
  let s = input.trim();
  if (s.toLowerCase().startsWith("max.")) s = s.slice(4);
  s = s.replace(/[A-Z]/g, (c) => c.toLowerCase());
  if (s.length < 3 || s.length > 32) return null;
  if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(s)) return null;
  return s;
}

export function displayHandle(name: string): string {
  return name.startsWith("max.") ? name : `max.${name}`;
}
