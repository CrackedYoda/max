import { normalizeName, type ReservedClaim, type XVerification } from '@max/sdk';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useEffect, useState } from 'react';
import { Alert, View } from 'react-native';

import { AppText, Handle } from '@/components/ui/AppText';
import { Banner, Card, List, ListRow, Loading, Pill, SectionTitle } from '@/components/ui/Blocks';
import { Button } from '@/components/ui/Button';
import { HandleField } from '@/components/ui/Fields';
import { Screen, TitleHeader } from '@/components/ui/Screen';
import { max } from '@/api/client';
import { keys } from '@/api/keys';
import { errorMessage } from '@/error/messages';
import { useAccountId, useAvailability, useReservedClaims, useVerifications } from '@/hooks/queries';
import { useDebounced } from '@/hooks/useDebounced';
import { completeX, getPendingName, setPendingName, verifyWithX, type XResult } from '@/services/xVerify';
import { toast } from '@/store/flows';
import { formatDate, relativeTime } from '@/utils/format';
import { stripPrefix } from '@/validation/identity';

// Mirrors X_CLAIM_FRESHNESS on the server; the server is authoritative.
const CLAIM_FRESHNESS_MS = 15 * 60_000;

const CLAIM_STATUS: Record<ReservedClaim['status'], { label: string; tone: 'neutral' | 'success' | 'danger' | 'clay' }> = {
  pending_review: { label: 'In review', tone: 'clay' },
  active: { label: 'Active', tone: 'success' },
  lapsed: { label: 'Re-verify to unlock', tone: 'danger' },
  denied: { label: 'Denied', tone: 'danger' },
  revoked: { label: 'Revoked', tone: 'danger' },
};

/**
 * Reserved names: prove control of the matching X account, then claim the name.
 * Params: `name` to prefill; `state`/`code`/`error` when the X deep link lands here.
 */
export default function Verify() {
  const params = useLocalSearchParams<{ name?: string; state?: string; code?: string; error?: string }>();
  const qc = useQueryClient();
  const accountId = useAccountId();
  const verifications = useVerifications();
  const claims = useReservedClaims();
  const [name, setName] = useState(() => stripPrefix(params.name ?? (params.state ? getPendingName() : '')));
  const [result, setResult] = useState<XResult | null>(null);
  const now = useNow(15_000);

  const debounced = useDebounced(name);
  const canonical = normalizeName(debounced);
  const avail = useAvailability(debounced);
  const settled = name === debounced;
  const a = settled ? avail.data : undefined;
  const active = (verifications.data ?? []).filter((v) => !v.revokedAt);

  const reservedFor = a && !a.available && a.reason === 'reserved' ? a.xHandle?.toLowerCase() : undefined;
  const blocked = a?.reason === 'reserved' && a.claimPolicy === 'blocked';
  const match = reservedFor ? active.find((v) => v.username.toLowerCase() === reservedFor) : undefined;
  const fresh = match ? now - new Date(match.verifiedAt).getTime() < CLAIM_FRESHNESS_MS : false;
  const ready = !!(reservedFor && match && fresh && !blocked);

  const onVerified = (r: XResult) => {
    setResult(r);
    if (r.kind === 'verified') {
      qc.invalidateQueries({ queryKey: keys.account(accountId) });
      setName((n) => n || getPendingName());
    }
  };

  const startX = useMutation({ mutationFn: (pending: string) => verifyWithX(pending), onSuccess: onVerified });

  // Android can deliver the deep link through the router instead of the auth session.
  const deep = useMutation({ mutationFn: completeX, onSuccess: onVerified });
  const { state, code, error } = params;
  const { mutate: completeDeep } = deep;
  useEffect(() => {
    if (state) completeDeep({ state, code, error });
  }, [state, code, error, completeDeep]);

  const claim = useMutation({
    mutationFn: () => max().reservedClaims.claim({ name: canonical! }),
    onSuccess: (c) => {
      qc.invalidateQueries({ queryKey: keys.account(accountId) });
      setPendingName('');
      setResult(null);
      toast(c.status === 'pending_review' ? `${c.handle} submitted for review` : `${c.handle} is yours`);
      setName('');
    },
  });

  let tone: 'muted' | 'success' | 'danger' = 'muted';
  let message = 'Enter the reserved name you want.';
  if (name.trim()) {
    if (!settled || (canonical && avail.isFetching && !avail.data)) message = 'Checking…';
    else if (!canonical) [tone, message] = ['danger', 'Use 3–32 lowercase letters, numbers or hyphens.'];
    else if (avail.error) [tone, message] = ['danger', errorMessage(avail.error)];
    else if (a?.available) [tone, message] = ['danger', `${a.handle} isn’t reserved. Anyone can claim it.`];
    else if (blocked) [tone, message] = ['danger', `${a!.handle} is permanently reserved and can’t be claimed.`];
    else if (reservedFor && match && fresh) [tone, message] = ['success', `Reserved for @${reservedFor}. You verified that account ${relativeTime(match.verifiedAt)}.`];
    else if (reservedFor && match) message = `You verified @${reservedFor} ${relativeTime(match.verifiedAt)}. Claims need a verification from the last 15 minutes. Verify again.`;
    else if (reservedFor) message = `Reserved for @${reservedFor} on X. Verify that account to claim it.`;
    else if (a) [tone, message] = ['danger', `${a.handle} isn’t available.`];
  }

  const busyX = startX.isPending || deep.isPending;
  const err = claim.error ?? startX.error ?? deep.error;
  const verify = () => startX.mutate(canonical ?? '');
  const close = () => (router.canGoBack() ? router.back() : router.replace('/ids'));

  return (
    <Screen
      scroll
      gap={22}
      header={<TitleHeader title="Reserved names" onBack={close} />}
      footer={
        ready ? (
          <Button label={`Claim max.${canonical}`} loading={claim.isPending} onPress={() => claim.mutate()} />
        ) : (
          <Button label={reservedFor ? `Verify @${reservedFor} on X` : 'Verify with X'} disabled={!reservedFor || blocked} loading={busyX} onPress={verify} />
        )
      }>
      <AppText variant="small">
        Well-known names are held for their owners on X. Prove you control the matching X account, then claim the name. MAX only reads your public profile.
      </AppText>

      {result?.kind === 'verified' ? (
        <Banner tone="success" title={`Verified @${result.verification.username}`}>
          {result.renewed > 0 ? `Also renewed ${result.renewed} reserved ${result.renewed === 1 ? 'name' : 'names'}. ` : ''}
          {name ? 'Claim your name now. The verification is valid for 15 minutes.' : 'You can claim names reserved for this account.'}
        </Banner>
      ) : null}

      <HandleField label="Reserved name" value={name} onChangeText={(v) => setName(stripPrefix(v))} message={message} messageTone={tone} />
      <AppText variant="caption">
        Personal accounts are claimed instantly. Brand and organisation names are reviewed by MAX first. Keep your X account: names must be re-verified every 180 days.
      </AppText>
      {err ? (
        <Banner tone="danger" title={claim.error ? 'Couldn’t claim it' : 'X verification didn’t finish'}>
          {errorMessage(err)}
        </Banner>
      ) : null}

      <XAccounts loading={verifications.isLoading} error={verifications.error} accounts={verifications.data ?? []} onVerify={verify} busy={busyX} />
      <Claims loading={claims.isLoading} error={claims.error} claims={claims.data ?? []} now={now} onVerify={verify} busy={busyX} />
    </Screen>
  );
}

