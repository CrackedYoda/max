import { isPendingApproval, type SaleView } from '@max/sdk';
import { useQueryClient } from '@tanstack/react-query';
import * as Haptics from 'expo-haptics';
import { Redirect, router } from 'expo-router';
import { useEffect, useState } from 'react';
import { ActivityIndicator, Linking, Pressable, ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { StepUpModal } from '@/components/auth/StepUpModal';
import { AppText, Handle, Mono } from '@/components/ui/AppText';
import { Banner, ReviewRow } from '@/components/ui/Blocks';
import { Button, IconButton, LinkButton } from '@/components/ui/Button';
import { CheckRow, CopyButton } from '@/components/ui/Fields';
import { Icon } from '@/components/ui/Icon';
import { max } from '@/api/client';
import { marketErrorMessage } from '@/error/market';
import { fetchTrustedDecimals, SALE_TERMINAL, useChains, useInvalidateMarket, useSale, useTrustedDecimals } from '@/hooks/queries';
import { useCountdown } from '@/hooks/useCountdown';
import { broadcastSale, ChainError, findSalePayment, hasRpc, signSale } from '@/services/chain';
import { stepUp } from '@/services/session';
import * as vault from '@/services/vault';
import { useSaleDraft, type SaleTarget } from '@/store/flows';
import { getGeneration, useSession } from '@/store/session';
import { colors, fonts, spacing } from '@/theme';
import { shortAddress } from '@/utils/format';
import { displayDecimal, pinnedPriceBase } from '@/utils/money';

import { isSolanaChain, SaleStatusPill } from './shared';

type Phase =
  | { k: 'starting' }
  | { k: 'start_failed' }
  /** An agent's action held by its administrator (MarketResult PendingApproval). */
  | { k: 'approval' }
  | { k: 'held'; saleId: string }
  | { k: 'review'; view: SaleView }
  | { k: 'working'; view: SaleView; step: 'sign' | 'broadcast' | 'record' }
  /** The send may have gone out: find it on chain before anything is signed again. */
  | { k: 'uncertain'; view: SaleView; lookup: 'checking' | 'not_found' | 'failed' }
  | { k: 'record_failed'; view: SaleView; txHash: string; error: string }
  | { k: 'watching'; saleId: string };

const STEP = { sign: 'Signing…', broadcast: 'Sending…', record: 'Recording with MAX…' } as const;

/**
 * Marketplace payment, as a variant of the review sheet: lock (buy) or fetch
 * (offer / held sale) → quote with the lock countdown → Face ID/passcode →
 * verify the instructions against what the user tapped (trusted decimals) →
 * sign once on this phone → broadcast → record → poll the sale to the end.
 *
 * A definite "not sent" goes back to review. An uncertain send is never
 * followed by a plain re-sign: the phone looks for the payment on chain (its
 * own signature, then the `max:sale:<id>` memo) and records it if found;
 * otherwise only the same signed bytes may be re-sent, or the user must
 * confirm they checked their wallet before a new signature.
 */
export function SaleReview() {
  const target = useSaleDraft((s) => s.target);
  if (!target) return <Redirect href="/market" />;
  return <SaleFlow target={target} />;
}

function SaleFlow({ target: t }: { target: SaleTarget }) {
  const insets = useSafeAreaInsets();
  const qc = useQueryClient();
  const invalidate = useInvalidateMarket();
  const accounts = useSession((s) => s.accounts);
  const chains = useChains();
  const chain = chains.data?.find((c) => c.key === t.chain);
  const nativeSymbol = chain?.nativeCoin ?? 'SOL';
  const trusted = useTrustedDecimals(t.chain, t.asset);
  const [rawPhase, setPhase] = useState<Phase>({ k: 'starting' });
  // Raw error, formatted at render so late-arriving token decimals still apply.
  const [failure, setFailure] = useState<unknown>(null);
  const [checked, setChecked] = useState(false);
  const units = { decimals: trusted.decimals, symbol: t.symbol, nativeSymbol, nativeDecimals: chain?.nativeDecimals ?? 9, amount: t.price };
  const error = failure == null ? null : marketErrorMessage(failure, units);

  const watchId = rawPhase.k === 'held' || rawPhase.k === 'watching' ? rawPhase.saleId : null;
  const sale = useSale(watchId);

  // Plain functions: they read the draft through getState and only set state,
  // so the mount-time copies the start effect captures stay correct.
  async function record(view: SaleView, txHash: string) {
    setPhase({ k: 'working', view, step: 'record' });
    try {
      await max().market.submit(view.sale.id, txHash);
      invalidate();
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success).catch(() => {});
      setFailure(null);
      setPhase({ k: 'watching', saleId: view.sale.id });
    } catch (err) {
      // Already on its way: only MAX's record of it failed. Never sign again.
      setPhase({ k: 'record_failed', view, txHash, error: marketErrorMessage(err) });
    }
  }

  /** After an uncertain send: find this sale's payment on chain before anyone signs again. */
  async function lookForPayment(view: SaleView) {
    setChecked(false);
    setPhase({ k: 'uncertain', view, lookup: 'checking' });
    const known = useSaleDraft.getState().signed[view.sale.id]?.hash;
    try {
      const sig = await findSalePayment(view.sale.chain, t.fromAddress, view.sale.id, known);
      if (sig) await record(view, sig);
      else setPhase({ k: 'uncertain', view, lookup: 'not_found' });
    } catch {
      setPhase({ k: 'uncertain', view, lookup: 'failed' });
    }
  }

  function advance(view: SaleView) {
    useSaleDraft.getState().setView(view);
    const s = view.sale;
    // Signed on this phone earlier but never recorded: it may be on chain.
    if (!s.txHash && useSaleDraft.getState().signed[s.id] && !['completed', 'cancelled', 'reversed'].includes(s.status)) void lookForPayment(view);
    else setPhase(phaseFor(view));
  }

  async function start() {
    const gen = getGeneration();
    setFailure(null);
    setPhase({ k: 'starting' });
    try {
      if (!isSolanaChain(t.chain)) throw new Error('Market sales settle on Solana only.');
      if (!hasRpc(t.chain)) throw new Error(`${t.chain} isn’t set up in this app.`);
      if (!vault.keyFor('solana', t.fromAddress)) throw new Error('The paying account isn’t on this phone.');
      const d = useSaleDraft.getState();
      const res =
        t.kind === 'listing'
          ? await max().market.buy(t.id, { fromAddress: t.fromAddress, idempotencyKey: d.keyFor(JSON.stringify(['buy', t.id, t.fromAddress])) })
          : t.kind === 'offer'
            ? await max().market.payOffer(t.id, { idempotencyKey: d.keyFor(JSON.stringify(['pay', t.id])) })
            : await max().market.getSale(t.id);
      if (gen !== getGeneration()) return;
      if (isPendingApproval(res)) {
        setPhase({ k: 'approval' });
        return;
      }
      invalidate();
      advance(res);
    } catch (err) {
      if (gen !== getGeneration()) return;
      setFailure(err);
      setPhase({ k: 'start_failed' });
    }
  }

  // Start once on open (deferred so StrictMode's mount/unmount/mount runs it once).
  useEffect(() => {
    const timer = setTimeout(() => void start(), 0);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const terminal = !!sale.data && SALE_TERMINAL.includes(sale.data.sale.status);
  useEffect(() => {
    if (terminal) invalidate();
  }, [terminal, invalidate]);

  async function sendSigned(view: SaleView, retryingUncertain = false) {
    const s = useSaleDraft.getState().signed[view.sale.id];
    if (!s) return;
    setPhase({ k: 'working', view, step: 'broadcast' });
    try {
      await broadcastSale(view.sale.chain, s);
    } catch (err) {
      setFailure(err);
      // A signature whose fate is unknown is never dropped, whatever the retry says.
      if (retryingUncertain || (err instanceof ChainError && err.ambiguous)) {
        await lookForPayment(view);
        return;
      }
      // Definitely not sent: drop the signature and go back to review.
      useSaleDraft.getState().setSigned(view.sale.id, null);
      setPhase({ k: 'review', view });
      return;
    }
    await record(view, s.hash);
  }

  /** Confirm it's you, pin the price, sign once, send. `replace` re-signs after the wallet check. */
  async function confirm(view: SaleView, replace = false) {
    setFailure(null);
    const back = () => setPhase(replace ? { k: 'uncertain', view, lookup: 'not_found' } : { k: 'review', view });
    if (lockPassed(view)) {
      setFailure('The payment window closed. Nothing was signed.');
      return back();
    }
    // The price the user saw, converted with trusted decimals (env pin cross-checked
    // with the mint on chain); the API's base units must agree. Never the sale's own.
    let priceBaseUnits: string;
    try {
      const pin = pinnedPriceBase(t.price, await fetchTrustedDecimals(qc, t.chain, t.asset), t.priceBaseUnits);
      if ('error' in pin) throw new Error(pin.error);
      priceBaseUnits = pin.base;
    } catch (err) {
      setFailure(err);
      return back();
    }
    const ok = await stepUp(`Approve paying ${displayDecimal(t.price)} ${t.symbol} for ${t.handle}.`);
    if (!ok) return;
    const draft = useSaleDraft.getState();
    if (replace) draft.setSigned(view.sale.id, null);
    if (!useSaleDraft.getState().signed[view.sale.id]) {
      try {
        setPhase({ k: 'working', view, step: 'sign' });
        const signed = await signSale(view, { buyer: t.fromAddress, asset: t.asset, priceBaseUnits });
        useSaleDraft.getState().setSigned(view.sale.id, signed);
      } catch (err) {
        setFailure(err);
        setPhase({ k: 'review', view });
        return;
      }
    }
    await sendSigned(view);
  }

  const close = () => {
    useSaleDraft.getState().close();
    if (router.canGoBack()) router.back();
    else router.replace('/market');
  };

  // A held (review-threshold) sale comes back locked with instructions once approved.
  const phase = rawPhase.k === 'held' && sale.data && sale.data.sale.status !== 'pending_review' ? phaseFor(sale.data) : rawPhase;

  if (phase.k === 'watching') {
    return <SaleOutcome saleId={phase.saleId} live={sale.data ?? null} target={t} explorerTxUrl={chain?.explorerTxUrl} onDone={close} />;
  }

  const working = phase.k === 'working';
  const view = phase.k === 'review' || phase.k === 'working' || phase.k === 'uncertain' || phase.k === 'record_failed' ? phase.view : null;
  const payer = accounts.find((a) => a.solana === t.fromAddress);

  return (
    <View style={{ flex: 1, backgroundColor: colors.scrim }}>
      <Pressable style={{ flex: 1, minHeight: 80 }} accessibilityLabel="Close" onPress={working ? undefined : close} />
      <View accessibilityViewIsModal style={{ backgroundColor: colors.surface, borderTopLeftRadius: 28, borderTopRightRadius: 28, paddingTop: 12, paddingHorizontal: spacing.gutter, paddingBottom: insets.bottom + 16, gap: 18, maxHeight: '92%' }}>
        <View style={{ alignSelf: 'center', width: 40, height: 5, borderRadius: 999, backgroundColor: colors.line }} />
        <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginRight: -10 }}>
          <AppText variant="title" accessibilityRole="header">
            {t.kind === 'listing' ? 'Buy this name' : 'Pay for this name'}
          </AppText>
          {working ? <View style={{ width: 44 }} /> : <IconButton icon="close" label="Close" color={colors.muted} onPress={close} />}
        </View>

        <ScrollView contentContainerStyle={{ gap: 18 }} bounces={false}>
          <View style={{ alignItems: 'center', gap: 4, paddingTop: 4, paddingBottom: 8 }}>
            <AppText style={{ fontFamily: fonts.display, fontSize: 40 }} numberOfLines={1} adjustsFontSizeToFit>
              {displayDecimal(t.price)} <AppText style={{ fontFamily: fonts.display, fontSize: 22, color: colors.muted }}>{t.symbol}</AppText>
            </AppText>
            <AppText size={14} color={colors.muted}>
              for <Handle name={t.handle} size={14} accent={colors.clayText} />
            </AppText>
          </View>

          {phase.k === 'starting' ? (
            <View style={{ padding: 24, alignItems: 'center', gap: 10 }}>
              <ActivityIndicator color={colors.muted} />
              <AppText variant="small">{t.kind === 'listing' ? 'Reserving the name for you…' : 'Getting a fresh quote…'}</AppText>
            </View>
          ) : null}
          {phase.k === 'start_failed' ? (
            <Banner tone="danger" title={t.kind === 'listing' ? 'Couldn’t reserve this name' : 'Couldn’t prepare this payment'}>
              {error ?? ''}
            </Banner>
          ) : null}
          {phase.k === 'approval' ? (
            <Banner tone="clay" title="Waiting for approval">
              This is waiting for approval by the agent’s administrator. Nothing has been charged.
            </Banner>
          ) : null}
          {phase.k === 'held' ? (
            <>
              <Banner tone="clay" title="Held for review">
                Large sales are checked by MAX before payment. Keep this open, or come back from Market → Offers: once it’s approved you’ll have 15 minutes to pay. Nothing has been charged.
              </Banner>
              <View style={{ flexDirection: 'row', gap: 8, alignItems: 'center', justifyContent: 'center' }}>
                <ActivityIndicator size="small" color={colors.muted} />
                <AppText variant="small">Checking every few seconds…</AppText>
              </View>
            </>
          ) : null}

          {view && (phase.k === 'review' || phase.k === 'working') ? <Quote view={view} target={t} nativeSymbol={nativeSymbol} payerLabel={payer?.label} /> : null}
          {view && phase.k === 'uncertain' ? <Countdown view={view} /> : null}

          {phase.k === 'uncertain' ? (
            <View style={{ gap: 12 }}>
              <Banner tone="clay" title="Your payment may already have been sent">
                {`${error ? `${error} ` : ''}Don’t pay again until you’ve checked.`}
              </Banner>
              {phase.lookup === 'checking' ? (
                <View style={{ flexDirection: 'row', gap: 8, alignItems: 'center' }}>
                  <ActivityIndicator size="small" color={colors.muted} />
                  <AppText variant="small" style={{ flex: 1 }}>
                    Looking for this payment in {shortAddress(t.fromAddress)}’s recent transactions…
                  </AppText>
                </View>
              ) : (
                <AppText size={14} style={{ lineHeight: 20 }}>
                  {phase.lookup === 'failed'
                    ? 'We couldn’t reach the network to check. Check your wallet’s activity on a block explorer before doing anything else.'
                    : `No payment for this sale shows up yet among ${shortAddress(t.fromAddress)}’s last 20 transactions. It can take a minute to appear: check again shortly.`}
                </AppText>
              )}
            </View>
          ) : null}

          {phase.k === 'record_failed' ? (
            <View style={{ gap: 12 }}>
              <Banner tone="clay" title="Sent, but MAX hasn’t recorded it yet">
                Don’t pay again. Retrying only links this transaction to the sale so it can be matched.
              </Banner>
              <View style={{ flexDirection: 'row', alignItems: 'flex-start', gap: 6, padding: 12, borderRadius: 14, backgroundColor: colors.ivory, borderWidth: 1, borderColor: colors.line }}>
                <View style={{ flex: 1, minWidth: 0 }}>
                  <Mono size={13} selectable>
                    {phase.txHash}
                  </Mono>
                </View>
                <CopyButton value={phase.txHash} label="Copy transaction signature" what="Signature copied" />
              </View>
              {phase.error ? <Banner tone="danger">{phase.error}</Banner> : null}
            </View>
          ) : null}

          {error && (phase.k === 'review' || phase.k === 'working') ? <Banner tone="danger" title="Not sent">{error}</Banner> : null}
          {trusted.error && phase.k === 'review' ? <Banner tone="danger">{trusted.error}</Banner> : null}
        </ScrollView>

        <Footer
          phase={phase}
          target={t}
          checked={checked}
          setChecked={setChecked}
          trustedError={!!trusted.error}
          onStart={() => void start()}
          onConfirm={(v, replace) => void confirm(v, replace)}
          onCheckAgain={(v) => void lookForPayment(v)}
          onResend={(v) => void sendSigned(v, true)}
          onRecord={(v, h) => void record(v, h)}
          onClose={close}
        />
        <AppText variant="caption" style={{ textAlign: 'center' }}>
          MAX never holds funds. The payment goes straight to the seller and MAX’s fee.
        </AppText>
      </View>
      {/* This screen is a native modal: the root's step-up host sits underneath it. */}
      <StepUpModal />
    </View>
  );
}

