import { isMaxApiError, isPendingApproval, type Listing, type MarketInbox, type Offer, type ReceivedOffer, type Sale } from '@max/sdk';
import { router } from 'expo-router';
import { useState, type ReactNode } from 'react';
import { Alert, RefreshControl, ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { ListingStatusPill, OfferStatusPill, SaleStatusPill, solanaPayouts } from '@/components/market/shared';
import { AppText, Handle } from '@/components/ui/AppText';
import { Banner, List, Loading, SectionTitle } from '@/components/ui/Blocks';
import { Button } from '@/components/ui/Button';
import { TitleHeader } from '@/components/ui/Screen';
import { MaxStatusBanner } from '@/components/wallet/MaxStatusBanner';
import { max } from '@/api/client';
import { marketErrorMessage } from '@/error/market';
import { useInvalidateMarket, useMarketInbox, useMyIds, useWallets } from '@/hooks/queries';
import { toast, useSaleDraft, type SaleTarget } from '@/store/flows';
import { useSession } from '@/store/session';
import { colors, spacing } from '@/theme';
import { formatDate, relativeTime, shortAddress, timeUntil } from '@/utils/format';
import { DEFAULT_FEE_BPS, decimalsFrom, displayDecimal, displayUnits, sellerReceives } from '@/utils/money';

const OPEN_LISTING = ['active', 'locked'];

/** My market: listings, offers received (accept/decline), offers made (withdraw/pay), sales. */
export default function MarketOffers() {
  const insets = useSafeAreaInsets();
  const inbox = useMarketInbox();
  const ids = useMyIds();
  const wallets = useWallets();
  const accounts = useSession((s) => s.accounts);
  const invalidate = useInvalidateMarket();
  const close = () => (router.canGoBack() ? router.back() : router.replace('/market'));
  const d = inbox.data;
  const handleOf = (maxId: string, fallback?: string) => fallback || ids.data?.find((i) => i.id === maxId)?.handle || 'MAX ID';
  const mine = new Set([...(wallets.data ?? []).map((w) => w.address), ...accounts.map((a) => a.solana).filter(Boolean)]);
  const onPhone = (address: string) => accounts.some((a) => a.solana === address);
  const disabled = isMaxApiError(inbox.error, 'MARKET_DISABLED') || (isMaxApiError(inbox.error) && inbox.error.status === 404);

  function pay(target: SaleTarget) {
    if (!onPhone(target.fromAddress)) {
      Alert.alert('Can’t pay from this phone', `This is paid from ${shortAddress(target.fromAddress)}, which isn’t an account on this phone. Pay it where that wallet lives.`);
      return;
    }
    useSaleDraft.getState().setTarget(target);
    router.push({ pathname: '/review', params: { flow: 'sale' } });
  }

  return (
    <View style={{ flex: 1, backgroundColor: colors.ivory, paddingTop: insets.top + 12 }}>
      <View style={{ paddingHorizontal: spacing.gutter, paddingBottom: 12 }}>
        <TitleHeader title="My market" onBack={close} />
      </View>
      <ScrollView
        contentContainerStyle={{ paddingHorizontal: spacing.gutter, paddingBottom: insets.bottom + 24, gap: 22 }}
        refreshControl={<RefreshControl refreshing={inbox.isRefetching} onRefresh={invalidate} tintColor={colors.muted} />}>
        <MaxStatusBanner />
        {disabled ? (
          <Banner tone="clay" title="The marketplace isn’t open yet" />
        ) : inbox.error ? (
          <Banner tone="danger">{marketErrorMessage(inbox.error)}</Banner>
        ) : null}
        {inbox.isLoading ? <Loading label="Loading your market" /> : null}
        {d ? (
          <>
            <Section title="My listings" empty={d.listings.some((l) => OPEN_LISTING.includes(l.status)) ? null : 'Nothing listed. Use Sell on an ID in the IDs tab.'}>
              {d.listings
                .filter((l) => OPEN_LISTING.includes(l.status))
                .map((l) => (
                  <ListingRow key={l.id} listing={l} handle={handleOf(l.maxId, l.handle)} />
                ))}
            </Section>

            <Section title="Offers received" empty={d.offersReceived.length ? null : 'No open offers on your names.'}>
              {d.offersReceived.map((o) => (
                <ReceivedRow key={o.id} offer={o} handle={handleOf(o.maxId, o.handle)} inbox={d} payout={solanaPayouts(wallets.data, accounts)[0]?.address} />
              ))}
            </Section>

            <Section title="Offers made" empty={d.offersMade.length ? null : 'You haven’t made any offers.'}>
              {d.offersMade.map((o) => (
                <MadeRow
                  key={o.id}
                  offer={o}
                  onPay={() =>
                    pay({
                      kind: 'offer',
                      id: o.id,
                      handle: o.handle ?? '',
                      chain: o.chain,
                      asset: o.asset,
                      symbol: o.symbol,
                      price: o.amount,
                      priceBaseUnits: o.amountBaseUnits,
                      fromAddress: o.fromAddress,
                    })
                  }
                />
              ))}
            </Section>

            <Section title="Sales" empty={d.sales.length ? null : 'No sales yet.'}>
              {d.sales.map((s) => (
                <SaleRow
                  key={s.id}
                  sale={s}
                  handle={handleOf(s.maxId, s.handle)}
                  bought={mine.has(s.buyerAddress)}
                  onPay={() => {
                    // Pin to what the buyer agreed to before the sale existed (their offer) when
                    // known; otherwise to the sale's price, converted with trusted decimals and
                    // checked against the sale's own base units.
                    const o = s.offerId ? d.offersMade.find((x) => x.id === s.offerId) : undefined;
                    pay({
                      kind: 'sale',
                      id: s.id,
                      handle: s.handle ?? handleOf(s.maxId),
                      chain: o?.chain ?? s.chain,
                      asset: o?.asset ?? s.asset,
                      symbol: o?.symbol ?? s.symbol,
                      price: o?.amount ?? s.price,
                      priceBaseUnits: o?.amountBaseUnits ?? s.priceBaseUnits,
                      fromAddress: o?.fromAddress ?? s.buyerAddress,
                    });
                  }}
                />
              ))}
            </Section>
          </>
        ) : null}
      </ScrollView>
    </View>
  );
}

function Section({ title, empty, children }: { title: string; empty: string | null; children: ReactNode }) {
  return (
    <View style={{ gap: 10 }}>
      <SectionTitle>{title}</SectionTitle>
      <List>
        {empty ? (
          <AppText variant="small" style={{ padding: 16 }}>
            {empty}
          </AppText>
        ) : (
          children
        )}
      </List>
    </View>
  );
}

function Row({ children }: { children: ReactNode }) {
  return <View style={{ paddingVertical: 14, paddingHorizontal: 16, gap: 10, borderBottomWidth: 1, borderBottomColor: colors.sunken }}>{children}</View>;
}

function Actions({ children }: { children: ReactNode }) {
  return <View style={{ flexDirection: 'row', gap: 8, flexWrap: 'wrap' }}>{children}</View>;
}

function ListingRow({ listing: l, handle }: { listing: Listing; handle: string }) {
  return (
    <Row>
      <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
        <Handle name={handle} size={15} />
        <ListingStatusPill status={l.status} />
      </View>
      <AppText variant="small">
        {displayDecimal(l.price)} {l.symbol} · listed {relativeTime(l.createdAt)}
      </AppText>
      <Actions>
        <Button label={l.status === 'active' ? 'Edit or delist' : 'View'} variant="secondary" size="sm" onPress={() => router.push({ pathname: '/sell', params: { id: l.maxId } })} />
      </Actions>
    </Row>
  );
}

function ReceivedRow({ offer: o, handle, inbox, payout }: { offer: ReceivedOffer; handle: string; inbox: MarketInbox; payout?: string }) {
  const invalidate = useInvalidateMarket();
  const [pending, setPending] = useState<'accept' | 'decline' | null>(null);
  const [error, setError] = useState<string | null>(null);
  const listed = inbox.listings.some((l) => l.maxId === o.maxId && l.status === 'active');
  const dec = decimalsFrom(o.amount, o.amountBaseUnits);
  // Display only: MAX computes the actual split.
  const receive = dec != null ? `${displayUnits(sellerReceives(BigInt(o.amountBaseUnits), DEFAULT_FEE_BPS), dec)} ${o.symbol}` : null;

  async function run(kind: 'accept' | 'decline') {
    setPending(kind);
    setError(null);
    try {
      if (kind === 'accept') {
        const res = await max().market.acceptOffer(o.id, listed ? {} : { payoutAddress: payout });
        toast(isPendingApproval(res) ? 'Held for approval' : 'Offer accepted. The buyer has 24 hours to pay.');
      } else {
        await max().market.declineOffer(o.id);
        toast('Offer declined');
      }
      invalidate();
    } catch (err) {
      setError(marketErrorMessage(err));
    } finally {
      setPending(null);
    }
  }

  function accept() {
    if (!listed && !payout) {
      setError('Link a Solana wallet to your account to get paid, then accept.');
      return;
    }
    Alert.alert(
      'Accept this offer?',
      [
        `${displayDecimal(o.amount)} ${o.symbol} for ${handle}.`,
        receive ? `You receive about ${receive} after MAX’s fee.` : null,
        !listed && payout ? `Paid to ${shortAddress(payout)}.` : null,
        'The name is reserved for the buyer for 24 hours while they pay. Other offers on it close, and your listing (if any) is paused.',
      ]
        .filter(Boolean)
        .join('\n\n'),
      [
        { text: 'Cancel', style: 'cancel' },
        { text: 'Accept', onPress: () => void run('accept') },
      ],
    );
  }

  function decline() {
    Alert.alert('Decline this offer?', `${displayDecimal(o.amount)} ${o.symbol} for ${handle}.`, [
      { text: 'Cancel', style: 'cancel' },
      { text: 'Decline', style: 'destructive', onPress: () => void run('decline') },
    ]);
  }

  return (
    <Row>
      <AppText weight="semibold" size={15}>
        {displayDecimal(o.amount)} {o.symbol}
      </AppText>
      <AppText variant="small">
        for <Handle name={handle} size={13} /> · expires {timeUntil(o.expiresAt)}
      </AppText>
      <Actions>
        <Button label="Accept" size="sm" loading={pending === 'accept'} disabled={!!pending} onPress={accept} />
        <Button label="Decline" variant="ghost" size="sm" loading={pending === 'decline'} disabled={!!pending} onPress={decline} />
      </Actions>
      {error ? <Banner tone="danger">{error}</Banner> : null}
    </Row>
  );
}

function MadeRow({ offer: o, onPay }: { offer: Offer; onPay: () => void }) {
  const invalidate = useInvalidateMarket();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const open = o.status === 'live' || o.status === 'unfunded';

  function withdraw() {
    Alert.alert('Withdraw this offer?', `${displayDecimal(o.amount)} ${o.symbol}${o.handle ? ` for ${o.handle}` : ''}.`, [
      { text: 'Keep it', style: 'cancel' },
      {
        text: 'Withdraw',
        style: 'destructive',
        onPress: async () => {
          setPending(true);
          setError(null);
          try {
            await max().market.withdrawOffer(o.id);
            invalidate();
            toast('Offer withdrawn');
          } catch (err) {
            setError(marketErrorMessage(err));
          } finally {
            setPending(false);
          }
        },
      },
    ]);
  }

  return (
    <Row>
      <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
        <AppText weight="semibold" size={15}>
          {displayDecimal(o.amount)} {o.symbol}
        </AppText>
        <OfferStatusPill status={o.status} />
      </View>
      <AppText variant="small">
        for {o.handle ? <Handle name={o.handle} size={13} /> : 'a name'}
        {open ? ` · expires ${timeUntil(o.expiresAt)}` : ''} · from {shortAddress(o.fromAddress)}
      </AppText>
      {o.status === 'unfunded' ? (
        <Banner tone="danger">{`The paying wallet no longer holds ${displayDecimal(o.amount)} ${o.symbol} plus fees, so the owner can’t see this offer. Top it up to make it visible again.`}</Banner>
      ) : null}
      {o.status === 'accepted' ? <Banner tone="clay">Accepted: pay within 24 hours of acceptance, or the offer lapses and counts against you.</Banner> : null}
      {o.status === 'declined' && o.declineNote ? <AppText variant="small">Note from the owner: {o.declineNote}</AppText> : null}
      {o.status === 'accepted' || open ? (
        <Actions>
          {o.status === 'accepted' ? <Button label="Pay" size="sm" onPress={onPay} /> : null}
          {open ? <Button label="Withdraw" variant="ghost" size="sm" loading={pending} onPress={withdraw} /> : null}
        </Actions>
      ) : null}
      {error ? <Banner tone="danger">{error}</Banner> : null}
    </Row>
  );
}

function SaleRow({ sale: s, handle, bought, onPay }: { sale: Sale; handle: string; bought: boolean; onPay: () => void }) {
  const payable = bought && !s.txHash && (s.status === 'locked' || s.status === 'pending_review');
  return (
    <Row>
      <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
        <Handle name={handle} size={15} />
        <SaleStatusPill status={s.status} />
      </View>
      <AppText variant="small">
        {bought ? 'Bought' : 'Sold'} for {displayDecimal(s.price)} {s.symbol}
        {!bought ? ` · you receive ${displayDecimal(s.sellerReceives)}` : ''} · {formatDate(s.completedAt ?? s.createdAt)}
      </AppText>
      {payable ? (
        <Actions>
          <Button label={s.status === 'pending_review' ? 'Check status' : 'Pay'} size="sm" onPress={onPay} />
        </Actions>
      ) : null}
    </Row>
  );
}
