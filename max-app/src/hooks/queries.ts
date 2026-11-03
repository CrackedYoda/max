import { isMaxApiError, normalizeName, TERMINAL_STATUSES, type Chain, type MaxIdType, type Namespace } from '@max/sdk';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { max } from '@/api/client';
import { keys } from '@/api/keys';
import { hasRpc, nativeBalance, usdcBalance, type Balance } from '@/services/chain';
import { useSession } from '@/store/session';

export function useAccountId(): string {
  return useSession((s) => s.accountId ?? 'anon');
}

const useReady = () => useSession((s) => s.max === 'ready');

// ---- public ----

export function useChains() {
  return useQuery({ queryKey: keys.chains, queryFn: () => max().chains(), staleTime: 60 * 60_000 });
}

/** Chains this app can actually use: listed by MAX and with an RPC configured. */
export function useUsableChains(): { chains: Chain[]; isLoading: boolean; error: unknown } {
  const q = useChains();
  return { chains: (q.data ?? []).filter((c) => hasRpc(c.key)), isLoading: q.isLoading, error: q.error };
}

export function useAvailability(input: string) {
  const name = normalizeName(input);
  return useQuery({
    queryKey: keys.availability(name ?? input),
    queryFn: () => max().availability(name!),
    enabled: !!name,
    staleTime: 10_000,
  });
}

/** Fresh resolution for review. Senders must always see the current wallet. */
export function useResolve(handle: string | null, chain: string, asset: string) {
  return useQuery({
    queryKey: keys.resolve(handle ?? '', chain, asset),
    queryFn: () => max().resolve(handle!, { chain, asset: asset === 'native' ? undefined : asset }),
    enabled: !!handle && !!chain,
    staleTime: 0,
    gcTime: 30_000,
    retry: (n, e) => !isMaxApiError(e) && n < 2,
  });
}

// ---- on-chain balances (no MAX involved) ----

export function useBalances() {
  const accounts = useSession((s) => s.accounts);
  const { chains } = useUsableChains();
  const holders = accounts.flatMap((a) => [a.evm && { ns: 'evm' as const, address: a.evm }, a.solana && { ns: 'solana' as const, address: a.solana }].filter(Boolean)) as {
    ns: Namespace;
    address: string;
  }[];
  const sig = holders.map((h) => h.address).join(',') + '|' + chains.map((c) => c.key).join(',');
  return useQuery({
    queryKey: keys.balances(sig),
    enabled: holders.length > 0 && chains.length > 0,
    staleTime: 20_000,
    refetchInterval: 30_000,
    queryFn: async () => {
      const jobs: Promise<Balance | null>[] = [];
      for (const c of chains) {
        for (const h of holders.filter((x) => x.ns === c.namespace)) {
          jobs.push(nativeBalance(c, h.address).catch(() => null));
          jobs.push(usdcBalance(c, h.address).catch(() => null));
        }
      }
      const all = (await Promise.all(jobs)).filter((b): b is Balance => !!b);
      // Natives always show; tokens only when held.
      return all.filter((b) => b.asset === 'native' || b.raw > 0n);
    },
  });
}

// ---- account (needs a MAX session) ----

export function useMyIds() {
  const accountId = useAccountId();
  return useQuery({ queryKey: keys.ids(accountId), queryFn: () => max().ids.list(), enabled: useReady() });
}

export function useWallets() {
  const accountId = useAccountId();
  return useQuery({ queryKey: keys.wallets(accountId), queryFn: () => max().wallets.list(), enabled: useReady() });
}

export function useTransfers(direction: 'sent' | 'received') {
  const accountId = useAccountId();
  return useQuery({
    queryKey: keys.transfers(accountId, direction),
    queryFn: () => max().transfers.list({ direction, limit: 20 }),
    enabled: useReady(),
  });
}

export function usePayment(id: string | null) {
  const accountId = useAccountId();
  return useQuery({
    queryKey: keys.payment(accountId, id ?? ''),
    queryFn: () => max().transfers.get(id!),
    enabled: !!id,
    refetchInterval: (q) => {
      const s = q.state.data?.status;
      return s && (TERMINAL_STATUSES.includes(s) || s === 'EXPIRED') ? false : 3000;
    },
  });
}

// ---- renames ----

export function useRenameEligibility(maxId: string) {
  const accountId = useAccountId();
  return useQuery({ queryKey: keys.rename(accountId, maxId), queryFn: () => max().renames.eligibility(maxId), enabled: useReady() && !!maxId, staleTime: 0 });
}

/** Polls a rename until its fee settles one way or the other. */
export function useRenameRequest(maxId: string, requestId: string | null) {
  const accountId = useAccountId();
  return useQuery({
    queryKey: keys.renameRequest(accountId, maxId, requestId ?? ''),
    queryFn: () => max().renames.get(maxId, requestId!),
    enabled: !!requestId,
    refetchInterval: (q) => (q.state.data && q.state.data.request.status !== 'awaiting_payment' ? false : 3000),
  });
}

// ---- X verification / reserved names ----

export function useVerifications() {
  const accountId = useAccountId();
  return useQuery({ queryKey: keys.verifications(accountId), queryFn: () => max().verifications.list(), enabled: useReady() });
}

export function useReservedClaims() {
  const accountId = useAccountId();
  return useQuery({ queryKey: keys.reservedClaims(accountId), queryFn: () => max().reservedClaims.list(), enabled: useReady() });
}

export function useInvalidateAccount() {
  const qc = useQueryClient();
  const accountId = useAccountId();
  return () => qc.invalidateQueries({ queryKey: keys.account(accountId) });
}

export function useClaimId() {
  const invalidate = useInvalidateAccount();
  return useMutation({
    mutationFn: (v: { name: string; type: MaxIdType }) => max().ids.claim(v),
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