/** Where a fetched sale belongs (no signature of ours pending). */
function phaseFor(view: SaleView): Phase {
  const s = view.sale;
  if (s.txHash) return { k: 'watching', saleId: s.id };
  if (s.status === 'pending_review') return { k: 'held', saleId: s.id };
  if (s.status === 'locked' && view.instructions?.solana?.length) return { k: 'review', view };
  return { k: 'watching', saleId: s.id };
}

function lockPassed(view: SaleView): boolean {
  const at = view.sale.lockExpiresAt;
  return !!at && new Date(at).getTime() <= Date.now();
}

function Footer({
  phase,
  target: t,
  checked,
  setChecked,
  trustedError,
  onStart,
  onConfirm,
  onCheckAgain,
  onResend,
  onRecord,
  onClose,
}: {
  phase: Phase;
  target: SaleTarget;
  checked: boolean;
  setChecked: (v: boolean) => void;
  trustedError: boolean;
  onStart: () => void;
  onConfirm: (v: SaleView, replace?: boolean) => void;
  onCheckAgain: (v: SaleView) => void;
  onResend: (v: SaleView) => void;
  onRecord: (v: SaleView, txHash: string) => void;
  onClose: () => void;
}) {
  const bio = useSession((s) => (s.biometricEnabled ? s.biometricLabel : null));
  // Re-render each second so the buttons disable when the lock passes.
  const view = phase.k === 'review' || phase.k === 'working' || phase.k === 'uncertain' ? phase.view : null;
  const { secondsLeft } = useCountdown(view?.sale.lockExpiresAt);
  const expired = !!view?.sale.lockExpiresAt && secondsLeft <= 0;

  switch (phase.k) {
    case 'start_failed':
      return <Button label="Try again" onPress={onStart} />;
    case 'approval':
    case 'held':
      return <Button label="Close" variant="secondary" onPress={onClose} />;
    case 'review':
    case 'working': {
      const w = phase.k === 'working';
      return (
        <Button
          label={w ? STEP[phase.step] : expired ? 'Payment window closed' : `Pay ${displayDecimal(t.price)} ${t.symbol}`}
          icon={w ? undefined : bio ? 'faceId' : 'lock'}
          loading={w}
          disabled={expired || trustedError}
          accessibilityHint={`Confirms with ${bio ?? 'your passcode'}, then signs on this phone`}
          onPress={() => onConfirm(phase.view)}
        />
      );
    }
    case 'uncertain': {
      if (phase.lookup === 'checking') return <Button label="Checking…" loading />;
      return (
        <View style={{ gap: 10 }}>
          <Button label="Check again" variant="secondary" onPress={() => onCheckAgain(phase.view)} />
          <Button label="Send the same signed payment again" variant="ghost" size="md" disabled={expired} onPress={() => onResend(phase.view)} />
          <CheckRow checked={checked} onChange={setChecked}>
            I checked my wallet and this payment isn’t there. Paying again could otherwise charge me twice.
          </CheckRow>
          <Button label="Sign a new payment" variant="danger" disabled={!checked || expired} onPress={() => onConfirm(phase.view, true)} />
        </View>
      );
    }
    case 'record_failed':
      return <Button label="Try recording again" onPress={() => onRecord(phase.view, phase.txHash)} />;
    default:
      return null;
  }
}

