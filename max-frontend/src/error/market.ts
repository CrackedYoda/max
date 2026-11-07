import { AuthorizationError, isMaxApiError } from "@max/sdk";

import { decimalsFrom, displayUnits } from "@/utils/money";
import { formatDate } from "@/utils/format";

import { errorMessage } from "./messages";

/** Copy for the `reason` codes of `listable` / NOT_LISTABLE (max-core market/eligibility.go). */
const LISTABLE_REASONS: Record<string, string> = {
  not_active: "This MAX ID isn’t active.",
  owner_suspended: "This account is suspended.",
  reserved_claim: "Names claimed through X verification or manual review can’t be sold.",
  holding_period: "New, renamed and just-bought names can be listed after a 72-hour holding period.",
  rename_cooldown: "This name was renamed recently and is still in its cooldown.",
  rename_pending: "A rename is waiting for its fee. Finish or let it expire first.",
  market_banned: "This account can’t use the marketplace.",
  unavailable: "This name isn’t available on the market right now.",
  payout_wallet_removed: "The listing’s payout wallet is no longer linked to your account. Delist and list again with a linked wallet.",
};

export function listableReason(reason: string | undefined, dates?: { listableAt?: string; renameAvailableAt?: string }): string {
  if (!reason) return "This name can’t be sold right now.";
  const base = LISTABLE_REASONS[reason] ?? "This name can’t be sold right now.";
  if (reason === "holding_period" && dates?.listableAt) return `${base} You can list it on ${formatDate(dates.listableAt)}.`;
  if (reason === "rename_cooldown" && dates?.renameAvailableAt) return `${base} You can list it on ${formatDate(dates.renameAvailableAt)}.`;
  return base;
}

export type AssetUnits = {
  /** Token decimals (6 for USDC). Unknown → amounts are not shown. */
  decimals?: number | null;
  symbol: string;
  nativeSymbol?: string;
  nativeDecimals?: number;
  /** The decimal amount the user agreed to; lets decimals be inferred from needAsset when unknown. */
  amount?: string;
};

const str = (v: unknown): string | null => (typeof v === "string" && /^\d+$/.test(v) ? v : typeof v === "number" ? String(v) : null);

/**
 * INSUFFICIENT_FUNDS: what the wallet needs vs what it has, in the token and
 * in SOL. The API sends both in base units.
 */
export function insufficientFundsCopy(details: Record<string, unknown> | undefined, u: AssetUnits): string | null {
  if (!details) return null;
  const nativeSym = u.nativeSymbol ?? "SOL";
  const nativeDec = u.nativeDecimals ?? 9;
  const needA = str(details.needAsset), haveA = str(details.haveAsset);
  const needN = str(details.needNative), haveN = str(details.haveNative);
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
  return parts.length ? `Not enough funds: the paying wallet ${parts.join(", and ")}.` : null;
}

/** errorMessage plus the market details: shortfalls, listable reasons, suspensions. */
export function marketErrorMessage(err: unknown, units?: AssetUnits): string {
  if (err instanceof AuthorizationError) {
    return "The payment instructions from MAX didn’t match the price you accepted, so nothing was signed. Reload and try again.";
  }
  if (isMaxApiError(err)) {
    const d = err.details;
    if (err.code === "INSUFFICIENT_FUNDS" && units) {
      const copy = insufficientFundsCopy(d, units);
      if (copy) return copy;
    }
    if (err.code === "NOT_LISTABLE" && typeof d?.reason === "string") return listableReason(d.reason);
    if (err.code === "MARKET_RESTRICTED" && d?.kind === "offer_suspension") {
      const until = typeof d.until === "string" ? ` until ${formatDate(d.until)}` : "";
      return `You can’t make offers${until}: too many accepted offers went unpaid.`;
    }
  }
  return errorMessage(err);
}
