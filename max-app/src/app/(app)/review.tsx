import { TERMINAL_STATUSES, type Payment } from '@max/sdk';
import { useQueryClient } from '@tanstack/react-query';
import * as Haptics from 'expo-haptics';
import { Redirect, router, useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, Linking, Pressable, ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { StepUpModal } from '@/components/auth/StepUpModal';
import { SaleReview } from '@/components/market/SaleReview';
import { StatusPill } from '@/components/payments/PaymentRow';
import { AppText, Handle, Mono } from '@/components/ui/AppText';
import { Banner, ReviewRow } from '@/components/ui/Blocks';
import { Button, IconButton, LinkButton } from '@/components/ui/Button';
import { Icon } from '@/components/ui/Icon';
import { ChainLogo } from '@/components/ui/ChainLogo';
import { max } from '@/api/client';
import { keys } from '@/api/keys';
import { errorCode, errorMessage } from '@/error/messages';
import { useAccountId, usePayment } from '@/hooks/queries';
import { broadcast, ChainError, estimateFee, signPayment } from '@/services/chain';
import * as pending from '@/services/pending';
import { stepUp } from '@/services/session';
import { useSendDraft, type SignedTx } from '@/store/flows';
import { getGeneration, useSession } from '@/store/session';
import { colors, fonts, spacing } from '@/theme';
import { formatAmount } from '@/utils/amount';
import { shortAddress } from '@/utils/format';

type Phase =
  | { kind: 'preparing' }
  | { kind: 'prepare_failed'; error: string; stale: boolean }
  | { kind: 'review' }
  | { kind: 'working'; step: 'sign' | 'broadcast' | 'record' }
  | { kind: 'failed'; error: string }
  | { kind: 'record_failed'; error: string; ambiguous: boolean }
  | { kind: 'sent'; ambiguous: boolean };

/**
 * The review sheet. `?flow=sale` is the marketplace variant (buy a listing,
 * pay an accepted offer); its terms live in useSaleDraft, never in params.
 */
export default function ReviewRoute() {
  const { flow } = useLocalSearchParams<{ flow?: string }>();
  return flow === 'sale' ? <SaleReview /> : <Review />;
}

/**
 * Review sheet → sign on device → broadcast → record with MAX. Signing happens
 * at most once per payment: the signed bytes are kept, so every retry
 * re-broadcasts the same transaction and re-submits the same hash.
 */
function Review() {
  const insets = useSafeAreaInsets();
  const qc = useQueryClient();
  const accountId = useAccountId();
  const terms = useSendDraft((s) => s.terms);
  const payment = useSendDraft((s) => s.payment);
  const signed = useSendDraft((s) => s.signed);
  const accounts = useSession((s) => s.accounts);
  const [phase, setPhase] = useState<Phase>(() => (useSendDraft.getState().signed ? { kind: 'record_failed', error: '', ambiguous: true } : { kind: 'preparing' }));
  const [fee, setFee] = useState<string | null | undefined>(undefined);

  const prepare = useCallback(async () => {
    const t = useSendDraft.getState().terms;
    if (!t) return;
    const gen = getGeneration();
    try {
      const draft = useSendDraft.getState();
      // Fresh resolution every time: the sender signs for the wallet they see now.
      // A converted payment resolves on the chain the recipient receives on.
      const r = t.receiveChain
        ? await max().resolve(t.handle, { chain: t.receiveChain })
        : await max().resolve(t.handle, { chain: t.chain, asset: t.asset === 'native' ? undefined : t.asset });
      const route = t.receiveChain ? { destinationChain: t.receiveChain, destinationAsset: 'native' } : {};
      const fingerprint = JSON.stringify([r.maxId, r.resolutionVersion, t.chain, t.asset, t.amount, t.senderAddress, t.receiveChain ?? '']);
      let p = await max().transfers.prepare({
        recipient: t.handle,
        chain: t.chain,
        asset: t.asset,
        amount: t.amount,
        senderAddress: t.senderAddress,
        resolutionVersion: r.resolutionVersion,
        idempotencyKey: draft.keyFor(fingerprint),
        ...route,
      });
      if (p.status === 'EXPIRED' && !useSendDraft.getState().signed) {
        // Same terms, but the old intent lapsed: start a new one.
        useSendDraft.setState({ fingerprint: null });
        p = await max().transfers.prepare({
          recipient: t.handle,
          chain: t.chain,
          asset: t.asset,
          amount: t.amount,
          senderAddress: t.senderAddress,
          resolutionVersion: r.resolutionVersion,
          idempotencyKey: useSendDraft.getState().keyFor(fingerprint),
          ...route,
        });
      }
      if (gen !== getGeneration()) return;
      if (p.recipient.address !== r.address) throw new Error('The recipient’s address changed while preparing. Review again.');
      useSendDraft.getState().setPayment(p);
      setPhase({ kind: 'review' });
      setFee(await estimateFee(p));
    } catch (err) {
      if (gen !== getGeneration()) return;
      setPhase({ kind: 'prepare_failed', error: errorMessage(err), stale: errorCode(err) === 'RESOLUTION_STALE' });
    }
  }, []);

  const retryPrepare = () => {
    setPhase({ kind: 'preparing' });
    setFee(undefined);
    void prepare();
  };

  // Prepare once on open (deferred so StrictMode's mount/unmount/mount runs it once).
  useEffect(() => {
    if (useSendDraft.getState().signed) return;
    const t = setTimeout(() => void prepare(), 0);
    return () => clearTimeout(t);
  }, [prepare]);

  // `s` is passed in, not re-read: a lock mid-send clears the draft, and the hash must still reach MAX.
  const record = useCallback(
    async (p: Payment, s: SignedTx, ambiguous: boolean) => {
      setPhase({ kind: 'working', step: 'record' });
      try {
        const updated = await max().transfers.submit(p.id, s.hash);
        pending.forgetQuietly(p.id);
        if (useSendDraft.getState().payment?.id === p.id) useSendDraft.getState().setPayment(updated);
        qc.invalidateQueries({ queryKey: keys.account(accountId) });
        Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success).catch(() => {});
        setPhase({ kind: 'sent', ambiguous });
      } catch (err) {
        setPhase({ kind: 'record_failed', error: errorMessage(err), ambiguous });
      }
    },
    [qc, accountId],
  );

  const sendSigned = useCallback(
    async (p: Payment) => {
      const s = useSendDraft.getState().signed;
      if (!s) return;
      setPhase({ kind: 'working', step: 'broadcast' });
      try {
        // Saved before it can go out, so a lock or crash from here on never loses the hash.
        await pending.remember('send', p.id, p.chain.key, s);
      } catch (err) {
        setPhase({ kind: 'failed', error: errorMessage(err) });
        return;
      }
      let ambiguous = false;
      try {
        await broadcast(p.chain, s);
      } catch (err) {
        if (err instanceof ChainError && err.ambiguous) {
          ambiguous = true; // may be on its way: record it, never sign again
        } else {
          // Definitely not sent: drop the signature so the next attempt is clean.
          pending.forgetQuietly(p.id);
          useSendDraft.getState().setSigned(null);
          setPhase({ kind: 'failed', error: errorMessage(err) });
          return;
        }
      }
      await record(p, s, ambiguous);
    },
    [record],
  );

  async function confirm() {
    const p = useSendDraft.getState().payment;
    const t = useSendDraft.getState().terms;
    if (!p || !t) return;
    const ok = await stepUp(`Approve sending ${t.amount} ${symbolOf(p)} to ${p.recipient.handle}.`);
    if (!ok) return;
    try {
      if (!useSendDraft.getState().signed) {
        setPhase({ kind: 'working', step: 'sign' });
        // A transaction already signed for this payment is reused; an earlier unrecorded one blocks a new signature.
        useSendDraft.getState().setSigned((await pending.beforeSigning('send', p.id)) ?? (await signPayment(p)));
      }
    } catch (err) {
      setPhase({ kind: 'failed', error: errorMessage(err) });
      return;
    }
    await sendSigned(p);
  }

  const close = () => {
    useSendDraft.getState().reset();
    router.back();
  };
  const finish = () => {
    useSendDraft.getState().reset();
    router.dismissAll();
    router.navigate('/home');
  };

  if (!terms) return <Redirect href="/send" />;

  if (phase.kind === 'sent' && payment) return <Sent payment={payment} ambiguous={phase.ambiguous} onDone={finish} />;

  const working = phase.kind === 'working';
  const sender = accounts.find((a) => a.evm?.toLowerCase() === terms.senderAddress.toLowerCase() || a.solana === terms.senderAddress);

  return (
    <View style={{ flex: 1, backgroundColor: colors.scrim }}>
      <Pressable style={{ flex: 1, minHeight: 80 }} accessibilityLabel="Close" onPress={working || signed ? undefined : close} />
      <View accessibilityViewIsModal style={{ backgroundColor: colors.surface, borderTopLeftRadius: 28, borderTopRightRadius: 28, paddingTop: 12, paddingHorizontal: spacing.gutter, paddingBottom: insets.bottom + 16, gap: 18, maxHeight: '92%' }}>
        <View style={{ alignSelf: 'center', width: 40, height: 5, borderRadius: 999, backgroundColor: colors.line }} />
        <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginRight: -10 }}>
          <AppText variant="title" accessibilityRole="header">
            Review payment
          </AppText>
          {working || signed ? <View style={{ width: 44 }} /> : <IconButton icon="close" label="Close" color={colors.muted} onPress={close} />}
        </View>

        <ScrollView contentContainerStyle={{ gap: 18 }} bounces={false}>
          <View style={{ alignItems: 'center', gap: 4, paddingTop: 4, paddingBottom: 8 }}>
            <AppText style={{ fontFamily: fonts.display, fontSize: 40 }} numberOfLines={1} adjustsFontSizeToFit>
              {terms.amount} <AppText style={{ fontFamily: fonts.display, fontSize: 22, color: colors.muted }}>{payment ? symbolOf(payment) : ''}</AppText>
            </AppText>
            <AppText size={14} color={colors.muted}>
              to <Handle name={terms.handle} size={14} accent={colors.clayText} />
            </AppText>
          </View>

          {phase.kind === 'preparing' ? (
            <View style={{ padding: 24, alignItems: 'center', gap: 10 }}>
              <ActivityIndicator color={colors.muted} />
              <AppText variant="small">Locking in the recipient’s current wallet…</AppText>
            </View>
          ) : phase.kind === 'prepare_failed' ? (
            <View style={{ gap: 12 }}>
              <Banner tone="danger" title={phase.stale ? 'Their wallet changed' : 'Couldn’t prepare this payment'}>
                {phase.error}
              </Banner>
              <Button label={phase.stale ? 'Review the new address' : 'Try again'} variant="secondary" onPress={retryPrepare} />
            </View>
          ) : payment ? (
            <>
              <View>
                <ReviewRow label="Network">
                  <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
                    <ChainLogo chain={payment.chain.key} size={16} />
                    <AppText weight="semibold" size={14}>
                      {payment.chain.name}
                    </AppText>
                  </View>
                </ReviewRow>
                <ReviewRow label="Asset">
                  <AppText weight="semibold" size={14}>
                    {payment.asset.kind === 'native' ? `${payment.chain.nativeCoin} (native)` : `${symbolOf(payment)} · ${shortAddress(payment.asset.address ?? '')}`}
                  </AppText>
                </ReviewRow>
                {payment.crossChain ? (
                  <>
                    <ReviewRow label="They receive about">
                      <AppText weight="semibold" size={14}>
                        {payment.crossChain.expectedAmountOut.display} {payment.crossChain.destinationAsset.symbol ?? payment.crossChain.destinationChain.nativeCoin} on {payment.crossChain.destinationChain.name}
                      </AppText>
                    </ReviewRow>
                    <ReviewRow label="At least">
                      <AppText size={14}>
                        {payment.crossChain.minAmountOut.display} {payment.crossChain.destinationAsset.symbol ?? payment.crossChain.destinationChain.nativeCoin}
                      </AppText>
                    </ReviewRow>
                  </>
                ) : null}
                <ReviewRow label="From">
                  <AppText size={14}>
                    {sender?.label ?? 'Account'} · <Mono size={14}>{shortAddress(terms.senderAddress)}</Mono>
                  </AppText>
                </ReviewRow>
                <ReviewRow label="Network fee">{fee === undefined ? '…' : fee === null ? 'Unavailable' : `≈ ${formatAmount(fee, 6)} ${payment.chain.nativeCoin}`}</ReviewRow>
                <ReviewRow label="Resolution" last>
                  <Mono size={14}>v{payment.recipient.resolutionVersion}</Mono>
                </ReviewRow>
              </View>
              {payment.warnings?.map((w) => (
                <Banner key={w} tone="clay">
                  {w}
                </Banner>
              ))}
            </>
          ) : null}

          {phase.kind === 'failed' ? <Banner tone="danger" title="Not sent">{phase.error}</Banner> : null}
          {phase.kind === 'record_failed' ? (
            <Banner tone="clay" title={phase.ambiguous ? 'Checking the network…' : 'Sent, but not recorded yet'}>
              {`${phase.ambiguous ? 'We may have lost the connection after sending.' : 'The network has it.'} Retrying won’t send it twice: it reuses the same signed transaction.${phase.error ? ` (${phase.error})` : ''}`}
            </Banner>
          ) : null}
        </ScrollView>

        {phase.kind === 'record_failed' && payment ? (
          <Button label="Retry" onPress={() => void sendSigned(payment)} />
        ) : (
          <Button
            label={working ? (phase.step === 'sign' ? 'Signing…' : phase.step === 'broadcast' ? 'Sending…' : 'Recording…') : `Confirm with ${useSession.getState().biometricEnabled ? useSession.getState().biometricLabel : 'passcode'}`}
            icon={useSession.getState().biometricEnabled ? 'faceId' : 'lock'}
            loading={working}
            disabled={phase.kind !== 'review' && phase.kind !== 'failed'}
            onPress={phase.kind === 'failed' && !payment ? retryPrepare : confirm}
          />
        )}
        <AppText variant="caption" style={{ textAlign: 'center' }}>
          Check the address. Crypto transfers can’t be reversed.
        </AppText>
      </View>
      {/* This screen is a native modal: the root's step-up host sits underneath it. */}
      <StepUpModal />
    </View>
  );
}

