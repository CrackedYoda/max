import * as Clipboard from 'expo-clipboard';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, View } from 'react-native';

import { AppText, Mono } from '@/components/ui/AppText';
import { Banner, Heading, List, ListRow } from '@/components/ui/Blocks';
import { Button } from '@/components/ui/Button';
import { Icon } from '@/components/ui/Icon';
import { Screen, TitleHeader } from '@/components/ui/Screen';
import { useSecureScreen } from '@/hooks/useAppLock';
import { useStepUpGate } from '@/hooks/useStepUpGate';
import { exportSecret } from '@/services/keys';
import * as vault from '@/services/vault';
import { toast } from '@/store/flows';
import { colors } from '@/theme';
import { shortAddress } from '@/utils/format';

const CLIPBOARD_CLEAR_MS = 60_000;

export default function ExportKey() {
  useSecureScreen('export-key');
  const allowed = useStepUpGate('Confirm it’s you to export a private key.');
  const [selected, setSelected] = useState<string | null>(null);
  const [shown, setShown] = useState(false);

  // A revoked gate (lock, blur) never reopens on this mount, so no key or secret derives from here on.
  // The vault can unload a render before the gate closes: never ask a locked vault for keys.
  const keys = allowed && vault.isLoaded() ? vault.allKeys() : [];
  const key = keys.find((k) => `${k.namespace}:${k.address}` === selected);
  const secret = key && shown ? exportSecret(key) : null;

  return (
    <Screen scroll header={<TitleHeader title="Export private key" onBack={() => router.back()} />} footer={<Button label="Done" variant="dark" onPress={() => router.back()} />}>
      <Heading title="Export a key" lead="For moving one account into another wallet app. Anyone with the key controls that account." />
      <List>
        {keys.map((k, i) => {
          const id = `${k.namespace}:${k.address}`;
          return (
            <ListRow
              key={id}
              title={k.namespace === 'evm' ? 'EVM' : 'Solana'}
              subtitle={<Mono size={12} color={colors.muted}>{shortAddress(k.address)}</Mono>}
              right={selected === id ? <Icon name="check" size={18} color={colors.clayText} /> : null}
              last={i === keys.length - 1}
              onPress={() => {
                setSelected(id);
                setShown(false);
              }}
            />
          );
        })}
      </List>
      {key ? (
        <View style={{ gap: 12 }}>
          <Banner tone="danger" title="Never share this key">
            MAX will never ask for it. Anyone who has it can move your funds.
          </Banner>
          <Pressable
            accessibilityRole="button"
            accessibilityLabel={shown ? 'Private key' : 'Tap to reveal the private key'}
            onPress={() => setShown(true)}
            style={{ padding: 14, borderRadius: 14, borderWidth: 1, borderColor: colors.line, backgroundColor: colors.surface, minHeight: 72, justifyContent: 'center' }}>
            {secret ? (
              <Mono size={13} selectable>
                {secret}
              </Mono>
            ) : (
              <AppText weight="semibold" size={14} style={{ textAlign: 'center' }}>
                Tap to reveal
              </AppText>
            )}
          </Pressable>
          {secret ? (
            <Button
              label="Copy (clears in 60s)"
              variant="secondary"
              size="md"
              icon="copy"
              onPress={async () => {
                await Clipboard.setStringAsync(secret);
                toast('Copied. Clipboard clears in 60 seconds.');
                // Reading the clipboard back would trigger iOS's paste prompt, so clear unconditionally.
                setTimeout(() => void Clipboard.setStringAsync(''), CLIPBOARD_CLEAR_MS);
              }}
            />
          ) : null}
        </View>
      ) : null}
    </Screen>
  );
}
