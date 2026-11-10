import { isMaxApiError, isPendingApproval, normalizeName } from '@max/sdk';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Keyboard, View } from 'react-native';

import { defaultOfferAsset, isSolanaChain, solanaPayers } from '@/components/market/shared';
import { AppText, Handle, Mono } from '@/components/ui/AppText';
import { Banner, Card, Choice, Loading } from '@/components/ui/Blocks';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Fields';
import { Screen, TitleHeader } from '@/components/ui/Screen';
import { MaxStatusBanner } from '@/components/wallet/MaxStatusBanner';
import { max } from '@/api/client';
import { marketErrorMessage } from '@/error/market';
import { useInvalidateMarket, useMarketName, useMyIds, useTrustedDecimals } from '@/hooks/queries';
import { hasRpc } from '@/services/chain';
import { toast, useSaleDraft } from '@/store/flows';
import { useSession } from '@/store/session';
import { colors } from '@/theme';
import { formatDate, relativeTime, shortAddress } from '@/utils/format';
import { displayDecimal, toBaseUnits } from '@/utils/money';

type OfferAsset = { chain: string; asset: string; symbol: string };

/** One name on the market: price, Buy now, Make offer; last sale. */
export default function MarketName() {
  const { name: raw } = useLocalSearchParams<{ name: string }>();
  const name = normalizeName(raw ?? '') ?? '';
  const view = useMarketName(name || null);
  const myIds = useMyIds();
  const maxStatus = useSession((s) => s.max);
  const accounts = useSession((s) => s.accounts);
  const payers = solanaPayers(accounts);
  const [payer, setPayer] = useState<string | null>(null);
  const [offering, setOffering] = useState(false);
  const from = payer ?? payers[0]?.address ?? null;
  const close = () => (router.canGoBack() ? router.back() : router.replace('/market'));

  const v = view.data;
  const header = <TitleHeader title="Market" onBack={close} />;
  if (!v) {
    return (
      <Screen topPadding={12} header={header}>
        {view.isLoading ? (
          <Loading label="Loading this name" />
        ) : (
          <Banner tone="danger">{isMaxApiError(view.error, 'MARKET_DISABLED') ? 'The marketplace isn’t open yet.' : view.error ? marketErrorMessage(view.error) : 'That name isn’t valid.'}</Banner>
        )}
      </Screen>
    );
  }

  const mine = myIds.data?.find((i) => i.id === v.maxId || i.name === name);
  const owned = !!mine;
  const listing = v.listing;
  const active = listing?.status === 'active';
  // v1 settles in USDC on Solana (spec §2): offers use the listing's asset, else the last sale's, else the configured one.
  const offerAsset: OfferAsset | null = listing
    ? { chain: listing.chain, asset: listing.asset, symbol: listing.symbol }
    : v.lastSale
      ? { chain: v.lastSale.chain, asset: v.lastSale.asset, symbol: v.lastSale.symbol }
      : defaultOfferAsset();
  const canOffer = !owned && (active || v.listable) && !!offerAsset;
  const ready = maxStatus === 'ready';
  const payable = !!offerAsset && isSolanaChain(offerAsset.chain) && hasRpc(offerAsset.chain);

  function buy() {
    if (!listing || !from) return;
    useSaleDraft.getState().setTarget({
      kind: 'listing',
      id: listing.id,
      handle: v!.handle,
      chain: listing.chain,
      asset: listing.asset,
      symbol: listing.symbol,
      price: listing.price,
      fromAddress: from,
    });
    router.push({ pathname: '/review', params: { flow: 'sale' } });
  }

  return (
    <Screen scroll gap={20} header={header}>
      <View style={{ gap: 6 }}>
        <Handle name={v.handle} size={26} />
        {listing ? (
          <>
            <AppText style={{ fontSize: 36 }} weight="semibold">
              {displayDecimal(listing.price)} <AppText size={18} color={colors.muted}>{listing.symbol}</AppText>
            </AppText>
            <AppText variant="small">
              {active ? `Listed for sale ${relativeTime(listing.createdAt)}` : 'A sale is in progress for this name.'}
              {v.liveOfferCount ? ` · ${v.liveOfferCount} open offer${v.liveOfferCount === 1 ? '' : 's'}` : ''}
            </AppText>
          </>
        ) : (
          <AppText variant="small">
            Not listed for sale.{v.liveOfferCount ? ` ${v.liveOfferCount} open offer${v.liveOfferCount === 1 ? '' : 's'}.` : ''}
          </AppText>
        )}
        {v.lastSale ? (
          <AppText variant="small">
            Last sold for {displayDecimal(v.lastSale.price)} {v.lastSale.symbol} on {formatDate(v.lastSale.completedAt)}.
          </AppText>
        ) : null}
      </View>

      <MaxStatusBanner />

      {owned ? (
        <Card>
          <View style={{ gap: 12 }}>
            <AppText size={15}>This name is yours.</AppText>
            {mine?.isDefault ? (
              // Default IDs are never listable, so there is nothing to sell.
              <AppText variant="small">It’s your default ID, for receiving, and can’t be sold.</AppText>
            ) : (
              <Button
                label={active ? 'Edit listing' : listing ? 'View in My market' : 'Sell it'}
                variant="secondary"
                size="md"
                onPress={() => {
                  if (listing && !active) router.push('/market/offers');
                  else if (mine) router.push({ pathname: '/sell', params: { id: mine.id } });
                }}
              />
            )}
          </View>
        </Card>
      ) : !listing && !canOffer ? (
        <Banner>{v.reason ? 'This name isn’t available on the market right now.' : 'This name isn’t for sale.'}</Banner>
      ) : (
        <>
          {(active || canOffer) && payers.length > 1 ? (
            <View style={{ gap: 8 }}>
              <AppText variant="label">Pay from</AppText>
              <View style={{ flexDirection: 'row', gap: 8, flexWrap: 'wrap' }}>
                {payers.map((p) => (
                  <Choice key={p.address} label={`${p.label} · ${shortAddress(p.address)}`} selected={from === p.address} onPress={() => setPayer(p.address)} />
                ))}
              </View>
            </View>
          ) : null}
          {!payers.length ? <Banner tone="clay">Sales are paid from a Solana account. Add one on this phone to buy or make offers.</Banner> : null}
          {offerAsset && !payable ? <Banner tone="clay">{`${offerAsset.chain} isn’t set up in this app, so you can’t pay from this phone.`}</Banner> : null}
          {offering && offerAsset && from ? (
            <OfferForm name={name} handle={v.handle} asset={offerAsset} fromAddress={from} listPrice={active ? listing?.price : undefined} onDone={() => setOffering(false)} />
          ) : (
            <View style={{ gap: 10 }}>
              {active ? <Button label="Buy now" disabled={!ready || !from || !payable} onPress={buy} /> : null}
              {canOffer ? (
                <Button label="Make offer" variant={active ? 'secondary' : 'primary'} disabled={!ready || !from || !payable} onPress={() => setOffering(true)} />
              ) : null}
              {active ? (
                <AppText variant="small">
                  Buy now reserves the name for you for 15 minutes and shows the full quote, including the network fee, before anything is signed. {from ? (
                    <>
                      You pay from <Mono size={13}>{shortAddress(from)}</Mono>.
                    </>
                  ) : null}
                </AppText>
              ) : null}
            </View>
          )}
        </>
      )}
    </Screen>
  );
}

