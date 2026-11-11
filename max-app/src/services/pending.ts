import { isMaxApiError, TERMINAL_STATUSES } from '@max/sdk';

import { max } from '@/api/client';
import * as secure from '@/services/secure';
import type { SignedTx } from '@/store/flows';

/**
 * Payments signed on this phone whose hash MAX hasn't recorded yet. The record
 * is written before broadcast and dropped once MAX has a hash, so a lock, crash
 * or lost connection between the two never loses the transaction's hash: the
 * next unlock hands it to MAX. Signed bytes are never re-broadcast from here;
 * the user approved that send once, in the flow that signed it.
 */

export type PendingKind = secure.PendingPayment['kind'];

// Read-modify-write on one keychain item: run updates one at a time.
let queue: Promise<unknown> = Promise.resolve();
function update(change: (list: secure.PendingPayment[]) => secure.PendingPayment[]): Promise<void> {
  const next = queue.then(async () => secure.savePending(change(await secure.readPending())));
  queue = next.catch(() => {});
  return next;
}

/** Call before broadcasting. Throws if it couldn't be saved: then don't broadcast. */
export function remember(kind: PendingKind, paymentId: string, chainKey: string, signed: SignedTx): Promise<void> {
  const entry: secure.PendingPayment = { kind, paymentId, chainKey, signed: { ...signed }, at: Date.now() };
  return update((list) => [...list.filter((p) => p.paymentId !== paymentId), entry]);
}

/** Call once MAX recorded the hash, or the signed bytes were definitely never sent. */
export function forget(paymentId: string): Promise<void> {
  return update((list) => list.filter((p) => p.paymentId !== paymentId));
}

/** Best-effort forget: a leftover record only means one more harmless submit later. */
export function forgetQuietly(paymentId: string): void {
  forget(paymentId).catch(() => {});
}

/**
 * Submits every remembered hash. Stops when `isCurrent` turns false (the app
 * locked). Returns how many MAX now has. Anything that fails stays for the
 * next unlock.
 */
export async function resume(isCurrent: () => boolean): Promise<number> {
  let settled = 0;
  for (const p of await secure.readPending()) {
    if (!isCurrent()) break;
    if (!(await submitted(p))) continue;
    await forget(p.paymentId);
    settled++;
  }
  return settled;
}

/** True once MAX has a hash for the payment or the payment is over. */
async function submitted(p: secure.PendingPayment): Promise<boolean> {
  try {
    await max().transfers.submit(p.paymentId, p.signed.hash);
    return true;
  } catch (err) {
    if (!isMaxApiError(err)) return false; // offline: keep it
  }
  try {
    const payment = await max().transfers.get(p.paymentId);
    return !!payment.txHash || TERMINAL_STATUSES.includes(payment.status);
  } catch {
    return false;
  }
}