function Countdown({ view }: { view: SaleView }) {
  const { label, secondsLeft } = useCountdown(view.sale.lockExpiresAt);
  if (!view.sale.lockExpiresAt) return null;
  if (secondsLeft <= 0) {
    return (
      <Banner tone="danger" title="The payment window closed">
        The name was released. If you didn’t pay, nothing was charged; a payment already sent is still matched by MAX.
      </Banner>
    );
  }
  return (
    <View accessibilityLiveRegion="polite" style={{ flexDirection: 'row', alignItems: 'center', gap: 8, alignSelf: 'flex-start', paddingHorizontal: 12, paddingVertical: 8, borderRadius: 10, backgroundColor: colors.claySoft }}>
      <Icon name="lock" size={16} color={colors.clayText} />
      <AppText weight="semibold" size={14} color={colors.clayText}>
        Reserved for you · {label} left to pay
      </AppText>
    </View>
  );
}

function Quote({ view, target: t, nativeSymbol, payerLabel }: { view: SaleView; target: SaleTarget; nativeSymbol: string; payerLabel?: string }) {
  const s = view.sale;
  const q = view.quote;
  const sym = t.symbol;
  return (
    <>
      <Countdown view={view} />
      <View>
        <ReviewRow label="Price">
          <AppText weight="semibold" size={14}>
            {displayDecimal(t.price)} {sym}
          </AppText>
        </ReviewRow>
        <ReviewRow label="Network fee">{q ? `≈ ${q.networkFee} ${nativeSymbol}` : '—'}</ReviewRow>
        <ReviewRow label="Total you pay">
          <AppText weight="semibold" size={14}>
            {q ? `${displayDecimal(t.price)} ${sym} + ${q.totalNative} ${nativeSymbol}` : `${displayDecimal(t.price)} ${sym}`}
          </AppText>
        </ReviewRow>
        <ReviewRow label="Seller receives">{`${displayDecimal(q?.sellerReceives ?? s.sellerReceives)} ${sym}`}</ReviewRow>
        <ReviewRow label={`MAX fee (${s.feeBps / 100}%, from the seller)`}>{`${displayDecimal(q?.fee ?? s.fee)} ${sym}`}</ReviewRow>
        <ReviewRow label="Network">{s.chain}</ReviewRow>
        <ReviewRow label="Paid from" last>
          <AppText size={14}>
            {payerLabel ?? 'Account'} · <Mono size={14}>{shortAddress(t.fromAddress)}</Mono>
          </AppText>
        </ReviewRow>
      </View>
      <AppText variant="small">The transaction is checked against this price and token before it’s signed on this phone.</AppText>
    </>
  );
}

