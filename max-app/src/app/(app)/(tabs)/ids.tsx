import type { MaxId } from '@max/sdk';
import { useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { Alert, Pressable, ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText, Handle, Mono } from '@/components/ui/AppText';
import { Banner, List, ListRow, Loading, Pill, SectionTitle } from '@/components/ui/Blocks';
import { Button, LinkButton } from '@/components/ui/Button';
import { ChainLogo } from '@/components/ui/ChainLogo';
import { Icon } from '@/components/ui/Icon';
import { MaxStatusBanner } from '@/components/wallet/MaxStatusBanner';
import { errorMessage } from '@/error/messages';
import { useMyIds } from '@/hooks/queries';
import { lock, refreshAccounts, removeWallet, stepUp } from '@/services/session';
import * as vault from '@/services/vault';
import { toast } from '@/store/flows';
import { useSession } from '@/store/session';
import { colors, spacing } from '@/theme';
import { DESTINATION_NAMESPACES, namespaceLabel, shortAddress } from '@/utils/format';

export default function Ids() {
  const insets = useSafeAreaInsets();
  const qc = useQueryClient();
  const ids = useMyIds();
  const accounts = useSession((s) => s.accounts);
  const maxStatus = useSession((s) => s.max);
  const bioLabel = useSession((s) => s.biometricLabel);
  const hasPhrase = accounts.some((a) => a.source === 'phrase');

  function manage(id: string) {
    const a = accounts.find((x) => x.id === id);
    if (!a) return;
    if (a.source === 'phrase') {
      Alert.alert(a.label, 'Comes from your recovery phrase. It can only be removed together with the whole wallet.');
      return;
    }
    Alert.alert(a.label, 'Remove this imported account from this phone? Its funds stay on-chain; you’ll need the private key to use it again.', [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Remove',
        style: 'destructive',
        onPress: async () => {
          if (!(await stepUp('Confirm removing this account from your phone.'))) return;
          try {
            await vault.removeImported(id);
            await refreshAccounts();
            toast('Account removed from this phone');
          } catch (err) {
            Alert.alert('Couldn’t remove it', errorMessage(err));
          }
        },
      },
    ]);
  }

  function confirmRemoveWallet() {
    Alert.alert(
      'Remove wallet from this phone?',
      hasPhrase
        ? 'Your funds stay on-chain, but only your recovery phrase can restore them. MAX can’t recover it for you.'
        : 'Your funds stay on-chain, but only your private keys can restore them. MAX can’t recover them for you.',
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Remove wallet',
          style: 'destructive',
          onPress: async () => {
            if (!(await stepUp('Confirm removing the wallet from this phone.'))) return;
            await removeWallet(qc);
          },
        },
      ],
    );
  }

  return (
    <ScrollView style={{ flex: 1, backgroundColor: colors.ivory }} contentContainerStyle={{ paddingTop: insets.top + 16, paddingHorizontal: spacing.gutter, paddingBottom: 24, gap: 22 }}>
      <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
        <AppText variant="h2" accessibilityRole="header">
          Wallet & IDs
        </AppText>
        <Button label="+ Claim" variant="soft" size="sm" disabled={maxStatus !== 'ready'} onPress={() => router.push('/claim')} />
      </View>

      <MaxStatusBanner />
      {ids.error ? <Banner tone="danger">{errorMessage(ids.error)}</Banner> : null}
      {maxStatus === 'ready' && ids.isLoading ? <Loading label="Loading your IDs" /> : null}
      {ids.data?.map((id) => <IdCard key={id.id} id={id} />)}
      {maxStatus === 'ready' && ids.data && !ids.data.some((i) => !i.isDefault) ? (
        <View style={{ backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, borderRadius: 20, padding: 18, gap: 10 }}>
          <AppText weight="semibold" size={16}>
            No claimed MAX ID yet
          </AppText>
          <AppText variant="small">Claim a handle so people can pay you by name.</AppText>
          <Button label="Claim your MAX ID" size="md" onPress={() => router.push('/claim')} />
        </View>
      ) : null}

      <View style={{ gap: 10 }}>
        <SectionTitle>Accounts on this device</SectionTitle>
        <List>
          {accounts.map((a) => (
            <ListRow
              key={a.id}
              title={
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
                  <AppText weight="semibold" size={14}>
                    {a.label}
                  </AppText>
                  <Pill small tone={a.source === 'phrase' ? 'neutral' : 'clay'} label={a.source === 'phrase' ? 'Recovery phrase' : `Private key · ${a.evm ? 'EVM' : 'Solana'}`} />
                </View>
              }
              subtitle={
                <Mono size={12} color={colors.muted}>
                  {[a.evm, a.solana].filter(Boolean).map((x) => shortAddress(x!)).join(' · ')}
                </Mono>
              }
              right={<LinkButton label="Manage" weight="semibold" size={13} onPress={() => manage(a.id)} />}
            />
          ))}
          <View style={{ paddingHorizontal: 8, paddingVertical: 4 }}>
            <LinkButton label="+ Add or import account" weight="semibold" size={14} color={colors.clayText} center={false} onPress={() => router.push('/add-account')} />
          </View>
        </List>
      </View>

      <View style={{ gap: 10 }}>
        <SectionTitle>Security</SectionTitle>
        <List>
          {hasPhrase ? (
            <ListRow
              title="Reveal recovery phrase"
              chevron
              onPress={() => router.push('/reveal')}
            />
          ) : null}
          <ListRow title="Export private key" chevron onPress={() => router.push('/export-key')} />
          <ListRow title={`Passcode & ${bioLabel}`} chevron onPress={() => router.push('/security')} />
          <ListRow title="Reserved names & X" chevron onPress={() => router.push('/verify')} />
          <ListRow title="Remove wallet from this phone" danger last onPress={confirmRemoveWallet} />
        </List>
      </View>

      <Button label="Lock wallet" variant="secondary" size="md" icon="lock" onPress={lock} />
    </ScrollView>
  );
}

