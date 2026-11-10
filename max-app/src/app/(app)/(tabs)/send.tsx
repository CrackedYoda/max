import { isMaxApiError, normalizeName, type Chain } from '@max/sdk';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { ScrollView, useWindowDimensions, View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Banner, Choice } from '@/components/ui/Blocks';
import { Button, IconButton, LinkButton } from '@/components/ui/Button';
import { HandleField, Keypad } from '@/components/ui/Fields';
import { Icon } from '@/components/ui/Icon';
import { Screen, TitleHeader } from '@/components/ui/Screen';
import { MaxStatusBanner } from '@/components/wallet/MaxStatusBanner';
import { USDC } from '@/config/env';
import { errorMessage } from '@/error/messages';
import { useBalances, useChains, useHoldings, useResolve, useUsableChains } from '@/hooks/queries';
import { useDebounced } from '@/hooks/useDebounced';
import { useSendDraft } from '@/store/flows';
import { useSession } from '@/store/session';
import { colors, fonts } from '@/theme';
import { applyAmountKey, formatAmount, isPositiveAmount } from '@/utils/amount';
import { shortAddress } from '@/utils/format';
import { stripPrefix } from '@/validation/identity';

/** USD with cents, or "<$0.01" for dust. */
function usd(v: string | number): string | null {
  const n = Number(v);
  if (!Number.isFinite(n)) return null;
  if (n > 0 && n < 0.01) return '<$0.01';
  return `$${n.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

export default function Send() {
  const params = useLocalSearchParams<{ to?: string }>();
  const [to, setTo] = useState('');
  const [chainKey, setChainKey] = useState<string | null>(null);
  const [asset, setAsset] = useState<string>('native');
  // null: the recipient gets the same asset; otherwise a receive-only chain key.
  const [receiveKey, setReceiveKey] = useState<string | null>(null);
  const allChains = useChains();
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
  const sender = chain ? accounts.find((a) => (chain.namespace === 'evm' ? a.evm : a.solana)) : undefined;
  const senderAddress = chain ? (chain.namespace === 'evm' ? sender?.evm : sender?.solana) : undefined;
  // Every token this account holds here; without it, native and USDC as before.
  const holdings = useHoldings(chain?.key, senderAddress);
  const held = holdings.data?.find((h) => (asset === 'native' ? h.kind === 'native' : h.address.toLowerCase() === asset.toLowerCase()));
  const assetParam = held ? (held.kind === 'native' ? 'native' : held.address) : asset !== 'native' && usdc ? usdc : 'native';
  const symbol = held ? held.symbol : assetParam === 'native' ? (chain?.nativeCoin ?? '') : 'USDC';
  const decimals = held ? held.decimals : assetParam === 'native' ? (chain?.nativeDecimals ?? 18) : 6;
  const balanceFallback = balances.data?.find((b) => b.chain.key === chain?.key && b.asset === assetParam && b.address === senderAddress);
  const available = held ? { amount: held.balance.display } : balanceFallback;
  const worth = held?.valueUsd != null ? usd(held.valueUsd) : null;
  const typedUsd = held?.priceUsd && isPositiveAmount(amount) ? usd(Number(amount) * Number(held.priceUsd)) : null;

  const receiveOptions = (allChains.data ?? []).filter((c) => c.receiveOnly && !c.testnet);
  const receiveChain = chain && !chain.testnet ? receiveOptions.find((c) => c.key === receiveKey) : undefined;

  const debounced = useDebounced(to, 350);
  const handle = normalizeName(debounced);
  const resolution = useResolve(maxReady ? handle : null, receiveChain?.key ?? chain?.key ?? '', receiveChain ? 'native' : assetParam);
  const settled = to === debounced;

  let status: { text: string; tone: 'success' | 'danger' | 'muted' } | null = null;
  if (to.trim() && (!settled || resolution.isFetching)) status = { text: 'Checking…', tone: 'muted' };
  else if (to.trim() && !handle) status = { text: 'Not a valid handle', tone: 'danger' };
  else if (resolution.data) status = { text: 'Found', tone: 'success' };
  else if (resolution.error) {
    status = isMaxApiError(resolution.error, 'MAX_ID_NOT_FOUND')
      ? { text: 'No one has this name', tone: 'danger' }
      : isMaxApiError(resolution.error, 'NO_COMPATIBLE_WALLET')
        ? { text: `No address on ${(receiveChain ?? chain)?.name}`, tone: 'danger' }
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
            setTerms({ handle: handle!, chain: chain.key, asset: assetParam, amount, senderAddress: senderAddress!, receiveChain: receiveChain?.key });
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

      {holdings.data ? (
        <ScrollView horizontal showsHorizontalScrollIndicator={false} accessibilityRole="radiogroup" accessibilityLabel="Asset" contentContainerStyle={{ gap: 8 }}>
          {holdings.data.map((h) => {
            const key = h.kind === 'native' ? 'native' : h.address;
            return <Choice key={key} pill label={h.verified ? h.symbol || 'Unknown' : `${h.symbol || 'Unknown'} · Unverified`} selected={held ? held === h : key === 'native'} onPress={() => (setAsset(key), setAmount('0'))} />;
          })}
        </ScrollView>
      ) : usdc ? (
        <View accessibilityRole="radiogroup" accessibilityLabel="Asset" style={{ flexDirection: 'row', gap: 8 }}>
          <Choice pill label={chain.nativeCoin} selected={assetParam === 'native'} onPress={() => (setAsset('native'), setAmount('0'))} />
          <Choice pill label="USDC" selected={assetParam !== 'native'} onPress={() => (setAsset('usdc'), setAmount('0'))} />
        </View>
      ) : null}

      {receiveOptions.length && !chain.testnet ? (
        <ScrollView horizontal showsHorizontalScrollIndicator={false} accessibilityRole="radiogroup" accessibilityLabel="They receive" contentContainerStyle={{ gap: 8, alignItems: 'center' }}>
          <AppText variant="small">They receive</AppText>
          <Choice pill label="Same" selected={!receiveChain} onPress={() => setReceiveKey(null)} />
          {receiveOptions.map((c) => (
            <Choice key={c.key} pill label={c.nativeCoin} selected={receiveChain?.key === c.key} onPress={() => setReceiveKey(c.key)} />
          ))}
        </ScrollView>
      ) : null}

      <View accessibilityLiveRegion="polite" style={{ alignItems: 'center', gap: 6, paddingVertical: 4 }}>
        <AppText accessibilityLabel={`${amount} ${symbol}`} numberOfLines={1} adjustsFontSizeToFit style={{ fontFamily: fonts.display, fontSize: compact ? 40 : 52, letterSpacing: -1 }}>
          {amount} <AppText style={{ fontFamily: fonts.display, fontSize: compact ? 22 : 28, color: colors.muted }}>{symbol}</AppText>
        </AppText>
        {typedUsd ? <AppText variant="small">≈ {typedUsd}</AppText> : null}
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 10, backgroundColor: colors.sunken, borderRadius: 12, paddingHorizontal: 12, paddingVertical: 6 }}>
          <AppText variant="small" color={over ? colors.danger : colors.body} numberOfLines={1} style={{ flexShrink: 1 }}>
            {over
              ? `More than your ${formatAmount(available!.amount)} ${symbol}`
              : `Balance ${available ? formatAmount(available.amount) : '…'} ${symbol}${held ? ` · ${worth ? `≈ ${worth}` : 'No price'}` : senderAddress ? ` · ${shortAddress(senderAddress)}` : ''}`}
          </AppText>
          {available && Number(available.amount) > 0 ? <LinkButton label="Max" weight="semibold" size={13} color={colors.clayText} onPress={() => setAmount(available.amount)} /> : null}
        </View>
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