const OUTCOME: Record<string, { title: string; body: string; tone: 'ok' | 'wait' | 'bad' }> = {
  locked: { title: 'Waiting for the payment', body: 'MAX hasn’t seen the transaction yet.', tone: 'wait' },
  submitted: { title: 'Payment sent', body: 'Waiting for the network to confirm it. You can leave: the sale finishes on its own.', tone: 'wait' },
  confirming: { title: 'Confirming', body: 'The network is confirming the payment. You can leave: the sale finishes on its own.', tone: 'wait' },
  completed: { title: 'It’s yours', body: 'The name is in your IDs. Its wallets and profile were reset: pick where it receives.', tone: 'ok' },
  expired: { title: 'The payment window closed', body: 'If you already sent the payment, MAX will still match it. Otherwise nothing was charged.', tone: 'bad' },
  cancelled: { title: 'This sale was cancelled', body: 'The name was released.', tone: 'bad' },
  manual_review: { title: 'This sale needs a manual check', body: 'Your payment arrived but the sale couldn’t complete automatically. MAX will contact you about the outcome.', tone: 'wait' },
  reversed: { title: 'This sale was reversed', body: 'The name went back to the seller.', tone: 'bad' },
  pending_review: { title: 'Held for review', body: 'MAX checks large sales before payment. Nothing has been charged.', tone: 'wait' },
};

