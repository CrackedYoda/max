import { isMaxApiError, normalizeName, type Chain } from '@max/sdk';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { ScrollView, useWindowDimensions, View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Banner, Choice } from '@/components/ui/Blocks';
import { Button, IconButton } from '@/components/ui/Button';
import { HandleField, Keypad } from '@/components/ui/Fields';
import { Icon } from '@/components/ui/Icon';
import { Screen, TitleHeader } from '@/components/ui/Screen';
import { MaxStatusBanner } from '@/components/wallet/MaxStatusBanner';
import { USDC } from '@/config/env';
import { errorMessage } from '@/error/messages';
import { useBalances, useResolve, useUsableChains } from '@/hooks/queries';
import { useDebounced } from '@/hooks/useDebounced';
import { useSendDraft } from '@/store/flows';
import { useSession } from '@/store/session';
import { colors, fonts } from '@/theme';
import { applyAmountKey, formatAmount, isPositiveAmount } from '@/utils/amount';
import { shortAddress } from '@/utils/format';
import { stripPrefix } from '@/validation/identity';

export default function Send() {
  const params = useLocalSearchParams<{ to?: string }>();
  const [to, setTo] = useState('');
  const [chainKey, setChainKey] = useState<string | null>(null);
  const [asset, setAsset] = useState<string>('native');
  const [amount, setAmount] = useState('0');
  const { chains, isLoading: chainsLoading, error: chainsError } = useUsableChains();
  const accounts = useSession((s) => s.accounts);
  const maxReady = useSession((s) => s.max === 'ready');
  const balances = useBalances();
  const setTerms = useSendDraft((s) => s.setTerms);
  // The artboard is 844pt tall; shorter phones get a tighter keypad so Review stays on screen.
  const { height } = useWindowDimensions();
  const compact = height < 760;
  const keyHeight = Math.round(Math.max(34, Math.min(54, (height - 540) / 4)));

  // Prefill from a scanned QR / deep link (adjusting state on param change, no effect).
  // The handle is public, fine in a param.
  const [seenTo, setSeenTo] = useState<string | undefined>(undefined);
  if (params.to !== seenTo) {
    setSeenTo(params.to);
    if (params.to) setTo(stripPrefix(params.to));
  }

  const chain: Chain | undefined = chains.find((c) => c.key === chainKey) ?? chains[0];
  const usdc = chain ? USDC[chain.key] : undefined;
  const assetParam = asset !== 'native' && usdc ? usdc : 'native';
  const symbol = assetParam === 'native' ? (chain?.nativeCoin ?? '') : 'USDC';
  const decimals = assetParam === 'native' ? (chain?.nativeDecimals ?? 18) : 6;

  const sender = chain ? accounts.find((a) => (chain.namespace === 'evm' ? a.evm : a.solana)) : undefined;
  const senderAddress = chain ? (chain.namespace === 'evm' ? sender?.evm : sender?.solana) : undefined;
  const available = balances.data?.find((b) => b.chain.key === chain?.key && b.asset === assetParam && b.address === senderAddress);

  const debounced = useDebounced(to, 350);
  const handle = normalizeName(debounced);
  const resolution = useResolve(maxReady ? handle : null, chain?.key ?? '', assetParam);
  const settled = to === debounced;

  let status: { text: string; tone: 'success' | 'danger' | 'muted' } | null = null;
  if (to.trim() && (!settled || resolution.isFetching)) status = { text: 'Checking…', tone: 'muted' };
  else if (to.trim() && !handle) status = { text: 'Not a valid handle', tone: 'danger' };
  else if (resolution.data) status = { text: 'Found', tone: 'success' };
  else if (resolution.error) {
    status = isMaxApiError(resolution.error, 'MAX_ID_NOT_FOUND')
      ? { text: 'No one has this name', tone: 'danger' }
      : isMaxApiError(resolution.error, 'NO_COMPATIBLE_WALLET')
        ? { text: `No wallet on ${chain?.name}`, tone: 'danger' }
        : { text: errorMessage(resolution.error), tone: 'danger' };
  }

  const over = available && isPositiveAmount(amount) ? Number(amount) > Number(available.amount) : false;
  const ownWallet = !!resolution.data && !!senderAddress && resolution.data.address.toLowerCase() === senderAddress.toLowerCase();
  const canReview = !!resolution.data && settled && isPositiveAmount(amount) && !!senderAddress && !over && !ownWallet && maxReady;

  if (chainsLoading) return <Screen header={<TitleHeader title="Send" />}>{null}</Screen>;
  if (!chain) {
    return (
      <Screen header={<TitleHeader title="Send" />}>
        <Banner tone="danger">{chainsError ? errorMessage(chainsError) : 'No networks are configured in this build.'}</Banner>
      </Screen>
    );
  }

  return (
    <Screen
      gap={compact ? 10 : 16}
      bottomInset={false}
      header={<TitleHeader title="Send" right={<IconButton icon="scan" label="Scan a QR code" onPress={() => router.push('/scan')} />} />}
      footer={
        <Button
          label="Review"
          disabled={!canReview}
          onPress={() => {
            setTerms({ handle: handle!, chain: chain.key, asset: assetParam, amount, senderAddress: senderAddress! });
            router.push('/review');
          }}
        />
      }>
      <MaxStatusBanner />
      <HandleField
        label="To"
        size={18}
        value={to}
        onChangeText={(v) => setTo(stripPrefix(v))}
        trailing={
          status ? (
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 4, maxWidth: 150 }}>
              {status.tone === 'success' ? <Icon name="check" size={14} color={colors.success} /> : null}
              <AppText numberOfLines={1} weight="semibold" size={12} color={status.tone === 'success' ? colors.success : status.tone === 'danger' ? colors.danger : colors.muted}>
                {status.text}
              </AppText>
            </View>
          ) : null
        }
      />

      <View>
        <ScrollView horizontal showsHorizontalScrollIndicator={false} accessibilityRole="radiogroup" accessibilityLabel="Network" contentContainerStyle={{ gap: 8 }}>
          {chains.map((c) => (
            <Choice
              key={c.key}
              pill
              label={c.name}
              selected={c.key === chain.key}
              onPress={() => {
                setChainKey(c.key);
                setAmount('0');
              }}
            />
          ))}
        </ScrollView>
      </View>

      {usdc ? (
        <View accessibilityRole="radiogroup" accessibilityLabel="Asset" style={{ flexDirection: 'row', gap: 8 }}>
          <Choice pill label={chain.nativeCoin} selected={assetParam === 'native'} onPress={() => (setAsset('native'), setAmount('0'))} />
          <Choice pill label="USDC" selected={assetParam !== 'native'} onPress={() => (setAsset('usdc'), setAmount('0'))} />
        </View>
      ) : null}

      <View accessibilityLiveRegion="polite" style={{ alignItems: 'center', gap: 6, paddingVertical: 4 }}>
        <AppText accessibilityLabel={`${amount} ${symbol}`} numberOfLines={1} adjustsFontSizeToFit style={{ fontFamily: fonts.display, fontSize: compact ? 40 : 52, letterSpacing: -1 }}>
          {amount} <AppText style={{ fontFamily: fonts.display, fontSize: compact ? 22 : 28, color: colors.muted }}>{symbol}</AppText>
        </AppText>
        <AppText variant="small" color={over ? colors.danger : colors.muted}>
          {over
            ? `More than your ${formatAmount(available!.amount)} ${symbol}`
            : `Available: ${available ? formatAmount(available.amount) : '…'} ${symbol}${senderAddress ? ` · ${shortAddress(senderAddress)}` : ''}`}
        </AppText>
        {ownWallet ? (
          <AppText variant="small" color={colors.danger}>
            That’s your own wallet.
          </AppText>
        ) : null}
        {!senderAddress ? (
          <AppText variant="small" color={colors.danger}>
            No account on this phone for {chain.name}.
          </AppText>
        ) : null}
      </View>

      <View style={{ marginTop: 'auto' }}>
        <Keypad keyHeight={keyHeight} extra="." onKey={(k) => setAmount((a) => applyAmountKey(a, k, decimals))} />
      </View>
    </Screen>
  );
}