function symbolOf(p: Payment): string {
  return p.asset.symbol ?? (p.asset.kind === 'native' ? p.chain.nativeCoin : 'tokens');
}

function Sent({ payment: initial, ambiguous, onDone }: { payment: Payment; ambiguous: boolean; onDone: () => void }) {
  const insets = useSafeAreaInsets();
  const live = usePayment(initial.id);
  const p = live.data ?? initial;
  const terminal = TERMINAL_STATUSES.includes(p.status) || p.status === 'EXPIRED';
  const failed = p.status === 'FAILED' || p.status === 'EXPIRED';
  const explorer = p.explorerUrl ?? (p.txHash ? `${p.chain.explorerTxUrl}${p.txHash}` : null);

  return (
    <View accessibilityRole="summary" style={{ flex: 1, backgroundColor: colors.ivory, paddingTop: insets.top + 80, paddingHorizontal: 24, paddingBottom: insets.bottom + 16, gap: 20, alignItems: 'center' }}>
      <View style={{ width: 72, height: 72, borderRadius: 36, backgroundColor: failed ? colors.dangerSoft : colors.successSoft, alignItems: 'center', justifyContent: 'center' }}>
        <Icon name={failed ? 'alert' : 'check'} size={34} color={failed ? colors.danger : colors.success} strokeWidth={2.5} />
      </View>
      <AppText variant="h1" accessibilityRole="header" style={{ fontSize: 34 }}>
        {failed ? 'Didn’t go through' : p.status === 'COMPLETED' ? 'Sent' : ambiguous ? 'Checking…' : 'Sent'}
      </AppText>
      <AppText size={16} color={colors.muted} style={{ textAlign: 'center' }}>
        {p.amount.display} {symbolOf(p)} to <Handle name={p.recipient.handle} size={16} accent={colors.clayText} /> on {p.chain.name}
      </AppText>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
        <StatusPill status={p.status} />
        {!terminal ? <ActivityIndicator size="small" color={colors.muted} /> : null}
      </View>
      {ambiguous && !terminal ? (
        <AppText variant="small" style={{ textAlign: 'center' }}>
          The connection dropped while sending. MAX is checking the network for it, so it won’t be sent twice.
        </AppText>
      ) : null}
      {p.txHash ? (
        <View style={{ width: '100%', gap: 6 }}>
          <AppText weight="semibold" size={12} color={colors.muted}>
            Transaction
          </AppText>
          <View style={{ paddingVertical: 12, paddingHorizontal: 14, borderRadius: 12, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line }}>
            <Mono size={13} selectable>
              {p.txHash}
            </Mono>
          </View>
        </View>
      ) : null}
      {explorer ? <LinkButton label="View on explorer ↗" weight="semibold" color={colors.clayText} onPress={() => Linking.openURL(explorer)} /> : null}
      <View style={{ marginTop: 'auto', width: '100%' }}>
        <Button label="Done" variant="dark" onPress={onDone} />
      </View>
    </View>
  );
}
