import { isPendingApproval, type Listing, type MarketAsset, type MarketEligibility, type MaxId } from '@max/sdk';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Alert, Keyboard, View } from 'react-native';

import { ListingStatusPill, solanaPayouts } from '@/components/market/shared';
import { AppText, Handle, Mono } from '@/components/ui/AppText';
import { Banner, Choice, Loading } from '@/components/ui/Blocks';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Fields';
import { Screen, TitleHeader } from '@/components/ui/Screen';
import { max } from '@/api/client';
import { listableReason, marketErrorMessage } from '@/error/market';
import { useInvalidateMarket, useMarketEligibility, useMarketInbox, useMyIds, useWallets } from '@/hooks/queries';
import { toast } from '@/store/flows';
import { useSession } from '@/store/session';
import { colors } from '@/theme';
import { shortAddress } from '@/utils/format';
import { displayDecimal, displayUnits, sellerPercent, sellerReceives, toBaseUnits } from '@/utils/money';

const OPEN = ['active', 'locked'];

/**
 * List a MAX ID for sale, or change its price / delist it. Shows exactly what
 * the seller receives: price − floor(price × feeBps / 10000), in base units.
 */
export default function Sell() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const ids = useMyIds();
  const maxId = ids.data?.find((x) => x.id === id);
  const elig = useMarketEligibility(id ?? '');
  const inbox = useMarketInbox();
  const close = () => (router.canGoBack() ? router.back() : router.replace('/ids'));
  const listing = inbox.data?.listings.find((l) => l.maxId === id && OPEN.includes(l.status));

  let body;
  // Never start from a cached eligibility: a stale "listable" would offer the wrong form.
  if (ids.isLoading || inbox.isLoading || !elig.isFetchedAfterMount) body = <Loading label="Checking this name" />;
  else if (!maxId) body = <Banner tone="danger">That MAX ID isn’t yours.</Banner>;
  else if (elig.error || !elig.data) body = <Banner tone="danger">{marketErrorMessage(elig.error)}</Banner>;
  else if (inbox.error) body = <Banner tone="danger">{marketErrorMessage(inbox.error)}</Banner>;
  if (body || !maxId || !elig.data) {
    return (
      <Screen topPadding={12} header={<TitleHeader title="Sell" onBack={close} />}>
        {body}
      </Screen>
    );
  }
  return <SellForm maxId={maxId} eligibility={elig.data} listing={listing} onClose={close} />;
}

