import { isMaxApiError, MaxNetworkError } from "@max/sdk";
import type { QueryClient } from "@tanstack/react-query";

import { max } from "@/api/client";
import { useSendDraft } from "@/store/flows";
import { useSession } from "@/store/session";
import { useWallet } from "@/store/wallet";

/** Ask the API who we are. The cookie is the session; there is no local flag to trust. */
export async function boot(): Promise<void> {
  const generation = useSession.getState().generation;
  try {
    const account = await max().auth.me();
    if (useSession.getState().generation !== generation) return;
    useSession.getState().setAuthenticated(account);
  } catch (err) {
    if (useSession.getState().generation !== generation) return;
    if (isMaxApiError(err) && err.status === 401) useSession.getState().setAnonymous();
    else if (err instanceof MaxNetworkError) useSession.getState().setUnavailable();
    else useSession.getState().setAnonymous();
  }
}

/** Sign out: revoke the server session, drop every account-scoped cache, clear flows. */
export async function logout(qc: QueryClient): Promise<void> {
  const accountId = useSession.getState().account?.id;
  useSession.getState().bump();
  try {
    await max().auth.logout();
  } catch {
    // Offline: the cookie still expires server-side.
  }
  if (accountId) qc.removeQueries({ queryKey: ["account", accountId] });
  qc.removeQueries({ queryKey: ["me"] });
  useSendDraft.getState().reset();
  useWallet.getState().set(null);
  useSession.getState().setAnonymous();
}
