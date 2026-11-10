import { normalizeName, type FeeOption, type MaxId, type RenameEligibility, type RenameView } from '@max/sdk';
import { useQueryClient } from '@tanstack/react-query';
import * as Haptics from 'expo-haptics';
import { router, useLocalSearchParams } from 'expo-router';
import { useEffect, useState } from 'react';
import { ActivityIndicator, Keyboard, Linking, View } from 'react-native';

import { StatusPill } from '@/components/payments/PaymentRow';
import { AppText, Handle, Mono } from '@/components/ui/AppText';
import { Banner, Card, Choice, Loading, ReviewRow } from '@/components/ui/Blocks';
import { Button, LinkButton } from '@/components/ui/Button';
import { HandleField } from '@/components/ui/Fields';
import { Icon } from '@/components/ui/Icon';
import { Screen, TitleHeader } from '@/components/ui/Screen';
import { ChainLogo } from '@/components/ui/ChainLogo';
import { max } from '@/api/client';
import { keys } from '@/api/keys';
import { errorMessage } from '@/error/messages';
import { useAccountId, useAvailability, useMyIds, usePayment, useRenameEligibility, useRenameRequest } from '@/hooks/queries';
import { useDebounced } from '@/hooks/useDebounced';
import type { AccountInfo } from '@/services/vault';
import { broadcast, ChainError, hasRpc, signPayment } from '@/services/chain';
import { stepUp } from '@/services/session';
import { useRenameDraft } from '@/store/flows';
import { getGeneration, useSession } from '@/store/session';
import { colors } from '@/theme';
import { formatDate, shortAddress } from '@/utils/format';
import { stripPrefix } from '@/validation/identity';

type Phase =
  | { kind: 'form' }
  | { kind: 'working'; step: 'reserve' | 'sign' | 'broadcast' | 'record' }
  | { kind: 'failed'; error: string }
  | { kind: 'record_failed'; view: RenameView; error: string; ambiguous: boolean };

/**
 * Paid rename: reserve the new name → sign the fee on this phone → MAX applies
 * the rename once the fee confirms. The fee is signed at most once: the signed
 * bytes are kept, so every retry re-broadcasts the same transaction.
 */
export default function Rename() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const ids = useMyIds();
  const maxId = ids.data?.find((x) => x.id === id);
  const elig = useRenameEligibility(id ?? '');
  const close = () => (router.canGoBack() ? router.back() : router.replace('/ids'));

  let body;
  // Never start from a cached eligibility: a stale `pending` would open the wrong rename.
  if (ids.isLoading || !elig.isFetchedAfterMount) body = <Loading label="Checking this handle" />;
  else if (!maxId || elig.error || !elig.data) body = <Banner tone="danger">{elig.error ? errorMessage(elig.error) : 'That MAX ID isn’t yours.'}</Banner>;
  if (body || !maxId || !elig.data) {
    return (
      <Screen topPadding={12} header={<TitleHeader title="Rename" onBack={close} />}>
        {body}
      </Screen>
    );
  }
  return <RenameFlow maxId={maxId} eligibility={elig.data} onClose={close} />;
}

