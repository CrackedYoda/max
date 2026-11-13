import type { ReceivableAsset } from '@max/sdk';
import { router, useLocalSearchParams } from 'expo-router';
import { useMemo, useState } from 'react';
import { View } from 'react-native';

import { AppText, Mono } from '@/components/ui/AppText';
import { Banner, List, ListRow, Loading, Pill } from '@/components/ui/Blocks';
import { ChainLogo } from '@/components/ui/ChainLogo';
import { TextField } from '@/components/ui/Fields';
import { Icon } from '@/components/ui/Icon';
import { Screen, TitleHeader } from '@/components/ui/Screen';
import { TokenLogo } from '@/components/ui/TokenLogo';
import { errorMessage } from '@/error/messages';
import { useChains, useReceivableAssets } from '@/hooks/queries';
import { useReceivePick } from '@/store/flows';
import { colors } from '@/theme';
import { shortAddress } from '@/utils/format';

const idOf = (a: ReceivableAsset) => `${a.chain}:${a.kind === 'native' ? 'native' : (a.address ?? '').toLowerCase()}`;

/**
 * "They receive": the same asset on the sending network, or anything a
 * cross-chain route can deliver, searchable and grouped by chain.
 */
export default function ReceiveAsset() {
  const { chain: sendKey } = useLocalSearchParams<{ chain: string }>();
  const chains = useChains();
  const assets = useReceivableAssets();
  const picked = useReceivePick((s) => s.asset);
  const setPicked = useReceivePick((s) => s.set);
  const [q, setQ] = useState('');
  const name = (key: string) => chains.data?.find((c) => c.key === key)?.name ?? key;
  const sendChain = chains.data?.find((c) => c.key === sendKey);
  const current = picked ? idOf(picked) : null;

  const groups = useMemo(() => {
    const needle = q.trim().toLowerCase();
    const byChain = new Map<string, ReceivableAsset[]>();
    for (const a of assets.data ?? []) {
      const chainName = chains.data?.find((c) => c.key === a.chain)?.name ?? a.chain;
      if (needle && !`${a.symbol} ${chainName} ${a.address ?? ''}`.toLowerCase().includes(needle)) continue;
      byChain.set(a.chain, [...(byChain.get(a.chain) ?? []), a]);
    }
    return [...byChain.entries()];
  }, [assets.data, chains.data, q]);

  const pick = (a: ReceivableAsset | null) => {
    setPicked(a);
    router.back();
  };

  return (
    <Screen scroll topPadding={0} header={<TitleHeader title="They receive" icon="close" onBack={() => router.back()} />}>
      <TextField value={q} onChangeText={setQ} placeholder="Search a token or chain" accessibilityLabel="Search a token or chain" returnKeyType="search" />

      {!q.trim() ? (
        <List>
          <ListRow
            last
            accessibilityLabel={`The same asset, on ${sendChain?.name ?? 'this network'}`}
            onPress={() => pick(null)}
            title={
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: 10 }}>
                {sendChain ? <ChainLogo chain={sendChain.key} size={26} /> : null}
                <View style={{ flex: 1 }}>
                  <AppText weight="semibold" size={14}>The same asset</AppText>
                  <AppText variant="caption">Delivered on {sendChain?.name ?? 'this network'}, no conversion</AppText>
                </View>
              </View>
            }
            right={!current ? <Icon name="check" size={18} color={colors.clayText} /> : null}
          />
        </List>
      ) : null}

      {assets.isLoading ? <Loading label="Loading what NEAR Intents can deliver" /> : null}
      {assets.error ? <Banner tone="danger">{errorMessage(assets.error)}</Banner> : null}
      {!assets.isLoading && !assets.error && !groups.length ? (
        <AppText variant="small">{q.trim() ? `Nothing matches “${q.trim()}”.` : 'Cross-chain delivery isn’t available right now.'}</AppText>
      ) : null}

      {groups.map(([chainKey, list]) => (
        <View key={chainKey} style={{ gap: 8 }}>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8, paddingHorizontal: 4 }} accessibilityRole="header">
            <ChainLogo chain={chainKey} size={16} />
            <AppText weight="semibold" size={12} color={colors.muted} style={{ textTransform: 'uppercase', letterSpacing: 0.7 }}>
              {name(chainKey)}
            </AppText>
            {chains.data?.find((c) => c.key === chainKey)?.receiveOnly ? <Pill small label="Receive only" /> : null}
          </View>
          <List>
            {list.map((a, i) => {
              const on = idOf(a) === current;
              return (
                <ListRow
                  key={idOf(a)}
                  last={i === list.length - 1}
                  accessibilityLabel={`${a.symbol} on ${name(chainKey)}${a.verified ? '' : ', unverified'}`}
                  onPress={() => pick(a)}
                  title={
                    <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12 }}>
                      <TokenLogo symbol={a.symbol} size={30} />
                      <View style={{ flex: 1, gap: 2 }}>
                        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
                          <AppText weight="semibold" size={14}>{a.symbol}</AppText>
                          {!a.verified ? <Pill small label="Unverified" /> : null}
                        </View>
                        {a.kind === 'native' ? (
                          <AppText variant="caption">{name(chainKey)} native coin</AppText>
                        ) : (
                          <Mono size={12} color={colors.muted}>{shortAddress(a.address ?? '')}</Mono>
                        )}
                      </View>
                    </View>
                  }
                  right={on ? <Icon name="check" size={18} color={colors.clayText} /> : null}
                />
              );
            })}
          </List>
        </View>
      ))}
    </Screen>
  );
}