function SellForm({ maxId, eligibility: e, listing, onClose }: { maxId: MaxId; eligibility: MarketEligibility; listing?: Listing; onClose: () => void }) {
  const invalidate = useInvalidateMarket();
  const wallets = useWallets();
  const accounts = useSession((s) => s.accounts);
  const [assetKey, setAssetKey] = useState('');
  const [price, setPrice] = useState(listing?.price ?? '');
  const [payout, setPayout] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState<'save' | 'delist' | null>(null);

  const title = listing ? 'Your listing' : 'Sell';
  const header = <TitleHeader title={title} onBack={pending ? undefined : onClose} />;

  if (listing?.status === 'locked') {
    return (
      <Screen topPadding={12} gap={18} header={header}>
        <Handle name={maxId.handle} size={24} />
        <ListingStatusPill status={listing.status} />
        <Banner tone="clay" title="A sale is in progress">
          {`A buyer is paying ${displayDecimal(listing.price)} ${listing.symbol} for this name. The listing can’t be changed until the sale completes or its payment window closes.`}
        </Banner>
      </Screen>
    );
  }
  // An existing listing already passed the checks; only its price changes.
  if (!listing && !e.listable) {
    return (
      <Screen topPadding={12} gap={18} header={header}>
        <Handle name={maxId.handle} size={24} />
        <Banner tone="clay" title="This name can’t be listed yet">
          {listableReason(e.reason, { listableAt: e.listableAt, renameAvailableAt: e.renameAvailableAt })}
        </Banner>
      </Screen>
    );
  }
  if (!e.assets.length) {
    return (
      <Screen topPadding={12} header={header}>
        <Banner>No payment token is enabled on the marketplace yet.</Banner>
      </Screen>
    );
  }

  const asset: MarketAsset =
    (listing && e.assets.find((a) => a.chain === listing.chain && a.asset === listing.asset)) || e.assets.find((a) => `${a.chain}:${a.asset}` === assetKey) || e.assets[0]!;
  const payouts = solanaPayouts(wallets.data, accounts);
  const payoutAddress = payout || payouts[0]?.address || '';
  const p = price.trim();
  const base = p ? toBaseUnits(p, asset.decimals) : null;
  const min = toBaseUnits(e.minPrice, asset.decimals) ?? 0n;
  const priceErr = !p
    ? null
    : base === null
      ? `Enter a price like 250 or 250.50 (up to ${asset.decimals} decimals).`
      : base < min
        ? `The minimum price is ${displayDecimal(e.minPrice)} ${asset.symbol}.`
        : null;
  const receive = base !== null && !priceErr ? sellerReceives(base, e.feeBps) : null;
  const unchanged = !!listing && base !== null && toBaseUnits(listing.price, asset.decimals) === base;
  const ok = base !== null && !priceErr && !unchanged && (listing || payoutAddress);
  const here = (address: string) => accounts.find((a) => a.solana === address)?.label;

  async function save() {
    Keyboard.dismiss();
    setError(null);
    setPending('save');
    try {
      const res = listing
        ? await max().market.updateListing(listing.id, p)
        : await max().market.list({ maxId: maxId.id, price: p, chain: asset.chain, asset: asset.asset, payoutAddress });
      invalidate();
      toast(isPendingApproval(res) ? 'Held for approval by the agent’s administrator' : listing ? 'Price updated' : `${maxId.handle} is listed`);
      onClose();
    } catch (err) {
      setError(marketErrorMessage(err));
    } finally {
      setPending(null);
    }
  }

  function delist() {
    if (!listing) return;
    Alert.alert('Delist this name?', `${maxId.handle} will no longer be for sale. Open offers stay open.`, [
      { text: 'Keep listed', style: 'cancel' },
      {
        text: 'Delist',
        style: 'destructive',
        onPress: async () => {
          setError(null);
          setPending('delist');
          try {
            await max().market.delist(listing.id);
            invalidate();
            toast(`${maxId.handle} delisted`);
            onClose();
          } catch (err) {
            setError(marketErrorMessage(err));
          } finally {
            setPending(null);
          }
        },
      },
    ]);
  }

  return (
    <Screen
      scroll
      gap={22}
      header={header}
      footer={
        <>
          <Button label={listing ? 'Update price' : 'List for sale'} disabled={!ok || !!pending} loading={pending === 'save'} onPress={() => void save()} />
          {listing ? <Button label="Delist" variant="danger" size="md" loading={pending === 'delist'} disabled={!!pending} onPress={delist} /> : null}
        </>
      }>
      <View style={{ gap: 8 }}>
        <Handle name={maxId.handle} size={24} />
        {listing ? <ListingStatusPill status={listing.status} /> : null}
        <AppText variant="small">When it sells, the buyer pays you and MAX’s {e.feeBps / 100}% fee in one transaction. The name moves to the buyer, and its wallets and profile are reset.</AppText>
      </View>

      {!listing && e.assets.length > 1 ? (
        <View style={{ gap: 8 }}>
          <AppText variant="label">Get paid in</AppText>
          <View style={{ flexDirection: 'row', gap: 8, flexWrap: 'wrap' }}>
            {e.assets.map((a) => (
              <Choice key={`${a.chain}:${a.asset}`} label={`${a.symbol} · ${a.chain}`} selected={a === asset} onPress={() => setAssetKey(`${a.chain}:${a.asset}`)} />
            ))}
          </View>
        </View>
      ) : null}

      <TextField
        label={`Price (${asset.symbol})`}
        value={price}
        onChangeText={(t) => {
          setError(null);
          setPrice(t.replace(',', '.'));
        }}
        keyboardType="decimal-pad"
        placeholder="0.00"
        error={priceErr}
        hint={`Minimum ${displayDecimal(e.minPrice)} ${asset.symbol}. Buyers pay this price.`}
      />

      <View accessibilityLiveRegion="polite" style={{ padding: 14, borderRadius: 14, backgroundColor: colors.successSoft }}>
        <AppText weight="semibold" size={15} color={colors.successText}>
          You receive {sellerPercent(e.feeBps)}%: {receive !== null ? displayUnits(receive, asset.decimals) : '0'} {asset.symbol}
        </AppText>
      </View>

      {listing ? (
        <AppText variant="small">
          Paid to <Mono size={13}>{shortAddress(listing.payoutAddress)}</Mono>, the wallet you chose when listing. To change it, delist and list again.
        </AppText>
      ) : payouts.length ? (
        <View style={{ gap: 8 }}>
          <AppText variant="label">Get paid to</AppText>
          <View style={{ flexDirection: 'row', gap: 8, flexWrap: 'wrap' }}>
            {payouts.map((w) => (
              <Choice key={w.id} label={`${here(w.address) ?? 'Linked wallet'} · ${shortAddress(w.address)}`} selected={payoutAddress === w.address} onPress={() => setPayout(w.address)} />
            ))}
          </View>
        </View>
      ) : wallets.isLoading ? (
        <Loading label="Loading your wallets" />
      ) : (
        <Banner tone="clay">{`Link a Solana wallet to your account to get paid. Sales settle on Solana in ${asset.symbol}.`}</Banner>
      )}

      {error ? <Banner tone="danger">{error}</Banner> : null}
    </Screen>
  );
}
