export function stripPrefix(v: string): string {
  return v.trim().replace(/^max\./i, '').toLowerCase();
}

/** Extracts a handle from a scanned QR: a profile link, "max.name", or a bare name. */
export function handleFromScan(data: string): string | null {
  const s = data.trim();
  try {
    const u = new URL(s);
    const seg = u.pathname.split('/').filter(Boolean)[0];
    if (seg) return stripPrefix(decodeURIComponent(seg));
  } catch {
    // not a URL
  }
  if (/^(max\.)?[a-z0-9-]{3,32}$/i.test(s)) return stripPrefix(s);
  return null;
}
