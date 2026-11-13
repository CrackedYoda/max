"use client";

import { isMaxApiError, normalizeName, TERMINAL_STATUSES, type Decision, type DestinationTarget, type MaxIdType, type Namespace, type ReceivableAsset, type SearchParams, type WalletSigner } from "@max/sdk";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { max } from "@/api/client";
import { keys } from "@/api/keys";
import { getEnv } from "@/config/env";
import { mintDecimals } from "@/services/market";
import { isSaleTerminal } from "@/services/saleAttempts";
import { useSession } from "@/store/session";

export function useAccountId(): string {
  return useSession((s) => s.account?.id ?? "anon");
}

const signedIn = () => useSession.getState().status === "authenticated";

// ---- public ----

export function useChains() {
  return useQuery({ queryKey: keys.chains, queryFn: () => max().chains(), staleTime: 60 * 60_000 });
}

/** What a cross-chain payment can deliver; the provider's list changes rarely. */
export function useReceivableAssets() {
  return useQuery({ queryKey: keys.receivableAssets, queryFn: () => max().receivableAssets(), staleTime: 10 * 60_000 });
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
/** Every token the wallet holds on the chain, for the Send asset list. */
export function useHoldings(chain: string | undefined, address: string | undefined) {
  return useQuery({
    queryKey: keys.holdings(chain ?? "", address ?? ""),
    queryFn: () => max().holdings(chain!, address!),
    enabled: !!chain && !!address,
    staleTime: 20_000,
    retry: 1,
  });
}

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

/** The signed-in account with its default and primary MAX IDs. */
export function useMe() {
  const accountId = useAccountId();
  return useQuery({ queryKey: keys.me(accountId), queryFn: () => max().auth.me(), enabled: signedIn() });
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
    // `signer` connects a linked wallet; it signs the claim message for this name.
    mutationFn: async ({ signer, ...v }: { name: string; type: MaxIdType; displayName?: string; signer: () => Promise<WalletSigner> }) =>
      max().ids.claim(v, await signer()),
    onSuccess: invalidate,
  });
}

export function useSetDestination() {
  const invalidate = useInvalidateAccount();
  return useMutation({
    mutationFn: (v: { id: string; namespace: Namespace; target: DestinationTarget }) => max().ids.setDestination(v.id, v.namespace, v.target),
    onSuccess: invalidate,
  });
}

/** The ID's "get paid in" preference; null clears it (receive whatever is sent). */
export function useSetSettlement() {
  const invalidate = useInvalidateAccount();
  return useMutation({
    mutationFn: (v: { id: string; asset: ReceivableAsset | null }) =>
      v.asset
        ? max().ids.setSettlement(v.id, { chain: v.asset.chain, asset: v.asset.kind === "native" ? "native" : v.asset.address })
        : max().ids.removeSettlement(v.id),
    onSuccess: invalidate,
  });
}

export function useSetChainDestination() {
  const invalidate = useInvalidateAccount();
  return useMutation({
    mutationFn: (v: { id: string; chain: string; target: DestinationTarget }) => max().ids.setChainDestination(v.id, v.chain, v.target),
    onSuccess: invalidate,
  });
}

