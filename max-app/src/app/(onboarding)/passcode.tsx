import { Redirect, router } from 'expo-router';
import { useEffect, useRef, useState } from 'react';
import { Switch, View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Banner, Heading } from '@/components/ui/Blocks';
import { Button } from '@/components/ui/Button';
import { Keypad, PasscodeDots } from '@/components/ui/Fields';
import { usePinInput } from '@/hooks/usePinInput';
import { Screen, StepHeader } from '@/components/ui/Screen';
import { errorMessage } from '@/error/messages';
import { biometricAvailable, biometricLabel, PASSCODE_LENGTH } from '@/services/passcode';
import { finishOnboarding, type NewWallet } from '@/services/session';
import { useOnboarding } from '@/store/flows';
import { colors } from '@/theme';

export default function Passcode() {
  const phrase = useOnboarding((s) => s.phrase);
  const imported = useOnboarding((s) => s.imported);
  const [first, setFirst] = useState<string | null>(null);
  const [mismatch, setMismatch] = useState(false);
  const firstRef = useRef<string | null>(null);
  const { code, press, set: setCode } = usePinInput(PASSCODE_LENGTH);
  const [bioAvailable, setBioAvailable] = useState(false);
  const [bioLabel, setBioLabel] = useState('Face ID');
  const [bio, setBio] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    biometricAvailable().then(setBioAvailable).catch(() => {});
    biometricLabel().then(setBioLabel).catch(() => {});
  }, []);

  const wallet: NewWallet | null = imported
    ? imported.kind === 'phrase'
      ? { mnemonic: imported.phrase }
      : { namespace: imported.namespace, secret: imported.secret }
    : phrase
      ? { mnemonic: phrase }
      : null;
  if (!wallet) return <Redirect href="/welcome" />;

  const confirming = first !== null;
  const matched = confirming && code.length === PASSCODE_LENGTH && code === first;

  function onKey(k: string) {
    if (busy) return;
    setError(null);
    setMismatch(false);
    const full = press(k);
    if (!full) return;
    if (firstRef.current === null) {
      setTimeout(() => {
        firstRef.current = full;
        setFirst(full);
        setCode('');
      }, 150);
    } else if (full !== firstRef.current) {
      setMismatch(true);
      setTimeout(() => {
        firstRef.current = null;
        setFirst(null);
        setCode('');
      }, 600);
    }
  }

  async function finish() {
    if (!matched || !wallet) return;
    setBusy(true);
    try {
      await finishOnboarding(wallet, code, bioAvailable && bio);
      // The root guard swaps onboarding for the wallet; nothing to navigate here.
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  }

  const total = imported ? 2 : 3;
  return (
    <Screen
      header={<StepHeader step={total} total={total} onBack={busy ? undefined : () => router.back()} />}
      footer={<Button label={busy ? 'Setting up your wallet…' : 'Continue'} disabled={!matched} loading={busy} onPress={finish} />}>
      <Heading center title={confirming ? 'Confirm passcode' : 'Set a passcode'} lead={confirming ? 'Enter the same 6 digits again.' : 'Unlocks MAX and approves payments on this phone.'} />
      <PasscodeDots length={PASSCODE_LENGTH} filled={code.length} error={mismatch} />
      {mismatch ? (
        <AppText accessibilityRole="alert" size={13} color={colors.danger} style={{ textAlign: 'center' }}>
          Those didn’t match. Start again.
        </AppText>
      ) : null}
      {bioAvailable ? (
        <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 12, paddingVertical: 12, paddingHorizontal: 16, borderWidth: 1, borderColor: colors.line, borderRadius: 16, backgroundColor: colors.surface }}>
          <View style={{ flex: 1, gap: 2 }}>
            <AppText weight="semibold" size={15}>
              Use {bioLabel}
            </AppText>
            <AppText variant="small">Passcode still works as a fallback</AppText>
          </View>
          <Switch value={bio} onValueChange={setBio} trackColor={{ true: colors.clay, false: colors.line }} accessibilityLabel={`Use ${bioLabel}`} />
        </View>
      ) : null}
      {error ? <Banner tone="danger" title="Couldn’t create the wallet">{error}</Banner> : null}
      <View style={{ marginTop: 'auto' }}>
        <Keypad onKey={onKey} />
      </View>
    </Screen>
  );
}
