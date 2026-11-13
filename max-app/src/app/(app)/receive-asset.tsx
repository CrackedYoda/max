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
 * "They receive": the same asset on the sending network, or pick a chain and
 * then one of the coins a cross-chain route can deliver there. Search spans
 * every chain.
 */
export default function ReceiveAsset() {
  const { chain: sendKey } = useLocalSearchParams<{ chain: string }>();
  const chains = useChains();
  const assets = useReceivableAssets();
  const picked = useReceivePick((s) => s.asset);
  const setPicked = useReceivePick((s) => s.set);
  const [q, setQ] = useState('');
  // null shows the list of chains; a chain key shows that chain's coins.
  const [chainKey, setChainKey] = useState<string | null>(null);
  const name = (key: string) => chains.data?.find((c) => c.key === key)?.name ?? key;
  const sendChain = chains.data?.find((c) => c.key === sendKey);
  const current = picked ? idOf(picked) : null;
  const needle = q.trim().toLowerCase();

  const byChain = useMemo(() => {
    const m = new Map<string, ReceivableAsset[]>();
    for (const a of assets.data ?? []) m.set(a.chain, [...(m.get(a.chain) ?? []), a]);
    return m;
  }, [assets.data]);
  const matches = (a: ReceivableAsset) => `${a.symbol} ${name(a.chain)} ${a.address ?? ''}`.toLowerCase().includes(needle);
  const shown = chainKey ? (byChain.get(chainKey) ?? []).filter((a) => !needle || matches(a)) : needle ? (assets.data ?? []).filter(matches) : [];

  const pick = (a: ReceivableAsset | null) => {
    setPicked(a);
    router.back();
  };
  const back = () => {
    if (chainKey) {
      setChainKey(null);
      setQ('');
    } else router.back();
  };

  return (
    <Screen
      scroll
      topPadding={0}
      header={<TitleHeader title={chainKey ? name(chainKey) : 'They receive'} icon={chainKey ? 'back' : 'close'} onBack={back} />}>
      <TextField
        value={q}
        onChangeText={setQ}
        placeholder={chainKey ? `Search ${name(chainKey)}` : 'Search a token or chain'}
        accessibilityLabel={chainKey ? `Search ${name(chainKey)}` : 'Search a token or chain'}
        returnKeyType="search"
      />

      {!chainKey && !needle ? (
        <>
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

          {assets.isLoading ? <Loading label="Loading coins" /> : null}
          {assets.error ? <Banner tone="danger">{errorMessage(assets.error)}</Banner> : null}
          {!assets.isLoading && !assets.error && !byChain.size ? <AppText variant="small">Cross-chain delivery isn’t available right now.</AppText> : null}

          {byChain.size ? (
            <View style={{ gap: 8 }}>
              <AppText weight="semibold" size={12} color={colors.muted} style={{ textTransform: 'uppercase', letterSpacing: 0.7, paddingHorizontal: 4 }}>
                Or convert on another chain
              </AppText>
              <List>
                {[...byChain.entries()].map(([key, list], i, all) => {
                  const holds = picked?.chain === key;
                  return (
                    <ListRow
                      key={key}
                      chevron
                      last={i === all.length - 1}
                      accessibilityLabel={`${name(key)}, ${list.length} ${list.length === 1 ? 'coin' : 'coins'}`}
                      onPress={() => {
                        setChainKey(key);
                        setQ('');
                      }}
                      title={
                        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12 }}>
                          <ChainLogo chain={key} size={26} />
                          <View style={{ flex: 1 }}>
                            <AppText weight="semibold" size={14}>{name(key)}</AppText>
                            <AppText variant="caption" color={holds ? colors.clayText : undefined}>
                              {holds && picked ? `${picked.symbol} selected` : `${list.length} ${list.length === 1 ? 'coin' : 'coins'}`}
                            </AppText>
                          </View>
                        </View>
                      }
                    />
                  );
                })}
              </List>
            </View>
          ) : null}
        </>
      ) : (
        <>
          {!shown.length ? <AppText variant="small">Nothing matches “{q.trim()}”.</AppText> : null}
          {shown.length ? (
            <List>
              {shown.map((a, i) => {
                const on = idOf(a) === current;
                return (
                  <ListRow
                    key={idOf(a)}
                    last={i === shown.length - 1}
                    accessibilityLabel={`${a.symbol} on ${name(a.chain)}${a.verified ? '' : ', unverified'}`}
                    onPress={() => pick(a)}
                    title={
                      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12 }}>
                        {/* Searching across chains, the badge says which chain each coin is on. */}
                        <TokenLogo symbol={a.symbol} chain={chainKey ? undefined : a.chain} size={30} />
                        <View style={{ flex: 1, gap: 2 }}>
                          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
                            <AppText weight="semibold" size={14}>{a.symbol}</AppText>
                            {!chainKey ? <AppText variant="caption">on {name(a.chain)}</AppText> : null}
                            {!a.verified ? <Pill small label="Unverified" /> : null}
                          </View>
                          {a.kind === 'native' ? (
                            <AppText variant="caption">{name(a.chain)} native coin</AppText>
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
          ) : null}
        </>
      )}
    </Screen>
  );
}