export function useRemoveChainDestination() {
  const invalidate = useInvalidateAccount();
  return useMutation({
    mutationFn: (v: { id: string; chain: string }) => max().ids.removeChainDestination(v.id, v.chain),
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

// ---- marketplace ----

export { isSaleTerminal } from "@/services/saleAttempts";

/** Public listing search with cursor pagination. */
export function useMarketSearch(params: Omit<SearchParams, "cursor">) {
  return useInfiniteQuery({
    queryKey: keys.marketSearch(params),
    queryFn: ({ pageParam }) => max().market.search({ ...params, cursor: pageParam || undefined }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextCursor || undefined,
    retry: (n, e) => !isMaxApiError(e) && n < 2,
  });
}

/** Public market view of a name. A disabled market (404 MARKET_DISABLED) is not retried. */
export function useMarketName(name: string | null) {
  return useQuery({
    queryKey: keys.marketName(name ?? ""),
    queryFn: () => max().market.getName(name!),
    enabled: !!name,
    retry: (n, e) => !isMaxApiError(e) && n < 2,
  });
}

/** Token decimals straight from the chain (not from MAX). */
export function useMintDecimals(chain: string | undefined, mint: string | undefined) {
  return useQuery({
    queryKey: keys.mintDecimals(chain ?? "", mint ?? ""),
    queryFn: () => mintDecimals(chain!, mint!),
    enabled: !!chain && !!mint,
    staleTime: Infinity,
    gcTime: Infinity,
  });
}

/**
 * Token decimals from trusted sources only: the env pin for a configured
 * market asset, cross-checked against the mint on chain, or the chain alone.
 * Never inferred from API fields. `error` is set when the two disagree.
 */
export function useTrustedDecimals(chain: string | undefined, mint: string | undefined): { decimals: number | null; error: string | null; loading: boolean } {
  const pinned = getEnv().marketAssets.find((a) => a.chain === chain && a.asset === mint)?.decimals;
  const onChain = useMintDecimals(chain, mint);
  if (pinned != null && onChain.data != null && onChain.data !== pinned) {
    return { decimals: null, error: "This token’s decimals on chain don’t match MAX’s configuration, so nothing can be signed.", loading: false };
  }
  const decimals = pinned ?? onChain.data ?? null;
  return { decimals, error: null, loading: decimals == null && onChain.isLoading };
}

export function useMarketInbox() {
  const accountId = useAccountId();
  return useQuery({
    queryKey: keys.marketInbox(accountId),
    queryFn: () => max().market.inbox(),
    enabled: signedIn(),
    retry: (n, e) => !isMaxApiError(e) && n < 2,
  });
}

export function useMarketEligibility(maxId: string | null) {
  const accountId = useAccountId();
  return useQuery({
    queryKey: keys.marketEligibility(accountId, maxId ?? ""),
    queryFn: () => max().market.eligibility(maxId!),
    enabled: signedIn() && !!maxId,
  });
}

/** Polls a sale every 3s until it reaches a terminal status. */
export function useSale(id: string | null, enabled = true) {
  const accountId = useAccountId();
  return useQuery({
    queryKey: keys.sale(accountId, id ?? ""),
    queryFn: () => max().market.getSale(id!),
    enabled: !!id && enabled,
    refetchInterval: (q) => {
      const s = q.state.data?.sale;
      return s && isSaleTerminal(s) ? false : 3000;
    },
  });
}

/** Refresh everything market-related: public views and the caller's inbox. */
export function useInvalidateMarket() {
  const qc = useQueryClient();
  const accountId = useAccountId();
  return () => {
    qc.invalidateQueries({ queryKey: keys.market });
    qc.invalidateQueries({ queryKey: keys.account(accountId) });
  };
}

// ---- platform admin ----

/**
 * Whether the signed-in account is a MAX administrator. max-core's admin
 * routes answer 404 to everyone else (middleware.Admin), so the probe is the
 * server's own check; the page is a UX gate only.
 */
export function useIsMarketAdmin() {
  const accountId = useAccountId();
  const q = useQuery({
    queryKey: keys.adminMarket(accountId, "probe", {}),
    queryFn: () => max().admin.marketSales({ status: "pending_review", limit: 1 }),
    enabled: signedIn(),
    retry: false,
    staleTime: 5 * 60_000,
  });
  return { isAdmin: q.isSuccess, isLoading: q.isLoading, error: q.error };
}

export function useAdminMarket<T>(what: string, filter: object, fn: () => Promise<T>, enabled = true) {
  const accountId = useAccountId();
  return useQuery({ queryKey: keys.adminMarket(accountId, what, filter), queryFn: fn, enabled: signedIn() && enabled });
}
