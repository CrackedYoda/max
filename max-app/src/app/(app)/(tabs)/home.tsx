import { router } from 'expo-router';
import { useEffect, useState } from 'react';
import { Pressable, RefreshControl, ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { PaymentRow } from '@/components/payments/PaymentRow';
import { AppText, Handle, Mono } from '@/components/ui/AppText';
import { Banner, Loading, SectionTitle } from '@/components/ui/Blocks';
import { IconButton } from '@/components/ui/Button';
import { Icon, type IconName } from '@/components/ui/Icon';
import { MaxStatusBanner } from '@/components/wallet/MaxStatusBanner';
import { errorMessage } from '@/error/messages';
import { useActivity } from '@/hooks/useActivity';
import { useBalances, useMe, useMyIds } from '@/hooks/queries';
import type { Balance } from '@/services/chain';
import { useSession } from '@/store/session';
import { colors, fonts, spacing } from '@/theme';
import { shortAddress } from '@/utils/format';
import { formatAmount } from '@/utils/amount';

export default function Home() {
  const insets = useSafeAreaInsets();
  const pendingClaim = useSession((s) => s.pendingClaim);
  const setPendingClaim = useSession((s) => s.setPendingClaim);
  const maxStatus = useSession((s) => s.max);
  const ids = useMyIds();
  const me = useMe();
  const balances = useBalances();
  const activity = useActivity();
  const [refreshing, setRefreshing] = useState(false);

  // Fresh wallets go straight to "Now pick your name".
  useEffect(() => {
    if (pendingClaim) {
      setPendingClaim(false);
      router.push('/claim');
    }
  }, [pendingClaim, setPendingClaim]);

  const primary = me.data?.primaryMaxId ?? null;
  const full = primary ? ids.data?.find((x) => x.id === primary.id) : undefined;
  const name = full?.displayName || (primary ? primary.handle : 'there');
  const top = pickHeadline(balances.data ?? []);

  return (
    <ScrollView
      style={{ flex: 1, backgroundColor: colors.ivory }}
      contentContainerStyle={{ paddingTop: insets.top + 16, paddingHorizontal: spacing.gutter, paddingBottom: 24, gap: 24 }}
      refreshControl={
        <RefreshControl
          refreshing={refreshing}
          onRefresh={async () => {
            setRefreshing(true);
            await Promise.allSettled([balances.refetch(), activity.refetch(), ids.refetch(), me.refetch()]);
            setRefreshing(false);
          }}
        />
      }>
      <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12, flex: 1 }}>
          <View style={{ width: 40, height: 40, borderRadius: 20, backgroundColor: colors.claySoft, alignItems: 'center', justifyContent: 'center' }}>
            <AppText style={{ fontFamily: fonts.display, fontSize: 18, color: colors.clayText }}>{name.replace(/^max\./, '').charAt(0).toUpperCase()}</AppText>
          </View>
          <View style={{ flex: 1 }}>
            <AppText variant="small">Good to see you</AppText>
            <AppText weight="semibold" size={16} numberOfLines={1}>
              {name}
            </AppText>
          </View>
        </View>
        <IconButton icon="settings" label="Wallet and settings" bordered onPress={() => router.navigate('/ids')} />
      </View>

      <MaxStatusBanner />

      <View accessibilityLabel="Your handle" style={{ backgroundColor: colors.night, borderRadius: 24, padding: 24, gap: 18 }}>
        <View style={{ gap: 6 }}>
          <AppText size={13} color={colors.nightMuted}>
            {primary ? 'Get paid at' : 'Pick a name to get paid by'}
          </AppText>
          {primary ? (
            <Handle name={primary.handle} size={30} accent={colors.nightAccent} color={colors.nightText} />
          ) : (
            <Pressable accessibilityRole="button" onPress={() => router.push('/claim')} disabled={maxStatus !== 'ready'}>
              <AppText style={{ fontFamily: fonts.monoSemibold, fontSize: 30, color: colors.nightText }}>
                <AppText style={{ fontFamily: fonts.monoSemibold, fontSize: 30, color: colors.nightAccent }}>max.</AppText>
                {maxStatus === 'ready' ? 'claim →' : '…'}
              </AppText>
            </Pressable>
          )}
        </View>
        <View style={{ gap: 2 }}>
          <AppText size={13} color={colors.nightMuted}>
            {top ? `${top.chain.name} · Account 1` : 'Balance'}
          </AppText>
          <AppText style={{ fontFamily: fonts.display, fontSize: 36, color: colors.nightText }} numberOfLines={1} adjustsFontSizeToFit>
            {balances.isLoading ? '…' : top ? `${formatAmount(top.amount)} ${top.symbol}` : '0'}
          </AppText>
        </View>
        <View style={{ flexDirection: 'row', gap: 8 }}>
          <Action icon="send" label="Send" primary onPress={() => router.navigate('/send')} />
          <Action icon="receive" label="Receive" onPress={() => router.navigate('/receive')} />
          <Action icon="scan" label="Scan" onPress={() => router.push('/scan')} />
        </View>
      </View>

      <View style={{ gap: 10 }}>
        <SectionTitle>Balances</SectionTitle>
        {balances.error ? <Banner tone="danger">{errorMessage(balances.error)}</Banner> : null}
        <View style={{ backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, borderRadius: 18 }}>
          {balances.isLoading ? (
            <Loading label="Loading balances" />
          ) : balances.data?.length ? (
            balances.data.map((b, i) => <BalanceRow key={`${b.chain.key}:${b.asset}:${b.address}`} b={b} last={i === balances.data!.length - 1} />)
          ) : (
            <AppText variant="small" style={{ padding: 16 }}>
              No networks are configured in this build.
            </AppText>
          )}
        </View>
      </View>

      <View style={{ gap: 10 }}>
        <SectionTitle>Recent</SectionTitle>
        {maxStatus !== 'ready' ? (
          <AppText variant="small">Payment history shows once you’re connected to MAX.</AppText>
        ) : activity.isLoading ? (
          <Loading label="Loading activity" />
        ) : activity.error ? (
          <Banner tone="danger">{errorMessage(activity.error)}</Banner>
        ) : activity.items.length ? (
          <View style={{ gap: 2 }}>
            {activity.items.map((a) => (
              <PaymentRow key={`${a.direction}:${a.payment.id}`} payment={a.payment} direction={a.direction} />
            ))}
          </View>
        ) : (
          <AppText variant="small">Nothing yet. Share your handle to get paid.</AppText>
        )}
      </View>
    </ScrollView>
  );
}

