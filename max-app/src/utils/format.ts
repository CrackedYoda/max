/** `0x5aAe…eAed` / `7xKX…gAsU`. Lists only; confirmations always show the full address. */
export function shortAddress(addr: string, head = 4, tail = 4): string {
  if (!addr) return '';
  const prefix = addr.startsWith('0x') ? 2 : 0;
  if (addr.length <= prefix + head + tail + 1) return addr;
  return `${addr.slice(0, prefix + head)}…${addr.slice(-tail)}`;
}

export function namespaceLabel(ns: string): string {
  return ns === 'evm' ? 'EVM' : ns === 'solana' ? 'Solana' : ns;
}

export function formatDate(iso: string | undefined): string {
  if (!iso) return '';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}

export function relativeTime(iso: string | undefined): string {
  if (!iso) return '';
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 60) return 'just now';
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  return formatDate(iso);
}

/** "in 3 days", "in 5 h", "in 12 min"; "now" once passed. */
export function timeUntil(iso: string | undefined): string {
  if (!iso) return '';
  const s = (new Date(iso).getTime() - Date.now()) / 1000;
  if (!(s > 0)) return 'now';
  if (s < 3600) return `in ${Math.max(1, Math.floor(s / 60))} min`;
  if (s < 86400) return `in ${Math.floor(s / 3600)} h`;
  const d = Math.floor(s / 86400);
  return `in ${d} day${d === 1 ? '' : 's'}`;
}
