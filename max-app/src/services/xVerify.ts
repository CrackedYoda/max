import type { XVerification } from '@max/sdk';
import * as Linking from 'expo-linking';
import * as WebBrowser from 'expo-web-browser';

import { max } from '@/api/client';

/**
 * X verification from the app. The auth session can't carry the app's MAX
 * token, so MAX's callback forwards X's `state` + `code` to max://verify/x and
 * the app finishes the flow itself with its own session (the one that started it).
 */

export type XResult = { kind: 'verified'; verification: XVerification; renewed: number } | { kind: 'cancelled' };

// The deep link can arrive twice on Android (auth session result + router).
const inflight = new Map<string, Promise<XResult>>();

/** Name the user was claiming, kept across the round trip to X. Not sensitive. */
let pendingName = '';
export const getPendingName = () => pendingName;
export const setPendingName = (n: string) => {
  pendingName = n;
};

export async function verifyWithX(name: string): Promise<XResult> {
  pendingName = name;
  const { authorizeUrl } = await max().verifications.startX({ client: 'native' });
  const res = await WebBrowser.openAuthSessionAsync(authorizeUrl, Linking.createURL('verify/x'));
  if (res.type !== 'success') return { kind: 'cancelled' };
  const q = Linking.parse(res.url).queryParams ?? {};
  return completeX({ state: str(q.state), code: str(q.code), error: str(q.error) });
}

/** Finishes a flow from the deep link's query. Safe to call twice for the same state. */
export function completeX(q: { state?: string; code?: string; error?: string }): Promise<XResult> {
  if (!q.state) return Promise.reject(new Error('That X sign-in expired. Start again.'));
  let p = inflight.get(q.state);
  if (!p) {
    p = max()
      .verifications.completeX({ state: q.state, code: q.code || undefined, error: q.error || undefined })
      .then((r) => ({ kind: 'verified' as const, verification: r.verification, renewed: r.renewedClaims }));
    inflight.set(q.state, p);
  }
  return p;
}

function str(v: unknown): string | undefined {
  return typeof v === 'string' ? v : Array.isArray(v) && typeof v[0] === 'string' ? v[0] : undefined;
}