function RenameFlow({ maxId, eligibility: e, onClose }: { maxId: MaxId; eligibility: RenameEligibility; onClose: () => void }) {
  const qc = useQueryClient();
  const accountId = useAccountId();
  const accounts = useSession((s) => s.accounts);
  const [q, setQ] = useState('');
  // Set once a rename is reserved: from then on the progress screen owns it.
  const [progressId, setProgressId] = useState<string | null>(e.pending?.id ?? null);
  const [phase, setPhase] = useState<Phase>(() => {
    const d = useRenameDraft.getState();
    // Came back to a fee that was signed but never recorded: offer the retry, never a new signature.
    if (d.view && d.signed && d.view.request.id === e.pending?.id) return { kind: 'record_failed', view: d.view, error: '', ambiguous: true };
    return { kind: 'form' };
  });

  // Only fees this phone can pay: a chain with an RPC and an account on it.
  const fees = e.fees.filter((f) => hasRpc(f.chain.key) && !!payerFor(f, maxId, accounts));
  const [fee, setFee] = useState<FeeOption | null>(fees[0] ?? null);
  const payer = fee ? payerFor(fee, maxId, accounts) : null;

  const debounced = useDebounced(q);
  const name = normalizeName(debounced);
  const avail = useAvailability(debounced);
  const settled = q === debounced;
  const same = name === maxId.name;
  const nameOk = settled && !!name && !same && !!avail.data?.available;
  const working = phase.kind === 'working';

  let tone: 'muted' | 'success' | 'danger' = 'muted';
  let message = '3–32 lowercase letters, numbers or hyphens.';
  if (!q.trim()) {
    // idle
  } else if (!settled || (name && !same && avail.isFetching && !avail.data)) {
    message = 'Checking…';
  } else if (!name) {
    tone = 'danger';
    message = 'Use 3–32 lowercase letters, numbers or hyphens.';
  } else if (same) {
    tone = 'danger';
    message = 'That’s already this handle’s name.';
  } else if (avail.error) {
    tone = 'danger';
    message = errorMessage(avail.error);
  } else if (avail.data) {
    tone = avail.data.available ? 'success' : 'danger';
    message = avail.data.available ? `${avail.data.handle} is available.` : `${avail.data.handle} isn’t available.`;
  }

  async function record(view: RenameView, ambiguous: boolean) {
    const s = useRenameDraft.getState().signed;
    if (!s) return;
    setPhase({ kind: 'working', step: 'record' });
    try {
      await max().transfers.submit(view.feePayment.id, s.hash);
      qc.invalidateQueries({ queryKey: keys.rename(accountId, maxId.id) });
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success).catch(() => {});
      setPhase({ kind: 'form' });
      setProgressId(view.request.id);
    } catch (err) {
      setPhase({ kind: 'record_failed', view, error: errorMessage(err), ambiguous });
    }
  }

  async function sendSigned(view: RenameView) {
    const s = useRenameDraft.getState().signed;
    if (!s) return;
    setPhase({ kind: 'working', step: 'broadcast' });
    let ambiguous = false;
    try {
      await broadcast(view.feePayment.chain, s);
    } catch (err) {
      if (err instanceof ChainError && err.ambiguous) {
        ambiguous = true; // may be on its way: record it, never sign again
      } else {
        // Definitely not sent: drop the signature so the next attempt is clean.
        useRenameDraft.getState().setSigned(null);
        setPhase({ kind: 'failed', error: errorMessage(err) });
        return;
      }
    }
    await record(view, ambiguous);
  }

  /** Signs (once) and sends the fee for a reserved rename. */
  async function payFee(view: RenameView) {
    const draft = useRenameDraft.getState();
    if (!draft.signed) {
      setPhase({ kind: 'working', step: 'sign' });
      // Older servers put the instructions on the view rather than the payment.
      const payment = { ...view.feePayment, instructions: view.feePayment.instructions ?? view.instructions };
      draft.setSigned(await signPayment(payment));
    }
    await sendSigned(view);
  }

  async function pay() {
    if (!fee || !name || !payer) return;
    Keyboard.dismiss(); // the passcode keypad sits where the keyboard would
    const ok = await stepUp(`Approve paying ${fee.amount} ${fee.symbol} to rename ${maxId.handle} to max.${name}.`);
    if (!ok) return;
    const gen = getGeneration();
    try {
      setPhase({ kind: 'working', step: 'reserve' });
      const draft = useRenameDraft.getState();
      const key = draft.keyFor(JSON.stringify([maxId.id, name, fee.chain.key, fee.asset, payer.address]));
      let view = useRenameDraft.getState().view;
      if (!view) {
        view = await max().renames.request(maxId.id, { newName: name, feeChain: fee.chain.key, feeAsset: fee.asset, senderAddress: payer.address, idempotencyKey: key });
        if (gen !== getGeneration()) return;
        useRenameDraft.getState().setView(view);
      }
      if (view.feePayment.txHash) {
        setPhase({ kind: 'form' });
        setProgressId(view.request.id);
        return;
      }
      if (view.feePayment.senderAddress.toLowerCase() !== payer.address.toLowerCase()) throw new Error('The fee payment names a different sender. Try again.');
      await payFee(view);
    } catch (err) {
      setPhase({ kind: 'failed', error: errorMessage(err) });
    }
  }

  async function payPending(view: RenameView) {
    const ok = await stepUp(`Approve paying ${view.feePayment.amount.display} ${view.feePayment.asset.symbol ?? view.feePayment.chain.nativeCoin} to rename ${view.request.oldHandle}.`);
    if (!ok) return;
    try {
      const draft = useRenameDraft.getState();
      // A signature kept for a different rename must never be sent for this one.
      if (draft.view?.request.id !== view.request.id) draft.setSigned(null);
      draft.setView(view);
      await payFee(view);
    } catch (err) {
      setPhase({ kind: 'failed', error: errorMessage(err) });
    }
  }

  const finish = () => {
    useRenameDraft.getState().reset();
    qc.invalidateQueries({ queryKey: keys.account(accountId) });
    onClose();
  };

  const recordFailed = phase.kind === 'record_failed' ? phase : null;
  const status = (
    <>
      {phase.kind === 'failed' ? (
        <Banner tone="danger" title="Fee not sent">
          {phase.error}
        </Banner>
      ) : null}
      {recordFailed ? (
        <Banner tone="clay" title={recordFailed.ambiguous ? 'Checking the network…' : 'Paid, but not recorded yet'}>
          {`${recordFailed.ambiguous ? 'We may have lost the connection after sending the fee.' : 'The network has your fee.'} Retrying won’t pay twice: it reuses the same signed transaction.${recordFailed.error ? ` (${recordFailed.error})` : ''}`}
        </Banner>
      ) : null}
    </>
  );
  const workingLabel = working ? { reserve: 'Reserving name…', sign: 'Signing…', broadcast: 'Sending fee…', record: 'Recording…' }[phase.step] : null;

  if (progressId) {
    return (
      <RenameProgress
        maxId={maxId}
        requestId={progressId}
        onPay={payPending}
        onRetry={recordFailed ? () => void sendSigned(recordFailed.view) : null}
        onRestart={() => {
          useRenameDraft.getState().reset();
          qc.invalidateQueries({ queryKey: keys.rename(accountId, maxId.id) });
          setPhase({ kind: 'form' });
          setProgressId(null);
        }}
        busyLabel={workingLabel}
        status={status}
        onDone={finish}
      />
    );
  }

  const locked = working || !!recordFailed;
  const cooldownAt = new Date(e.renameAvailableAt);

  return (
    <Screen
      scroll
      gap={22}
      header={<TitleHeader title="Rename" onBack={locked ? undefined : onClose} />}
      footer={
        !e.eligible ? (
          <Button label="Back" variant="secondary" onPress={onClose} />
        ) : recordFailed ? (
          <Button label="Retry" onPress={() => void sendSigned(recordFailed.view)} />
        ) : (
          <Button
            label={
              workingLabel
                ? workingLabel
                : fee
                  ? `Pay ${fee.amount} ${fee.symbol} and rename`
                  : 'Rename'
            }
            icon={working ? undefined : useSession.getState().biometricEnabled ? 'faceId' : 'lock'}
            loading={working}
            disabled={!nameOk || !fee || !payer}
            onPress={() => void pay()}
          />
        )
      }>
      <View style={{ gap: 6 }}>
        <AppText variant="h2" accessibilityRole="header">
          Rename <Handle name={maxId.handle} size={26} />
        </AppText>
        <AppText variant="small">Your wallets, destinations and profile move to the new name. The old name is held for 30 days so nobody else can catch payments meant for you.</AppText>
      </View>
      {/* Up top so it's visible without scrolling. */}
      {status}

      {!e.eligible ? (
        <Banner tone="clay" title="Can’t rename right now">
          {e.reason?.startsWith('cooldown')
            ? `Handles can be renamed once every ${e.cooldownHours} hours. You can rename again ${formatDate(e.renameAvailableAt)} at ${cooldownAt.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })}.`
            : (e.reason ?? 'This handle can’t be renamed.')}
        </Banner>
      ) : (
        <>
          <HandleField label="New name" value={q} onChangeText={(v) => !locked && setQ(stripPrefix(v))} message={message} messageTone={tone} />

          <View style={{ gap: 8 }}>
            <AppText variant="label">Pay the fee with</AppText>
            {fees.length ? (
              <View style={{ flexDirection: 'row', gap: 8, flexWrap: 'wrap' }}>
                {fees.map((f) => (
                  <Choice
                    key={`${f.chain.key}:${f.asset}`}
                    label={`${f.amount} ${f.symbol} · ${f.chain.name}`}
                    selected={fee?.chain.key === f.chain.key && fee.asset === f.asset}
                    onPress={() => !locked && setFee(f)}
                  />
                ))}
              </View>
            ) : (
              <Banner tone="clay">{e.fees.length ? 'None of the fee networks are set up on this phone, or you have no account on them.' : 'No fee options are configured.'}</Banner>
            )}
          </View>

          {fee && payer ? (
            <Card>
              <ReviewRow label="Fee">
                <AppText weight="semibold" size={14}>
                  {fee.amount} {fee.symbol}
                </AppText>
              </ReviewRow>
              <ReviewRow label="Network">
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
                  <ChainLogo chain={fee.chain.key} size={16} />
                  <AppText weight="semibold" size={14}>
                    {fee.chain.name}
                  </AppText>
                </View>
              </ReviewRow>
              <ReviewRow label="Paid from" last>
                <AppText size={14}>
                  {payer.label} · <Mono size={14}>{shortAddress(payer.address)}</Mono>
                </AppText>
              </ReviewRow>
            </Card>
          ) : null}

          <View style={{ gap: 8, padding: 14, borderRadius: 14, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line }}>
            <Bullet icon="check">The rename applies after the fee confirms on-chain.</Bullet>
            <Bullet icon="check">Payments already in flight still reach you.</Bullet>
            <Bullet icon="lock">Next rename possible after {e.cooldownHours} hours.</Bullet>
          </View>
        </>
      )}
    </Screen>
  );
}

