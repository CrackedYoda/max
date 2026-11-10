import { normalizeName, type MaxIdType } from '@max/sdk';
import { router } from 'expo-router';
import { useState } from 'react';
import { View } from 'react-native';

import { AppText, Mono } from '@/components/ui/AppText';
import { Banner, Heading, Pill, Segmented } from '@/components/ui/Blocks';
import { Button, LinkButton } from '@/components/ui/Button';
import { HandleField } from '@/components/ui/Fields';
import { Screen, TitleHeader } from '@/components/ui/Screen';
import { errorCode, errorDetails, errorMessage } from '@/error/messages';
import { useAvailability, useClaimId, useMe, useMyIds } from '@/hooks/queries';
import { useDebounced } from '@/hooks/useDebounced';
import { connectMax } from '@/services/session';
import { toast } from '@/store/flows';
import { useSession } from '@/store/session';
import { colors } from '@/theme';
import { shortAddress } from '@/utils/format';
import { usd } from '@/utils/money';
import { stripPrefix } from '@/validation/identity';

export default function Claim() {
  const [q, setQ] = useState('');
  const [type, setType] = useState<MaxIdType>('human');
  const debounced = useDebounced(q);
  const name = normalizeName(debounced);
  const avail = useAvailability(debounced);
  const claim = useClaimId();
  const accounts = useSession((s) => s.accounts);
  const maxStatus = useSession((s) => s.max);
  const ids = useMyIds();
  const me = useMe();
  const defaultHandle = me.data?.defaultMaxId?.handle;
  // The auto-created default does not count: the first claim is still the onboarding step.
  const first = !ids.data?.some((x) => !x.isDefault);

  // Never show a verdict about a different name than the one in the field.
  const settled = q === debounced;
  let tone: 'muted' | 'success' | 'danger' = 'muted';
  let message = '3–32 lowercase letters, numbers or hyphens.';
  if (!q.trim()) {
    // idle
  } else if (!settled || (name && avail.isFetching && !avail.data)) {
    message = 'Checking…';
  } else if (!name) {
    tone = 'danger';
    message = 'Use 3–32 lowercase letters, numbers or hyphens.';
  } else if (avail.error) {
    tone = 'danger';
    message = errorMessage(avail.error);
  } else if (avail.data) {
    const a = avail.data;
    if (a.available) {
      tone = 'success';
      message = `${a.handle} is available.`;
    } else {
      tone = 'danger';
      message =
        a.reason === 'reserved'
          ? a.xHandle && a.claimPolicy !== 'blocked'
            ? `${a.handle} is reserved for @${a.xHandle} on X.`
            : `${a.handle} is reserved.`
          : a.reason === 'quarantined'
            ? `${a.handle} was released recently. Try again later.`
            : a.reason === 'invalid'
              ? `${a.handle} uses a reserved format. Try another.`
              : `${a.handle} is taken. Try another.`;
    }
  }
  // Reserved for an X account: claimed through verification, not here.
  const viaX = settled && avail.data?.reason === 'reserved' && avail.data.claimPolicy !== 'blocked' && !!avail.data.xHandle;
  const canClaim = settled && !!name && !!avail.data?.available && maxStatus === 'ready';

  const evm = accounts.find((a) => a.evm)?.evm;
  const sol = accounts.find((a) => a.solana)?.solana;

  const done = () => (router.canGoBack() ? router.back() : router.replace('/home'));

  return (
    <Screen
      scroll
      gap={22}
      topPadding={first ? 40 : 12}
      header={first ? undefined : <TitleHeader title="Claim a name" onBack={done} />}
      footer={
        <>
          <Button
            label={canClaim ? `Claim max.${name}` : 'Claim'}
            disabled={!canClaim}
            loading={claim.isPending}
            onPress={() =>
              claim.mutate(
                { name: name!, type },
                {
                  onSuccess: (id) => {
                    toast(`${id.handle} is yours`);
                    done();
                  },
                },
              )
            }
          />
          {first ? <LinkButton label="Skip for now" onPress={done} /> : null}
        </>
      }>
      {first ? (
        <View style={{ gap: 10 }}>
          <Pill tone="success" icon="check" label="Wallet ready" />
          <Heading title="Now pick your name" lead="People will pay you by typing it." />
        </View>
      ) : null}

      {defaultHandle ? <AppText variant="small">You already have {defaultHandle} for receiving.</AppText> : null}

      {maxStatus === 'connecting' || maxStatus === 'signed_out' ? <Banner>Connecting to MAX…</Banner> : null}
      {maxStatus === 'unavailable' ? (
        <Banner tone="clay" title="Can’t reach MAX right now">
          <LinkButton label="Try again" onPress={() => void connectMax()} weight="semibold" color={colors.clayText} size={14} center={false} />
        </Banner>
      ) : null}

      <HandleField label="Handle" value={q} onChangeText={(v) => setQ(stripPrefix(v))} message={message} messageTone={tone} autoFocus={first} />
      {viaX && name ? (
        <Banner tone="clay" title={`Is @${avail.data!.xHandle} yours?`}>
          <LinkButton
            label="Verify on X and claim it →"
            onPress={() => router.push({ pathname: '/verify', params: { name } })}
            weight="semibold"
            color={colors.clayText}
            size={14}
            center={false}
          />
        </Banner>
      ) : null}

      <View style={{ gap: 8 }}>
        <AppText variant="label">This handle is for</AppText>
        <Segmented
          label="This handle is for"
          value={type}
          onChange={setType}
          options={[
            { value: 'human', label: 'Me' },
            { value: 'agent', label: 'An AI agent' },
          ]}
        />
      </View>

      <View style={{ backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, borderRadius: 18, padding: 16, gap: 10 }}>
        <AppText weight="semibold" size={13} color={colors.muted} accessibilityRole="header">
          Payments will go to this wallet
        </AppText>
        {evm ? (
          <View style={{ flexDirection: 'row', justifyContent: 'space-between' }}>
            <AppText weight="semibold" size={14}>
              EVM chains
            </AppText>
            <Mono size={14}>{shortAddress(evm)}</Mono>
          </View>
        ) : null}
        {sol ? (
          <View style={{ flexDirection: 'row', justifyContent: 'space-between' }}>
            <AppText weight="semibold" size={14}>
              Solana
            </AppText>
            <Mono size={14}>{shortAddress(sol)}</Mono>
          </View>
        ) : null}
      </View>

      {claim.error ? (
        errorCode(claim.error) === 'INSUFFICIENT_REGISTRATION_BALANCE' ? (
          <RegistrationGate details={errorDetails(claim.error)} />
        ) : (
          <Banner tone="danger">{errorMessage(claim.error)}</Banner>
        )
      ) : null}
    </Screen>
  );
}

/** INSUFFICIENT_REGISTRATION_BALANCE: counted vs required USD, both from the API's error details. */
function RegistrationGate({ details }: { details?: Record<string, unknown> }) {
  const required = usd(details?.requiredUsd);
  const counted = usd(details?.countedUsd);
  return (
    <Banner tone="clay" title="Your linked wallets don’t hold enough to claim yet">
      <View style={{ gap: 6 }}>
        {required && counted ? (
          <Banner.Text>{`MAX counted ${counted} across your linked wallets; claiming needs ${required}.`}</Banner.Text>
        ) : null}
        <Banner.Text>{`Hold at least ${required ?? 'the minimum'} in USDC, USDT, ETH, SOL or POL in a linked wallet, then try again. Nothing is spent or locked.`}</Banner.Text>
        <LinkButton label="Add or import another account →" onPress={() => router.push('/add-account')} weight="semibold" color={colors.clayText} size={14} center={false} />
      </View>
    </Banner>
  );
}
