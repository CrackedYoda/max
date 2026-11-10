import { router } from 'expo-router';
import * as Clipboard from 'expo-clipboard';
import { ScrollView, Share, View } from 'react-native';
import QRCode from 'react-native-qrcode-svg';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText, Handle, Mono } from '@/components/ui/AppText';
import { Banner, List, ListRow, Loading, SectionTitle } from '@/components/ui/Blocks';
import { Button, LinkButton } from '@/components/ui/Button';
import { CopyButton } from '@/components/ui/Fields';
import { MaxStatusBanner } from '@/components/wallet/MaxStatusBanner';
import { getEnv } from '@/config/env';
import { errorMessage } from '@/error/messages';
import { useMe, useMyIds } from '@/hooks/queries';
import { toast } from '@/store/flows';
import { useSession } from '@/store/session';
import { colors, spacing } from '@/theme';
import { namespaceLabel, shortAddress } from '@/utils/format';

const RECEIVE_ONLY: string[] = ['bitcoin', 'litecoin', 'tron'];

export default function Receive() {
  const insets = useSafeAreaInsets();
  const ids = useMyIds();
  const accounts = useSession((s) => s.accounts);
  const maxReady = useSession((s) => s.max === 'ready');
  const me = useMe();
  const primaryId = me.data?.primaryMaxId?.id;
  const id = (primaryId ? ids.data?.find((x) => x.id === primaryId) : undefined) ?? ids.data?.[0];
  const link = id ? `${getEnv().webUrl}/${id.handle}` : null;

  // With a handle: where payments by name land. Without: this phone's own addresses.
  const rows = id
    ? id.destinations.map((d) => ({
        key: d.namespace,
        label: d.namespace === 'evm' ? 'EVM chains' : namespaceLabel(d.namespace),
        address: d.address,
        pasted: d.source === 'pasted',
        receiveOnly: RECEIVE_ONLY.includes(d.namespace),
      }))
    : [
        ...(accounts.find((a) => a.evm)?.evm ? [{ key: 'evm', label: 'EVM chains', address: accounts.find((a) => a.evm)!.evm!, pasted: false, receiveOnly: false }] : []),
        ...(accounts.find((a) => a.solana)?.solana ? [{ key: 'solana', label: 'Solana', address: accounts.find((a) => a.solana)!.solana!, pasted: false, receiveOnly: false }] : []),
      ];

  return (
    <ScrollView style={{ flex: 1, backgroundColor: colors.ivory }} contentContainerStyle={{ paddingTop: insets.top + 16, paddingHorizontal: spacing.gutter, paddingBottom: 24, gap: 22 }}>
      <AppText variant="h2" accessibilityRole="header">
        Receive
      </AppText>
      <MaxStatusBanner />

      <View accessibilityLabel="Your handle" style={{ backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, borderRadius: 24, padding: 24, alignItems: 'center', gap: 18 }}>
        {ids.isLoading && maxReady ? (
          <Loading />
        ) : id && link ? (
          <>
            <View accessible accessibilityRole="image" accessibilityLabel={`QR code linking to ${id.handle}`} style={{ padding: 14, borderRadius: 16, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line }}>
              <QRCode value={link} size={168} color={colors.ink} backgroundColor={colors.surface} ecl="M" />
            </View>
            <Handle name={id.handle} size={28} />
            <View style={{ flexDirection: 'row', gap: 8, alignSelf: 'stretch' }}>
              <Button
                style={{ flex: 1 }}
                size="md"
                variant="secondary"
                icon="copy"
                label="Copy"
                onPress={async () => {
                  await Clipboard.setStringAsync(id.handle);
                  toast(`${id.handle} copied`);
                }}
              />
              <Button style={{ flex: 1 }} size="md" icon="share" label="Share" onPress={() => Share.share({ message: `Pay me at ${id.handle} — ${link}` })} />
            </View>
            <AppText variant="small" style={{ textAlign: 'center' }}>
              Senders only need your handle. You don’t need to do anything to receive.
            </AppText>
          </>
        ) : (
          <>
            <AppText variant="lead" style={{ textAlign: 'center' }}>
              Claim a name so people can pay you without long addresses.
            </AppText>
            <Button label="Claim your name" disabled={!maxReady} onPress={() => router.push('/claim')} style={{ alignSelf: 'stretch' }} />
          </>
        )}
      </View>
      {ids.error ? <Banner tone="danger">{errorMessage(ids.error)}</Banner> : null}

      <View style={{ gap: 10 }}>
        <SectionTitle>{id ? 'Where payments land' : 'Your addresses'}</SectionTitle>
        <List>
          {rows.map((r, i) => (
            <ListRow
              key={r.key}
              title={[r.label, r.pasted ? 'Unverified' : null, r.receiveOnly ? 'Receive-only' : null].filter(Boolean).join(' · ')}
              subtitle={<Mono size={13} color={colors.muted}>{shortAddress(r.address)}</Mono>}
              right={<CopyButton value={r.address} label={`Copy ${r.label} address`} what="Address copied" />}
              last={!id && i === rows.length - 1}
            />
          ))}
          {id ? (
            <View style={{ paddingHorizontal: 8, paddingVertical: 4 }}>
              <LinkButton label="Change destinations" weight="semibold" size={14} color={colors.clayText} center={false} onPress={() => router.navigate('/ids')} />
            </View>
          ) : null}
        </List>
        {id && !id.destinations.length ? <Banner tone="clay">No destinations set: senders can’t pay this handle yet.</Banner> : null}
      </View>

      {rows.some((r) => r.receiveOnly) ? (
        <View style={{ gap: 10 }}>
          <SectionTitle>Receive-only addresses</SectionTitle>
          <AppText variant="small">MAX can’t send on these networks. Share the address or QR code with someone sending from their own wallet.</AppText>
          {rows
            .filter((r) => r.receiveOnly)
            .map((r) => (
              <View key={r.key} accessible accessibilityRole="image" accessibilityLabel={`QR code for your ${r.label} address`} style={{ backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, borderRadius: 20, padding: 18, alignItems: 'center', gap: 10 }}>
                <QRCode value={r.address} size={148} color={colors.ink} backgroundColor={colors.surface} ecl="M" />
                <AppText weight="semibold">{r.label}</AppText>
                <Mono size={12} color={colors.muted} selectable>{r.address}</Mono>
              </View>
            ))}
        </View>
      ) : null}
    </ScrollView>
  );
}
