import type { Sale } from "@max/sdk";

/**
 * The blockhash a signed transaction was built with. Until the chain passes
 * lastValidBlockHeight that transaction can still land, so signing a second
 * one before then could pay twice.
 */
export type SignAttempt = { chain: string; blockhash: string; lastValidBlockHeight: number };
export type StoredAttempt = SignAttempt & { sentAt: number };

// Per sale id. Module memory survives the dialog closing; sessionStorage
// survives a reload of the tab. Storage can be missing or throw (private
// mode, blocked site data), so every access is guarded and memory still works.
const memory = new Map<string, StoredAttempt>();
const KEY = (saleId: string) => `max:sale-attempt:${saleId}`;

function storage(): Storage | null {
  try {
    return typeof sessionStorage === "undefined" ? null : sessionStorage;
  } catch {
    return null;
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

export function loadAttempt(saleId: string): StoredAttempt | null {
  const m = memory.get(saleId);
  if (m) return m;
  try {
    const raw = storage()?.getItem(KEY(saleId));
    if (!raw) return null;
    const parsed: unknown = JSON.parse(raw);
    if (!valid(parsed)) return null;
    memory.set(saleId, parsed);
    return parsed;
  } catch {
    return null;
  }
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