/** Largest native balance as the card headline. No fiat: MAX doesn't run a price feed. */
function pickHeadline(list: Balance[]): Balance | null {
  const natives = list.filter((b) => b.asset === 'native');
  if (!natives.length) return null;
  return natives.reduce((best, b) => (Number(b.amount) > Number(best.amount) ? b : best), natives[0]!);
}

function Action({ icon, label, onPress, primary }: { icon: IconName; label: string; onPress: () => void; primary?: boolean }) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      onPress={onPress}
      style={({ pressed }) => ({
        flex: 1,
        alignItems: 'center',
        gap: 6,
        paddingVertical: 12,
        borderRadius: 14,
        backgroundColor: primary ? (pressed ? colors.clayPressed : colors.clay) : pressed ? '#3A3936' : colors.nightRaised,
      })}>
      <Icon name={icon} size={20} color={primary ? colors.surface : colors.nightText} />
      <AppText weight="semibold" size={13} color={primary ? colors.surface : colors.nightText}>
        {label}
      </AppText>
    </Pressable>
  );
}

function BalanceRow({ b, last }: { b: Balance; last: boolean }) {
  return (
    <View
      accessible
      accessibilityLabel={`${b.name} on ${b.chain.name}: ${formatAmount(b.amount)} ${b.symbol}`}
      style={{ flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 14, paddingHorizontal: 16, borderBottomWidth: last ? 0 : 1, borderBottomColor: colors.sunken }}>
      <View style={{ width: 36, height: 36, borderRadius: 18, backgroundColor: colors.sunken, alignItems: 'center', justifyContent: 'center' }}>
        <AppText weight="semibold" size={b.symbol.length > 3 ? 10 : 12} color={colors.body}>
          {b.symbol}
        </AppText>
      </View>
      <View style={{ flex: 1, minWidth: 0 }}>
        <AppText weight="semibold" size={15}>
          {b.name}
        </AppText>
        <AppText variant="caption" numberOfLines={1}>
          {b.chain.name} · <Mono size={12} color={colors.muted}>{shortAddress(b.address)}</Mono>
        </AppText>
      </View>
      <AppText weight="semibold" size={15}>
        {formatAmount(b.amount)} {b.symbol}
      </AppText>
    </View>
  );
}
