/** `0x5aAe…eAed` / `7xKX…gAsU`. Lists only; confirmations always show the full address. */
export function shortAddress(addr: string, head = 4, tail = 4): string {
  if (!addr) return "";
  const prefix = addr.startsWith("0x") ? 2 : 0;
  if (addr.length <= prefix + head + tail + 1) return addr;
  return `${addr.slice(0, prefix + head)}…${addr.slice(-tail)}`;
}

export function namespaceLabel(ns: string): string {
  return ns === "evm" ? "EVM" : ns === "solana" ? "Solana" : ns;
}

export function formatDate(iso: string | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

export function relativeTime(iso: string | undefined): string {
  if (!iso) return "";
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  return formatDate(iso);
}

/** "in 12 min" / "in 3 h" for future timestamps; "expired" once passed. */
export function timeUntil(iso: string | undefined, now = Date.now()): string {
  if (!iso) return "";
  const s = (new Date(iso).getTime() - now) / 1000;
  if (s <= 0) return "expired";
  if (s < 60) return "in under a minute";
  if (s < 3600) return `in ${Math.ceil(s / 60)} min`;
  if (s < 86400) return `in ${Math.round(s / 3600)} h`;
  return `on ${formatDate(iso)}`;
}

/** Base-unit integer → display string, without floating point. */
export function fromBaseUnits(raw: string, decimals: number): string {
  const neg = raw.startsWith("-");
  const digits = (neg ? raw.slice(1) : raw).padStart(decimals + 1, "0");
  const int = digits.slice(0, digits.length - decimals) || "0";
  const frac = decimals ? digits.slice(-decimals).replace(/0+$/, "") : "";
  return `${neg ? "-" : ""}${int}${frac ? `.${frac}` : ""}`;
}

/** Policy reasons quote base units ("amount 200000000 exceeds …"); show coin amounts instead. */
export function humanizeReason(reason: string, decimals: number, symbol: string): string {
  return reason.replace(/\b\d{4,}\b/g, (n) => `${fromBaseUnits(n, decimals)} ${symbol}`);
}
