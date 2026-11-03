import * as Clipboard from 'expo-clipboard';
import { useState } from 'react';
import { View } from 'react-native';

import { AppText, Mono } from '@/components/ui/AppText';
import { Banner, Choice, Segmented } from '@/components/ui/Blocks';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Fields';
import { Icon } from '@/components/ui/Icon';
import { useSecureScreen } from '@/hooks/useAppLock';
import { checkPhrase, checkPrivateKey, normalizePhrase, type KeyNamespace } from '@/services/keys';
import { colors } from '@/theme';
import { shortAddress } from '@/utils/format';

export type ImportResult = { kind: 'phrase'; phrase: string } | { kind: 'key'; namespace: KeyNamespace; secret: string };

/**
 * Recovery phrase or single private key. `allowPhrase` is false when adding an
 * account to a wallet that already has one (one phrase per phone).
 */
export function ImportForm({ allowPhrase = true, submitLabel = 'Import', busy, onSubmit }: { allowPhrase?: boolean; submitLabel?: string; busy?: boolean; onSubmit: (r: ImportResult) => void }) {
  useSecureScreen('import');
  const [method, setMethod] = useState<'phrase' | 'key'>(allowPhrase ? 'phrase' : 'key');
  const [phrase, setPhrase] = useState('');
  const [ns, setNs] = useState<KeyNamespace>('evm');
  const [key, setKey] = useState('');
  const [tried, setTried] = useState(false);

  const words = normalizePhrase(phrase).split(' ').filter(Boolean).length;
  const phraseCheck = checkPhrase(phrase);
  // Only nag once the count is plausible or after a submit attempt.
  const phraseError = !phraseCheck.ok && (tried || words === 12 || words === 24) ? phraseCheck.message : null;

  const keyCheck = key.trim() ? checkPrivateKey(ns, key) : null;

  const submit = () => {
    setTried(true);
    if (method === 'phrase' && phraseCheck.ok) onSubmit({ kind: 'phrase', phrase: phraseCheck.phrase });
    if (method === 'key' && keyCheck?.ok) onSubmit({ kind: 'key', namespace: ns, secret: keyCheck.secret });
  };

  return (
    <View style={{ gap: 20, flexGrow: 1 }}>
      {allowPhrase ? (
        <Segmented
          label="Import method"
          value={method}
          onChange={setMethod}
          options={[
            { value: 'phrase', label: 'Recovery phrase' },
            { value: 'key', label: 'Private key' },
          ]}
        />
      ) : null}

      {method === 'phrase' ? (
        <View style={{ gap: 14 }}>
          <AppText variant="lead">Restores every account from that phrase: Ethereum, Base and Solana addresses come from the same words.</AppText>
          <TextField
            label="12 or 24 words, separated by spaces"
            value={phrase}
            onChangeText={setPhrase}
            placeholder="word1 word2 word3 …"
            multiline
            mono
            autoComplete="off"
            importantForAutofill="no"
            style={{ minHeight: 150 }}
            error={phraseError}
            valid={phraseCheck.ok}
          />
          <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' }}>
            <AppText variant="small">
              {words} / {words > 12 ? 24 : 12} words
            </AppText>
            <Button
              label="Paste"
              variant="secondary"
              size="sm"
              onPress={async () => {
                const text = await Clipboard.getStringAsync();
                if (text) setPhrase(normalizePhrase(text));
                // Don't leave the phrase sitting on the clipboard.
                await Clipboard.setStringAsync('');
              }}
            />
          </View>
        </View>
      ) : (
        <View style={{ gap: 14 }}>
          <AppText variant="lead">Imports a single account on one network. It can’t be restored from a recovery phrase later, so keep the key backed up.</AppText>
          <AppText variant="label">Network</AppText>
          <View accessibilityRole="radiogroup" accessibilityLabel="Network" style={{ flexDirection: 'row', gap: 8 }}>
            <Choice label="EVM" selected={ns === 'evm'} onPress={() => setNs('evm')} flex />
            <Choice label="Solana" selected={ns === 'solana'} onPress={() => setNs('solana')} flex />
          </View>
          <TextField
            label="Private key"
            value={key}
            onChangeText={setKey}
            placeholder={ns === 'evm' ? '0x followed by 64 hex characters' : 'Base58 secret key'}
            secureTextEntry
            mono
            autoComplete="off"
            importantForAutofill="no"
            textContentType="none"
            error={keyCheck && !keyCheck.ok && (tried || key.trim().length >= 40) ? keyCheck.message : null}
            valid={!!keyCheck?.ok}
          />
          {keyCheck?.ok ? (
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 10, padding: 12, borderRadius: 12, backgroundColor: colors.successSoft }}>
              <Icon name="check" size={18} color={colors.successText} />
              <AppText size={14} color={colors.successText}>
                Valid key for <Mono color={colors.successText}>{shortAddress(keyCheck.address)}</Mono>
              </AppText>
            </View>
          ) : null}
        </View>
      )}

      <View style={{ marginTop: 'auto', gap: 12 }}>
        {tried && method === 'key' && !keyCheck ? <Banner tone="danger">Paste the private key first.</Banner> : null}
        <View style={{ flexDirection: 'row', gap: 8, alignItems: 'flex-start' }}>
          <Icon name="shield" size={16} color={colors.successStrong} />
          <AppText variant="small" style={{ flex: 1 }}>
            Encrypted on this device and never sent to MAX.
          </AppText>
        </View>
        <Button label={submitLabel} loading={busy} disabled={method === 'phrase' ? words === 0 : !key.trim()} onPress={submit} />
      </View>
    </View>
  );
}