function SaleOutcome({ saleId, live, target: t, explorerTxUrl, onDone }: { saleId: string; live: SaleView | null; target: SaleTarget; explorerTxUrl?: string; onDone: () => void }) {
  const insets = useSafeAreaInsets();
  const s = live?.sale;
  const terminal = !!s && SALE_TERMINAL.includes(s.status);
  const c = s ? (OUTCOME[s.status] ?? { title: 'In progress', body: '', tone: 'wait' as const }) : { title: 'Loading the sale…', body: '', tone: 'wait' as const };
  const body = s && (s.status === 'cancelled' || s.status === 'reversed') && s.statusReason ? s.statusReason : c.body;
  const ok = c.tone === 'ok';
  const bad = c.tone === 'bad';
  return (
    <View accessibilityRole="summary" style={{ flex: 1, backgroundColor: colors.ivory, paddingTop: insets.top + 80, paddingHorizontal: 24, paddingBottom: insets.bottom + 16, gap: 20, alignItems: 'center' }}>
      <View style={{ width: 72, height: 72, borderRadius: 36, backgroundColor: bad ? colors.dangerSoft : ok ? colors.successSoft : colors.sunken, alignItems: 'center', justifyContent: 'center' }}>
        {ok || bad ? <Icon name={bad ? 'alert' : 'check'} size={34} color={bad ? colors.danger : colors.success} strokeWidth={2.5} /> : <ActivityIndicator color={colors.muted} />}
      </View>
      <AppText variant="h1" accessibilityRole="header" style={{ fontSize: 32, textAlign: 'center' }}>
        {c.title}
      </AppText>
      <AppText size={16} color={colors.muted} style={{ textAlign: 'center' }}>
        <Handle name={t.handle} size={16} accent={colors.clayText} /> for {displayDecimal(t.price)} {t.symbol}
      </AppText>
      {s ? (
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
          <SaleStatusPill status={s.status} />
          {!terminal ? <ActivityIndicator size="small" color={colors.muted} /> : null}
        </View>
      ) : null}
      {body ? (
        <AppText variant="small" accessibilityLiveRegion="polite" style={{ textAlign: 'center' }}>
          {s?.status === 'confirming' ? `${s.confirmations} confirmation${s.confirmations === 1 ? '' : 's'} so far. ` : ''}
          {body}
        </AppText>
      ) : null}
      {s?.txHash ? (
        <View style={{ width: '100%', gap: 6 }}>
          <AppText weight="semibold" size={12} color={colors.muted}>
            Transaction
          </AppText>
          <View style={{ paddingVertical: 12, paddingHorizontal: 14, borderRadius: 12, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line }}>
            <Mono size={13} selectable>
              {s.txHash}
            </Mono>
          </View>
        </View>
      ) : null}
      {s?.txHash && explorerTxUrl ? <LinkButton label="View on explorer ↗" weight="semibold" color={colors.clayText} onPress={() => Linking.openURL(`${explorerTxUrl}${s.txHash}`)} /> : null}
      <View style={{ marginTop: 'auto', width: '100%', gap: 10 }}>
        {s?.status === 'completed' ? (
          <Button
            label="Go to my IDs"
            onPress={() => {
              onDone();
              router.navigate('/ids');
            }}
          />
        ) : null}
        <Button label="Done" variant={s?.status === 'completed' ? 'secondary' : 'dark'} onPress={onDone} />
      </View>
      <AppText variant="caption" style={{ textAlign: 'center' }}>
        Sale {saleId.slice(0, 8)}
      </AppText>
    </View>
  );
}
