import { looksLikeAddress, type DestinationTarget, type Namespace } from '@max/sdk';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText, Handle, Mono } from '@/components/ui/AppText';
import { Banner, Loading } from '@/components/ui/Blocks';
import { Button, IconButton } from '@/components/ui/Button';
import { CheckRow, TextField } from '@/components/ui/Fields';
import { ChainLogo } from '@/components/ui/ChainLogo';
import { Icon } from '@/components/ui/Icon';
import { errorMessage } from '@/error/messages';
import { useChains, useMyIds, useRemoveChainDestination, useSetChainDestination, useSetDestination, useWallets } from '@/hooks/queries';
import { toast } from '@/store/flows';
import { useSession } from '@/store/session';
import { colors, spacing } from '@/theme';
import { formatDate, namespaceLabel } from '@/utils/format';

const LINKABLE: Namespace[] = ['evm', 'solana'];
const EVM_WARNING = 'This address will receive on every EVM chain you haven’t customized. Exchange deposit addresses often accept only one chain.';

type Submit = (target: DestinationTarget, onSuccess: () => void) => void;

/**
 * Bottom sheet: pick a linked wallet, or paste an address, to receive on a
 * namespace, or (with `chain`) on one EVM chain instead of the EVM default.
 */
export default function Destination() {
  const { id, ns, chain } = useLocalSearchParams<{ id: string; ns: Namespace; chain?: string }>();
  const chainInfo = useChains().data?.find((c) => c.key === chain);
  const setChain = useSetChainDestination();
  const removeChain = useRemoveChainDestination();
  const insets = useSafeAreaInsets();
  const ids = useMyIds();
  const wallets = useWallets();
  const accounts = useSession((s) => s.accounts);
  const setDest = useSetDestination();
  const maxId = ids.data?.find((x) => x.id === id);
  const current = maxId?.destinations.find((d) => (chain ? d.chain === chain : d.namespace === ns && !d.chain));
  const label = chain ? (chainInfo?.name ?? chain) : namespaceLabel(ns ?? '');
  const pending = setDest.isPending || setChain.isPending || removeChain.isPending;
  const error = setDest.error ?? setChain.error ?? removeChain.error;
  const submit: Submit = (target, onSuccess) =>
    chain ? setChain.mutate({ id: id!, chain, target }, { onSuccess }) : setDest.mutate({ id: id!, namespace: ns!, target }, { onSuccess });
  const options = (wallets.data ?? []).filter((w) => w.namespace === ns);
  const linkable = LINKABLE.includes(ns!);
  const [pasting, setPasting] = useState(!linkable);
  const onPhone = (address: string) => accounts.some((a) => (ns === 'evm' ? a.evm?.toLowerCase() === address.toLowerCase() : a.solana === address));

  return (
    <View style={{ flex: 1, backgroundColor: colors.scrim }}>
      <Pressable style={{ flex: 1 }} accessibilityLabel="Close" onPress={() => router.back()} />
      <View accessibilityViewIsModal style={{ backgroundColor: colors.surface, borderTopLeftRadius: 28, borderTopRightRadius: 28, paddingTop: 12, paddingHorizontal: spacing.gutter, paddingBottom: insets.bottom + 16, gap: 16 }}>
        <View style={{ alignSelf: 'center', width: 40, height: 5, borderRadius: 999, backgroundColor: colors.line }} />
        <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginRight: -10 }}>
          <View style={{ flex: 1, gap: 2 }}>
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 10 }}>
              <ChainLogo chain={chain ?? ns ?? ''} size={24} />
              <AppText variant="title" accessibilityRole="header">
                {label} destination
              </AppText>
            </View>
            {maxId ? <Handle name={maxId.handle} size={13} /> : null}
          </View>
          <IconButton icon="close" label="Close" color={colors.muted} onPress={() => router.back()} />
        </View>
        <AppText variant="small">Payments to this handle on {chain ? label : ns === 'evm' ? 'every EVM chain you haven’t customized' : label} go to the address you pick. Payments already in flight keep their original address.</AppText>
        {chain && current ? (
          <Button
            label="Use EVM default"
            variant="secondary"
            size="sm"
            loading={removeChain.isPending}
            onPress={() => removeChain.mutate({ id: id!, chain }, { onSuccess: () => { toast(`${label} uses your EVM default`); router.back(); } })}
          />
        ) : null}
        {pasting ? (
          <PasteForm ns={ns!} label={label} perChain={!!chain} submit={submit} pending={pending} error={error} onBack={linkable ? () => setPasting(false) : undefined} />
        ) : (
        <>
        {wallets.isLoading ? <Loading /> : null}
        {!wallets.isLoading && !options.length ? <Banner tone="clay">No {namespaceLabel(ns ?? '')} wallet is linked to your account yet.</Banner> : null}
        <View style={{ gap: 8 }}>
          {options.map((w) => {
            const on = current?.walletId === w.id;
            return (
              <Pressable
                key={w.id}
                accessibilityRole="radio"
                accessibilityState={{ checked: on, disabled: pending }}
                disabled={pending}
                onPress={() =>
                  on
                    ? router.back()
                    : submit({ walletId: w.id }, () => {
                        toast(`${label} destination updated`);
                        router.back();
                      })
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
        <Button label="Paste an address" variant="secondary" size="sm" onPress={() => setPasting(true)} />
        {error ? <Banner tone="danger">{errorMessage(error)}</Banner> : null}
        </>
        )}
      </View>
    </View>
  );
}

/** Paste an address, review it in full, and confirm control before saving. */
function PasteForm({ ns, label, perChain, submit, pending, error, onBack }: {
  ns: Namespace;
  label: string;
  perChain: boolean;
  submit: Submit;
  pending: boolean;
  error: unknown;
  onBack?: () => void;
}) {
  const [address, setAddress] = useState('');
  const [reviewing, setReviewing] = useState(false);
  const [confirmed, setConfirmed] = useState(false);
  const trimmed = address.trim();
  const shapeOk = looksLikeAddress(ns, trimmed);

  if (!reviewing) {
    return (
      <View style={{ gap: 12 }}>
        <TextField
          label={`${label} receive address`}
          mono
          value={address}
          onChangeText={setAddress}
          valid={shapeOk}
          error={trimmed !== '' && !shapeOk ? `That doesn’t look like a ${label} address.` : undefined}
          hint="Paste the address exactly as your wallet or exchange shows it."
        />
        <Button label="Review" size="sm" disabled={!shapeOk} onPress={() => setReviewing(true)} />
        {onBack ? <Button label="Back" variant="secondary" size="sm" onPress={onBack} /> : null}
      </View>
    );
  }
  return (
    <View style={{ gap: 12 }}>
      <AppText weight="semibold">Check this {label} address</AppText>
      <View style={{ backgroundColor: colors.sunken, borderRadius: 12, padding: 12 }}>
        <Mono size={13} selectable>{trimmed}</Mono>
      </View>
      {ns === 'evm' && !perChain ? <AppText variant="small">{EVM_WARNING}</AppText> : null}
      <CheckRow checked={confirmed} onChange={setConfirmed}>I control this address. Payments sent to it can’t be reversed.</CheckRow>
      <Button
        label="Save"
        size="sm"
        disabled={!confirmed}
        loading={pending}
        onPress={() =>
          submit({ address: trimmed, confirmed: true }, () => {
            toast(`${label} destination updated`);
            router.back();
          })
        }
      />
      <Button label="Edit" variant="secondary" size="sm" onPress={() => setReviewing(false)} />
      {error ? <Banner tone="danger">{errorMessage(error)}</Banner> : null}
    </View>
  );
}
