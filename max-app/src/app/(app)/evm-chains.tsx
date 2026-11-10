import { router, useLocalSearchParams } from 'expo-router';
import { View } from 'react-native';

import { AppText, Mono } from '@/components/ui/AppText';
import { Banner, List, ListRow, Loading, Pill } from '@/components/ui/Blocks';
import { ChainLogo } from '@/components/ui/ChainLogo';
import { Screen, TitleHeader } from '@/components/ui/Screen';
import { errorMessage } from '@/error/messages';
import { useChains, useMyIds } from '@/hooks/queries';
import { colors } from '@/theme';
import { shortAddress } from '@/utils/format';

/** Every EVM chain for one MAX ID: uses the EVM default unless customized. */
export default function EvmChains() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const ids = useMyIds();
  const chains = useChains();
  const maxId = ids.data?.find((x) => x.id === id);
  const fallback = maxId?.destinations.find((d) => d.namespace === 'evm' && !d.chain);
  const evmChains = (chains.data ?? []).filter((c) => c.namespace === 'evm');

  return (
    <Screen scroll topPadding={0} header={<TitleHeader title="Customize EVM chains" icon="close" onBack={() => router.back()} />}>
      <AppText variant="small">
        Chains you don’t customize use your EVM default{fallback ? ` (${shortAddress(fallback.address)})` : ', which isn’t set yet'}.
      </AppText>
      {chains.isLoading || ids.isLoading ? <Loading /> : null}
      {chains.error ? <Banner tone="danger">{errorMessage(chains.error)}</Banner> : null}
      <List>
        {evmChains.map((c, i) => {
          const override = maxId?.destinations.find((d) => d.chain === c.key);
          return (
            <ListRow
              key={c.key}
              chevron
              last={i === evmChains.length - 1}
              accessibilityLabel={`${c.name}: ${override ? override.address : 'uses default'}. Change`}
              onPress={() => router.push({ pathname: '/destination', params: { id: id!, ns: 'evm', chain: c.key } })}
              title={
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: 10 }}>
                  <ChainLogo chain={c.key} size={20} />
                  <AppText weight="semibold" size={14}>{c.name}</AppText>
                  {c.testnet ? <Pill small label="Testnet" /> : null}
                </View>
              }
              subtitle={
                override ? (
                  <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
                    <Mono size={12} color={colors.muted}>{shortAddress(override.address)}</Mono>
                    {override.source === 'pasted' ? <Pill small label="Unverified" /> : null}
                  </View>
                ) : (
                  'Uses default'
                )
              }
            />
          );
        })}
      </List>
    </Screen>
  );
}