const EXPIRY_DAYS = [1, 3, 7, 14, 30] as const;
const daysFromNow = (days: number) => new Date(Date.now() + days * 86_400_000).toISOString();

/** Make an offer: nothing is charged now; it's paid from `fromAddress` if the owner accepts. */
function OfferForm({ name, handle, asset, fromAddress, listPrice, onDone }: { name: string; handle: string; asset: OfferAsset; fromAddress: string; listPrice?: string; onDone: () => void }) {
  const invalidate = useInvalidateMarket();
  const trusted = useTrustedDecimals(asset.chain, asset.asset);
  const [amount, setAmount] = useState('');
  const [days, setDays] = useState<number>(7);
  const [failure, setFailure] = useState<unknown>(null);
  const [pending, setPending] = useState(false);
  const a = amount.trim();
  const amountOk = /^\d+(\.\d+)?$/.test(a) && (trusted.decimals == null ? Number(a) > 0 : (toBaseUnits(a, trusted.decimals) ?? 0n) > 0n);
  const error = failure == null ? null : marketErrorMessage(failure, { decimals: trusted.decimals, symbol: asset.symbol, amount: a });

  async function submit() {
    Keyboard.dismiss();
    setFailure(null);
    setPending(true);
    try {
      const res = await max().market.offer({ name, amount: a, chain: asset.chain, asset: asset.asset, fromAddress, expiresAt: daysFromNow(days) });
      invalidate();
      toast(isPendingApproval(res) ? 'Offer held for approval by the agent’s administrator' : `Offer sent for ${handle}`);
      onDone();
    } catch (err) {
      setFailure(err);
    } finally {
      setPending(false);
    }
  }

  return (
    <Card>
      <View style={{ gap: 16 }}>
        <AppText weight="semibold" size={16} accessibilityRole="header">
          Make an offer
        </AppText>
        {listPrice ? <AppText variant="small">Listed at {displayDecimal(listPrice)} {asset.symbol}.</AppText> : null}
        <TextField
          label={`Your offer (${asset.symbol})`}
          value={amount}
          onChangeText={(t) => {
            setFailure(null);
            setAmount(t.replace(',', '.'));
          }}
          keyboardType="decimal-pad"
          placeholder="0.00"
          error={a && !amountOk ? 'Enter an amount like 25 or 25.50' : null}
        />
        <View style={{ gap: 8 }}>
          <AppText variant="label">Open for</AppText>
          <View style={{ flexDirection: 'row', gap: 6, flexWrap: 'wrap' }}>
            {EXPIRY_DAYS.map((d) => (
              <Choice key={d} pill label={`${d} day${d === 1 ? '' : 's'}`} selected={days === d} onPress={() => setDays(d)} />
            ))}
          </View>
        </View>
        <AppText variant="small">
          Nothing is charged now. {shortAddress(fromAddress)} must keep holding the amount plus a little SOL for fees, or the owner won’t see the offer. If it’s accepted you’ll have 24 hours to
          pay; unpaid accepted offers count against you.
        </AppText>
        {error ? <Banner tone="danger">{error}</Banner> : null}
        <Button label={amountOk ? `Offer ${displayDecimal(a)} ${asset.symbol}` : 'Make offer'} disabled={!amountOk} loading={pending} onPress={() => void submit()} />
        <Button label="Cancel" variant="ghost" size="md" disabled={pending} onPress={onDone} />
      </View>
    </Card>
  );
}
