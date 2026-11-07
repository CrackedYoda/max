import type { Sale } from "@max/sdk";

/**
 * The blockhash a signed transaction was built with. Until the chain passes
 * lastValidBlockHeight that transaction can still land, so signing a second
 * one before then could pay twice.
 */
export type SignAttempt = { chain: string; blockhash: string; lastValidBlockHeight: number };
export type StoredAttempt = SignAttempt & { sentAt: number };

// Per sale id. localStorage is shared by every tab of this origin (and
// survives reloads), so a second tab sees an attempt the first one sent.
// Module memory backs it up when storage is missing or throws (private mode,
// blocked site data); every storage access is guarded.
const memory = new Map<string, StoredAttempt>();
const PREFIX = "max:sale-attempt:";
const KEY = (saleId: string) => `${PREFIX}${saleId}`;
/** A blockhash is valid for ~1–2 minutes; an hour-old attempt can't land. */
export const ATTEMPT_TTL_MS = 60 * 60 * 1000;

function storage(): Storage | null {
  try {
    return typeof localStorage === "undefined" ? null : localStorage;
  } catch {
    return null;
  }
}

/** Drop attempts older than ATTEMPT_TTL_MS from memory and storage. */
function prune(now: number): void {
  for (const [id, a] of memory) if (now - a.sentAt > ATTEMPT_TTL_MS) memory.delete(id);
  try {
    const st = storage();
    if (!st) return;
    const stale: string[] = [];
    for (let i = 0; i < st.length; i++) {
      const k = st.key(i);
      if (!k?.startsWith(PREFIX)) continue;
      try {
        const v: unknown = JSON.parse(st.getItem(k) ?? "null");
        if (!valid(v) || now - v.sentAt > ATTEMPT_TTL_MS) stale.push(k);
      } catch {
        stale.push(k);
      }
    }
    for (const k of stale) st.removeItem(k);
  } catch {
    // storage unavailable: memory was pruned
  }
}

function valid(v: unknown): v is StoredAttempt {
  const a = v as Partial<StoredAttempt> | null;
  return !!a && typeof a.chain === "string" && typeof a.blockhash === "string" && Number.isSafeInteger(a.lastValidBlockHeight) && typeof a.sentAt === "number";
}

/** Record an attempt BEFORE asking the wallet to sign and send it. */
export function saveAttempt(saleId: string, attempt: SignAttempt, now = Date.now()): StoredAttempt {
  const stored = { ...attempt, sentAt: now };
  memory.set(saleId, stored);
  try {
    storage()?.setItem(KEY(saleId), JSON.stringify(stored));
  } catch {
    // memory still holds it
  }
  return stored;
}

/**
 * The newest attempt for a sale: storage (which another tab may have written)
 * merged with this tab's memory, keeping the larger lastValidBlockHeight.
 */
export function loadAttempt(saleId: string, now = Date.now()): StoredAttempt | null {
  prune(now);
  let stored: StoredAttempt | null = null;
  try {
    const raw = storage()?.getItem(KEY(saleId));
    const parsed: unknown = raw ? JSON.parse(raw) : null;
    if (valid(parsed)) stored = parsed;
  } catch {
    stored = null;
  }
  const mem = memory.get(saleId) ?? null;
  const newest = !stored ? mem : !mem ? stored : stored.lastValidBlockHeight >= mem.lastValidBlockHeight ? stored : mem;
  if (newest) memory.set(saleId, newest);
  return newest;
}

/** Only when the sale is recorded (MAX has its tx hash) or is terminal and unpaid. */
export function clearAttempt(saleId: string): void {
  memory.delete(saleId);
  try {
    storage()?.removeItem(KEY(saleId));
  } catch {
    // nothing else to do
  }
}

/**
 * Whether polling can stop. `cancelled` is final only without a tx hash: a
 * cancelled sale whose payment turns up can still move to manual_review.
 */
export function isSaleTerminal(sale: Pick<Sale, "status" | "txHash">): boolean {
  switch (sale.status) {
    case "completed":
    case "expired":
    case "manual_review":
    case "reversed":
      return true;
    case "cancelled":
      return !sale.txHash;
    default:
      return false;
  }
}

export type SaleRoute = "held" | "review" | "uncertain" | "watching";

/**
 * Where the buy dialog goes for a sale. A payable sale with a stored attempt
 * never goes straight to review: that attempt may still land, so it is looked
 * for on chain (and waited out) first.
 */
export function routeFor(sale: Pick<Sale, "status" | "txHash">, hasInstructions: boolean, attempt: StoredAttempt | null): SaleRoute {
  if (sale.status === "pending_review") return "held";
  if (sale.status === "locked" && !sale.txHash && hasInstructions) return attempt ? "uncertain" : "review";
  return "watching";
}

/** Housekeeping for a sale view: drop the attempt once it's recorded or the sale ended unpaid. */
export function settleAttempt(sale: Pick<Sale, "id" | "status" | "txHash">): void {
  if (sale.txHash || (isSaleTerminal(sale) && !sale.txHash)) clearAttempt(sale.id);
}
