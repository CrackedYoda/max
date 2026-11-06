import { AuthorizationError, isMaxApiError } from '@max/sdk';

import { formatDate } from '@/utils/format';
import { insufficientFundsCopy, type AssetUnits } from '@/utils/money';

import { errorMessage } from './messages';

/** Copy for the `reason` codes of `listable` / NOT_LISTABLE (max-core market eligibility). */
const LISTABLE_REASONS: Record<string, string> = {
  not_active: 'This MAX ID isn’t active.',
  owner_suspended: 'This account is suspended.',
  reserved_claim: 'Names claimed through X verification or manual review can’t be sold.',
  holding_period: 'New, renamed and just-bought names can be listed after a 72-hour holding period.',
  rename_cooldown: 'This name was renamed recently and is still in its cooldown.',
  rename_pending: 'A rename is waiting for its fee. Finish it or let it expire first.',
  market_banned: 'This account can’t use the marketplace.',
  unavailable: 'This name isn’t available on the market right now.',
  payout_wallet_removed: 'The listing’s payout wallet is no longer linked to your account. Delist and list again with a linked wallet.',
};

export function listableReason(reason: string | undefined, dates?: { listableAt?: string; renameAvailableAt?: string }): string {
  const base = (reason && LISTABLE_REASONS[reason]) || 'This name can’t be sold right now.';
  if (reason === 'holding_period' && dates?.listableAt) return `${base} You can list it on ${formatDate(dates.listableAt)}.`;
  if (reason === 'rename_cooldown' && dates?.renameAvailableAt) return `${base} You can list it on ${formatDate(dates.renameAvailableAt)}.`;
  return base;
}

/** errorMessage plus the market details: shortfalls (need vs have), listable reasons, suspensions. */
export function marketErrorMessage(err: unknown, units?: AssetUnits): string {
  if (err instanceof AuthorizationError) {
    return 'The payment instructions from MAX didn’t match the price you accepted, so nothing was signed. Go back and try again.';
  }
  if (isMaxApiError(err)) {
    const d = err.details;
    if (err.code === 'INSUFFICIENT_FUNDS' && units) {
      const copy = insufficientFundsCopy(d, units);
      if (copy) return copy;
    }
    if (err.code === 'NOT_LISTABLE' && typeof d?.reason === 'string') return listableReason(d.reason);
    if (err.code === 'MARKET_RESTRICTED' && d?.kind === 'offer_suspension') {
      const until = typeof d.until === 'string' ? ` until ${formatDate(d.until)}` : '';
      return `You can’t make offers${until}: too many accepted offers went unpaid.`;
    }
  }
  return errorMessage(err);
}