function IdCard({ id }: { id: MaxId }) {
  const dest = (ns: string) => id.destinations.find((d) => d.namespace === ns && !d.chain);
  const overrides = id.destinations.filter((d) => d.chain).length;
  return (
    <View style={{ backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, borderRadius: 20, padding: 18, gap: 14 }}>
      <View style={{ gap: 10 }}>
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
          <Handle name={id.handle} size={20} />
          <Pill small tone={id.type === 'agent' ? 'clay' : 'neutral'} label={id.type === 'agent' ? 'Agent' : 'Human'} />
          {id.isDefault ? <Pill small tone="clay" label="Default ID" /> : null}
        </View>
        <View style={{ flexDirection: 'row', gap: 6, flexWrap: 'wrap' }}>
          <Button label="Edit" variant="secondary" size="sm" onPress={() => router.push({ pathname: '/edit-id', params: { id: id.id } })} />
          {id.isDefault ? null : (
            <>
              <Button label="Rename" variant="secondary" size="sm" onPress={() => router.push({ pathname: '/rename', params: { id: id.id } })} />
              <Button label="Sell" variant="soft" size="sm" onPress={() => router.push({ pathname: '/sell', params: { id: id.id } })} />
            </>
          )}
        </View>
      </View>
      <View style={{ borderTopWidth: 1, borderTopColor: colors.sunken }}>
        {DESTINATION_NAMESPACES.map((ns, i) => {
          const d = dest(ns);
          return (
            <View key={ns} style={{ borderBottomWidth: i < DESTINATION_NAMESPACES.length - 1 ? 1 : 0, borderBottomColor: colors.sunken }}>
            <Pressable
              key={ns}
              accessibilityRole="button"
              accessibilityLabel={`${namespaceLabel(ns)} destination: ${d ? d.address : 'not set'}${d?.source === 'pasted' ? ', unverified' : ''}. Change`}
              onPress={() => router.push({ pathname: '/destination', params: { id: id.id, ns } })}
              style={({ pressed }) => ({ flexDirection: 'row', alignItems: 'center', gap: 10, minHeight: 48, opacity: pressed ? 0.6 : 1 })}>
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8, width: 92 }}>
                <ChainLogo chain={ns} />
                <AppText weight="semibold" size={12} color={colors.muted}>
                  {namespaceLabel(ns)}
                </AppText>
              </View>
              <View style={{ flex: 1 }}>
                {d ? (
                  <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                    <Mono size={13}>{shortAddress(d.address)}</Mono>
                    {d.source === 'pasted' ? <Pill small label="Unverified" /> : null}
                  </View>
                ) : (
                  <AppText weight="semibold" size={13} color={colors.clayText}>
                    Set destination
                  </AppText>
                )}
              </View>
              <Icon name="chevronDown" size={18} color={colors.muted} />
            </Pressable>
            {ns === 'evm' ? (
              <LinkButton
                label={overrides ? `${overrides} ${overrides === 1 ? 'chain' : 'chains'} customized` : 'Customize per chain'}
                weight="medium"
                size={13}
                color={colors.clayText}
                center={false}
                onPress={() => router.push({ pathname: '/evm-chains', params: { id: id.id } })}
              />
            ) : null}
            </View>
          );
        })}
      </View>
    </View>
  );
}
