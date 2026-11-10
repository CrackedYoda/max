import type { ListingStatus, OfferStatus, SaleStatus, Wallet } from '@max/sdk';

import { Pill } from '@/components/ui/Blocks';
import { getEnv } from '@/config/env';
import type { AccountInfo } from '@/services/vault';

type Tone = 'neutral' | 'clay' | 'success' | 'danger';

const SALE: Record<SaleStatus, { label: string; tone: Tone }> = {
  pending_review: { label: 'In review', tone: 'clay' },
  locked: { label: 'Awaiting payment', tone: 'neutral' },
  submitted: { label: 'Submitted', tone: 'neutral' },
  confirming: { label: 'Confirming', tone: 'neutral' },
  completed: { label: 'Completed', tone: 'success' },
  expired: { label: 'Expired', tone: 'danger' },
  cancelled: { label: 'Cancelled', tone: 'danger' },
  manual_review: { label: 'Manual review', tone: 'clay' },
  reversed: { label: 'Reversed', tone: 'danger' },
};

const LISTING: Record<ListingStatus, { label: string; tone: Tone }> = {
  active: { label: 'Listed', tone: 'success' },
  locked: { label: 'Sale in progress', tone: 'clay' },
  sold: { label: 'Sold', tone: 'neutral' },
  cancelled: { label: 'Delisted', tone: 'neutral' },
  admin_delisted: { label: 'Removed by MAX', tone: 'danger' },
};

const OFFER: Record<OfferStatus, { label: string; tone: Tone }> = {
  live: { label: 'Open', tone: 'success' },
  unfunded: { label: 'Unfunded', tone: 'danger' },
  accepted: { label: 'Accepted: pay now', tone: 'clay' },
  declined: { label: 'Declined', tone: 'neutral' },
  superseded: { label: 'Closed', tone: 'neutral' },
  expired: { label: 'Expired', tone: 'neutral' },
  withdrawn: { label: 'Withdrawn', tone: 'neutral' },
  lapsed: { label: 'Lapsed (unpaid)', tone: 'danger' },
  completed: { label: 'Completed', tone: 'success' },
};

export function SaleStatusPill({ status }: { status: SaleStatus }) {
  const s = SALE[status] ?? { label: status, tone: 'neutral' as const };
  return <Pill small tone={s.tone} label={s.label} />;
}

export function ListingStatusPill({ status }: { status: ListingStatus }) {
  const s = LISTING[status] ?? { label: status, tone: 'neutral' as const };
  return <Pill small tone={s.tone} label={s.label} />;
}

export function OfferStatusPill({ status }: { status: OfferStatus }) {
  const s = OFFER[status] ?? { label: status, tone: 'neutral' as const };
  return <Pill small tone={s.tone} label={s.label} />;
}

/** Sales settle on Solana only (spec §2): the namespace of a market chain key. */
export const isSolanaChain = (chainKey: string) => chainKey.startsWith('solana');

/** The Solana accounts on this phone, i.e. the ones that can pay. */
export function solanaPayers(accounts: AccountInfo[]): { label: string; address: string }[] {
  return accounts.filter((a) => !!a.solana).map((a) => ({ label: a.label, address: a.solana! }));
}

/** Linked Solana wallets, the ones on this phone first (payout choices). */
export function solanaPayouts(wallets: Wallet[] | undefined, accounts: AccountInfo[]): Wallet[] {
  const here = new Set(accounts.map((a) => a.solana).filter(Boolean));
  return (wallets ?? []).filter((w) => w.namespace === 'solana').sort((a, b) => Number(here.has(b.address)) - Number(here.has(a.address)));
}

/** The asset offers default to on an unlisted name with no sales: the first configured market asset (v1: USDC). */
export function defaultOfferAsset(): { chain: string; asset: string; symbol: string } | null {
  const a = getEnv().marketAssets[0];
  return a ? { chain: a.chain, asset: a.asset, symbol: 'USDC' } : null;
}