function XAccounts({ loading, error, accounts, onVerify, busy }: { loading: boolean; error: unknown; accounts: XVerification[]; onVerify: () => void; busy: boolean }) {
  const qc = useQueryClient();
  const accountId = useAccountId();
  const revoke = useMutation({
    mutationFn: (id: string) => max().verifications.revoke(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: keys.account(accountId) });
      toast('X account removed');
    },
    onError: (e) => Alert.alert('Couldn’t remove it', errorMessage(e)),
  });

  const confirmRemove = (v: XVerification) =>
    Alert.alert(`Remove @${v.username}?`, 'Names reserved through it will lapse at their next re-verification.', [
      { text: 'Cancel', style: 'cancel' },
      { text: 'Remove', style: 'destructive', onPress: () => revoke.mutate(v.id) },
    ]);

  return (
    <View style={{ gap: 10 }}>
      <SectionTitle right={accounts.length ? <Button label="+ Verify" variant="soft" size="sm" loading={busy} onPress={onVerify} /> : undefined}>Verified X accounts</SectionTitle>
      {error ? <Banner tone="danger">{errorMessage(error)}</Banner> : null}
      {loading ? (
        <Loading label="Loading X accounts" />
      ) : accounts.length ? (
        <List>
          {accounts.map((v, i) => (
            <ListRow
              key={v.id}
              last={i === accounts.length - 1}
              title={`@${v.username}${v.displayName ? ` · ${v.displayName}` : ''}`}
              subtitle={v.revokedAt ? `Removed ${formatDate(v.revokedAt)}${v.revokeReason ? ` · ${v.revokeReason}` : ''}` : `Verified ${relativeTime(v.verifiedAt)}`}
              right={v.revokedAt ? <Pill small label="Removed" /> : <Button label="Remove" variant="secondary" size="sm" onPress={() => confirmRemove(v)} />}
            />
          ))}
        </List>
      ) : (
        <AppText variant="small">No X accounts yet.</AppText>
      )}
    </View>
  );
}

function Claims({ loading, error, claims, now, onVerify, busy }: { loading: boolean; error: unknown; claims: ReservedClaim[]; now: number; onVerify: () => void; busy: boolean }) {
  if (!loading && !claims.length && !error) return null;
  return (
    <View style={{ gap: 10 }}>
      <SectionTitle>Your reserved names</SectionTitle>
      {error ? <Banner tone="danger">{errorMessage(error)}</Banner> : null}
      {loading ? <Loading label="Loading your claims" /> : null}
      {claims.map((c) => {
        const s = CLAIM_STATUS[c.status];
        const dueSoon = c.status === 'active' && !!c.verifiedUntil && new Date(c.verifiedUntil).getTime() - now < 14 * 86_400_000;
        return (
          <Card key={c.id} style={{ gap: 6 }}>
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
              <Handle name={c.handle} size={18} />
              <Pill small tone={s.tone} label={s.label} />
            </View>
            <AppText variant="small">
              Via @{c.xUsername}
              {c.status === 'active' && c.verifiedUntil ? ` · re-verify by ${formatDate(c.verifiedUntil)}` : ''}
              {c.status === 'pending_review' ? ` · submitted ${relativeTime(c.createdAt)}` : ''}
            </AppText>
            {c.reviewNote ? <AppText size={14}>MAX: {c.reviewNote}</AppText> : null}
            {c.status === 'lapsed' || dueSoon ? (
              <View style={{ alignItems: 'flex-start', paddingTop: 4 }}>
                <Button label={`Re-verify @${c.xUsername}`} variant={c.status === 'lapsed' ? 'primary' : 'secondary'} size="sm" loading={busy} onPress={onVerify} />
              </View>
            ) : null}
          </Card>
        );
      })}
    </View>
  );
}

function useNow(ms: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), ms);
    return () => clearInterval(t);
  }, [ms]);
  return now;
}
