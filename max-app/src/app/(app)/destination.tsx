import { router, useLocalSearchParams } from 'expo-router';
import { Pressable, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText, Handle, Mono } from '@/components/ui/AppText';
import { Banner, Loading } from '@/components/ui/Blocks';
import { IconButton } from '@/components/ui/Button';
import { Icon } from '@/components/ui/Icon';
import { errorMessage } from '@/error/messages';
import { useMyIds, useSetDestination, useWallets } from '@/hooks/queries';
import { toast } from '@/store/flows';
import { useSession } from '@/store/session';
import { colors, spacing } from '@/theme';
import { formatDate, namespaceLabel } from '@/utils/format';

/** Bottom sheet: pick which linked wallet receives on a namespace. */
export default function Destination() {
  const { id, ns } = useLocalSearchParams<{ id: string; ns: 'evm' | 'solana' }>();
  const insets = useSafeAreaInsets();
  const ids = useMyIds();
  const wallets = useWallets();
  const accounts = useSession((s) => s.accounts);
  const setDest = useSetDestination();
  const maxId = ids.data?.find((x) => x.id === id);
  const current = maxId?.destinations.find((d) => d.namespace === ns);
  const options = (wallets.data ?? []).filter((w) => w.namespace === ns);
  const onPhone = (address: string) => accounts.some((a) => (ns === 'evm' ? a.evm?.toLowerCase() === address.toLowerCase() : a.solana === address));

  return (
    <View style={{ flex: 1, backgroundColor: colors.scrim }}>
      <Pressable style={{ flex: 1 }} accessibilityLabel="Close" onPress={() => router.back()} />
      <View accessibilityViewIsModal style={{ backgroundColor: colors.surface, borderTopLeftRadius: 28, borderTopRightRadius: 28, paddingTop: 12, paddingHorizontal: spacing.gutter, paddingBottom: insets.bottom + 16, gap: 16 }}>
        <View style={{ alignSelf: 'center', width: 40, height: 5, borderRadius: 999, backgroundColor: colors.line }} />
        <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginRight: -10 }}>
          <View style={{ flex: 1, gap: 2 }}>
            <AppText variant="title" accessibilityRole="header">
              {namespaceLabel(ns ?? '')} destination
            </AppText>
            {maxId ? <Handle name={maxId.handle} size={13} /> : null}
          </View>
          <IconButton icon="close" label="Close" color={colors.muted} onPress={() => router.back()} />
        </View>
        <AppText variant="small">Payments to this handle on {ns === 'evm' ? 'every EVM chain' : 'Solana'} go to the wallet you pick. Payments already in flight keep their original address.</AppText>
        {wallets.isLoading ? <Loading /> : null}
        {!wallets.isLoading && !options.length ? <Banner tone="clay">No {namespaceLabel(ns ?? '')} wallet is linked to your account yet.</Banner> : null}
        <View style={{ gap: 8 }}>
          {options.map((w) => {
            const on = current?.walletId === w.id;
            return (
              <Pressable
                key={w.id}
                accessibilityRole="radio"
                accessibilityState={{ checked: on, disabled: setDest.isPending }}
                disabled={setDest.isPending}
                onPress={() =>
                  on
                    ? router.back()
                    : setDest.mutate(
                        { id: id!, namespace: ns!, walletId: w.id },
                        {
                          onSuccess: () => {
                            toast(`${namespaceLabel(ns!)} destination updated`);
                            router.back();
                          },
                        },
                      )
                }
                style={{ flexDirection: 'row', alignItems: 'center', gap: 12, padding: 14, borderRadius: 14, borderWidth: on ? 2 : 1, borderColor: on ? colors.accent : colors.line, backgroundColor: on ? colors.claySoft : colors.surface }}>
                <View style={{ flex: 1, gap: 2 }}>
                  <Mono size={13}>{w.address}</Mono>
                  <AppText variant="caption">{onPhone(w.address) ? 'On this phone' : 'Linked elsewhere'} · since {formatDate(w.verifiedAt)}</AppText>
                </View>
                {on ? <Icon name="check" size={20} color={colors.clayText} /> : null}
              </Pressable>
            );
          })}
        </View>
        {setDest.error ? <Banner tone="danger">{errorMessage(setDest.error)}</Banner> : null}
      </View>
    </View>
  );
}
