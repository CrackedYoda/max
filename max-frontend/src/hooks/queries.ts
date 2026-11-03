"use client";

import { isMaxApiError, normalizeName, TERMINAL_STATUSES, type Decision, type MaxIdType, type Namespace } from "@max/sdk";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { max } from "@/api/client";
import { keys } from "@/api/keys";
import { useSession } from "@/store/session";

export function useAccountId(): string {
  return useSession((s) => s.account?.id ?? "anon");
}

const signedIn = () => useSession.getState().status === "authenticated";

// ---- public ----

export function useChains() {
  return useQuery({ queryKey: keys.chains, queryFn: () => max().chains(), staleTime: 60 * 60_000 });
}

/** Availability for a debounced input. Invalid names never hit the API. */
export function useAvailability(input: string) {
  const name = normalizeName(input);
  return useQuery({
    queryKey: keys.availability(name ?? input),
    queryFn: () => max().availability(name!),
    enabled: !!name,
    staleTime: 10_000,
  });
}

export function useProfile(handle: string) {
  return useQuery({
    queryKey: keys.profile(handle),
    queryFn: () => max().profile(handle),
    retry: (n, e) => !isMaxApiError(e, "MAX_ID_NOT_FOUND") && n < 2,
  });
}

/** Fresh resolution for review. Never cached for long: senders must see the current wallet. */
export function useResolve(handle: string | null, chain: string, asset: string) {
  return useQuery({
    queryKey: keys.resolve(handle ?? "", chain, asset),
    queryFn: () => max().resolve(handle!, { chain, asset: asset || undefined }),
    enabled: !!handle && !!chain,
    staleTime: 0,
    gcTime: 30_000,
    retry: (n, e) => !isMaxApiError(e) && n < 2,
  });
}

// ---- account ----

export function useMyIds() {
  const accountId = useAccountId();
  return useQuery({ queryKey: keys.ids(accountId), queryFn: () => max().ids.list(), enabled: signedIn() });
}

export function useWallets() {
  const accountId = useAccountId();
  return useQuery({ queryKey: keys.wallets(accountId), queryFn: () => max().wallets.list(), enabled: signedIn() });
}

export function useTransfers(direction: "sent" | "received") {
  const accountId = useAccountId();
  return useQuery({
    queryKey: keys.transfers(accountId, direction),
    queryFn: () => max().transfers.list({ direction, limit: 20 }),
    enabled: signedIn(),
  });
}

/** Polls a payment until it settles. */
export function usePayment(id: string | null) {
  const accountId = useAccountId();
  return useQuery({
    queryKey: keys.payment(accountId, id ?? ""),
    queryFn: () => max().transfers.get(id!),
    enabled: !!id,
    refetchInterval: (q) => {
      const s = q.state.data?.status;
      return s && (TERMINAL_STATUSES.includes(s) || s === "EXPIRED") ? false : 3000;
    },
  });
}

export function useInvalidateAccount() {
  const qc = useQueryClient();
  const accountId = useAccountId();
  return () => qc.invalidateQueries({ queryKey: keys.account(accountId) });
}

export function useClaimId() {
  const invalidate = useInvalidateAccount();
  return useMutation({
    mutationFn: (v: { name: string; type: MaxIdType; displayName?: string }) => max().ids.claim(v),
    onSuccess: invalidate,
  });
}

export function useSetDestination() {
  const invalidate = useInvalidateAccount();
  return useMutation({
    mutationFn: (v: { id: string; namespace: Namespace; walletId: string }) => max().ids.setDestination(v.id, v.namespace, v.walletId),
    onSuccess: invalidate,
  });
}

export function useUpdateProfile() {
  const invalidate = useInvalidateAccount();
  return useMutation({
    mutationFn: (v: { id: string; displayName: string; description: string }) =>
      max().ids.updateProfile(v.id, { displayName: v.displayName, description: v.description }),
    onSuccess: invalidate,
  });
}

export function useRemoveWallet() {
  const invalidate = useInvalidateAccount();
  return useMutation({ mutationFn: (walletId: string) => max().wallets.remove(walletId), onSuccess: invalidate });
}

// ---- agents ----

export function useAgents() {
  const accountId = useAccountId();
  return useQuery({ queryKey: keys.agents(accountId), queryFn: () => max().agents.list(), enabled: signedIn() });
}

export function useAgent(id: string) {
  const accountId = useAccountId();
  return useQuery({ queryKey: keys.agent(accountId, id), queryFn: () => max().agents.get(id), enabled: signedIn() && !!id });
}

export function useAgentTransfers(id: string, decision: Decision | "all") {
  const accountId = useAccountId();
  return useQuery({
    queryKey: keys.agentTransfers(accountId, id, decision),
    queryFn: () => max().agents.transfers(id, decision === "all" ? {} : { decision }),
    enabled: signedIn() && !!id,
    // Pending approvals expire; keep the queue fresh while it's open.
    refetchInterval: decision === "pending_approval" ? 10_000 : false,
  });
}

// ---- renames ----

export function useRenameEligibility(maxId: string) {
  const accountId = useAccountId();
  return useQuery({ queryKey: keys.rename(accountId, maxId), queryFn: () => max().renames.eligibility(maxId), enabled: signedIn() && !!maxId });
}

/** Polls a rename until its fee settles one way or the other. */
export function useRenameRequest(maxId: string, requestId: string | null) {
  const accountId = useAccountId();
  return useQuery({
    queryKey: keys.renameRequest(accountId, maxId, requestId ?? ""),
    queryFn: () => max().renames.get(maxId, requestId!),
    enabled: !!requestId,
    refetchInterval: (q) => (q.state.data && q.state.data.request.status !== "awaiting_payment" ? false : 3000),
  });
}

// ---- X verification / reserved names ----

export function useVerifications() {
  const accountId = useAccountId();
  return useQuery({ queryKey: keys.verifications(accountId), queryFn: () => max().verifications.list(), enabled: signedIn() });
}

export function useReservedClaims() {
  const accountId = useAccountId();
  return useQuery({ queryKey: keys.reservedClaims(accountId), queryFn: () => max().reservedClaims.list(), enabled: signedIn() });
}
