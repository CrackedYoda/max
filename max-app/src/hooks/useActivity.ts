import type { Payment } from '@max/sdk';

import { useTransfers } from './queries';

export type ActivityItem = { payment: Payment; direction: 'sent' | 'received' };

/** Sent and received merged, newest first. Rename fees are account housekeeping, not activity. */
export function useActivity(limit = 6) {
  const sent = useTransfers('sent');
  const received = useTransfers('received');
  const items: ActivityItem[] = [
    ...(sent.data ?? []).filter((p) => p.purpose === 'transfer').map((payment) => ({ payment, direction: 'sent' as const })),
    ...(received.data ?? []).map((payment) => ({ payment, direction: 'received' as const })),
  ]
    .sort((a, b) => b.payment.createdAt.localeCompare(a.payment.createdAt))
    .slice(0, limit);
  return {
    items,
    isLoading: sent.isLoading || received.isLoading,
    error: sent.error ?? received.error,
    refetch: () => Promise.all([sent.refetch(), received.refetch()]),
  };
}