function Bullet({ icon, children }: { icon: 'check' | 'lock'; children: React.ReactNode }) {
  return (
    <View style={{ flexDirection: 'row', gap: 8, alignItems: 'flex-start' }}>
      <Icon name={icon} size={16} color={icon === 'check' ? colors.success : colors.muted} />
      <AppText size={14} style={{ flex: 1 }}>
        {children}
      </AppText>
    </View>
  );
}

/** The account that pays: the ID's own destination when it's on this phone, else the first account on that network. */
function payerFor(fee: FeeOption, maxId: MaxId, accounts: AccountInfo[]): { label: string; address: string } | null {
  const ns = fee.chain.namespace;
  if (ns !== 'evm' && ns !== 'solana') return null;
  const dest = maxId.destinations.find((d) => d.namespace === ns)?.address;
  const own = accounts.find((a) => dest && a[ns]?.toLowerCase() === dest.toLowerCase()) ?? accounts.find((a) => a[ns]);
  const address = own?.[ns];
  return own && address ? { label: own.label, address } : null;
}

function RenameProgress({
  maxId,
  requestId,
  onPay,
  onRetry,
  onRestart,
  busyLabel,
  status: payStatus,
  onDone,
}: {
  maxId: MaxId;
  requestId: string;
  onPay: (v: RenameView) => Promise<void>;
  onRetry: (() => void) | null;
  onRestart: () => void;
  busyLabel: string | null;
  status: React.ReactNode;
  onDone: () => void;
}) {
  const qc = useQueryClient();
  const accountId = useAccountId();
  const view = useRenameRequest(maxId.id, requestId);
  const payment = usePayment(view.data?.feePayment.id ?? null);
  const status = view.data?.request.status;

  useEffect(() => {
    if (status === 'completed') qc.invalidateQueries({ queryKey: keys.account(accountId) });
  }, [status, qc, accountId]);

  if (!view.data) {
    return (
      <Screen topPadding={12} header={<TitleHeader title="Rename" onBack={busyLabel || onRetry ? undefined : onDone} />}>
        {view.error ? <Banner tone="danger">{errorMessage(view.error)}</Banner> : <Loading label="Loading the rename" />}
      </Screen>
    );
  }
  const r = view.data.request;
  const p = payment.data ?? view.data.feePayment;
  const done = r.status === 'completed';
  const failed = r.status === 'failed' || r.status === 'expired';
  // Reserved but the fee never went out (the app closed before sending it).
  const unpaid = r.status === 'awaiting_payment' && !p.txHash;
  const explorer = p.explorerUrl ?? (p.txHash ? `${p.chain.explorerTxUrl}${p.txHash}` : null);

  return (
    <Screen
      topPadding={60}
      gap={20}
      footer={
        <>
          {onRetry ? (
            <Button label="Retry" onPress={onRetry} />
          ) : failed ? (
            <Button label="Try another name" onPress={onRestart} />
          ) : unpaid ? (
            <Button label={busyLabel ?? `Pay ${p.amount.display} ${p.asset.symbol ?? p.chain.nativeCoin} fee`} loading={!!busyLabel} onPress={() => void onPay(view.data!)} />
          ) : null}
          {busyLabel || onRetry ? null : <Button label="Done" variant={unpaid || failed ? 'secondary' : 'dark'} onPress={onDone} />}
        </>
      }>
      <View accessibilityRole="summary" style={{ alignItems: 'center', gap: 18 }}>
        <View style={{ width: 72, height: 72, borderRadius: 36, backgroundColor: failed ? colors.dangerSoft : done ? colors.successSoft : colors.sunken, alignItems: 'center', justifyContent: 'center' }}>
          {done || failed ? <Icon name={failed ? 'alert' : 'check'} size={34} color={failed ? colors.danger : colors.success} strokeWidth={2.5} /> : <ActivityIndicator color={colors.muted} />}
        </View>
        <AppText variant="h1" accessibilityRole="header" style={{ fontSize: 32, textAlign: 'center' }}>
          {done ? 'Renamed' : failed ? 'Rename didn’t go through' : unpaid ? 'Fee not paid yet' : 'Waiting for the fee to confirm'}
        </AppText>
        <AppText size={16} color={colors.muted} style={{ textAlign: 'center' }} accessibilityLiveRegion="polite">
          <Handle name={r.oldHandle} size={16} /> → <Handle name={r.newHandle} size={16} accent={colors.clayText} />
        </AppText>
        {!done && !failed && !unpaid ? (
          <>
            <StatusPill status={p.status} />
            <AppText variant="small" style={{ textAlign: 'center' }}>
              This updates on its own. You can leave. The rename finishes without you.
            </AppText>
          </>
        ) : null}
        {unpaid ? (
          <AppText variant="small" style={{ textAlign: 'center' }}>
            The new name is reserved for you until the fee arrives. Pay it now to finish the rename.
          </AppText>
        ) : null}
        {failed ? <Banner tone="danger">{r.failureReason ?? 'The fee wasn’t confirmed in time. Your handle is unchanged.'}</Banner> : null}
        {!done && !failed ? <View style={{ alignSelf: 'stretch', gap: 10 }}>{payStatus}</View> : null}
        {explorer ? <LinkButton label="View fee transaction ↗" weight="semibold" color={colors.clayText} onPress={() => Linking.openURL(explorer)} /> : null}
      </View>
    </Screen>
  );
}
