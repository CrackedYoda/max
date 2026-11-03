import { router } from 'expo-router';
import { useEffect, useRef, useState } from 'react';
import { Switch, View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Banner, Heading, List, ListRow } from '@/components/ui/Blocks';
import { Button } from '@/components/ui/Button';
import { Keypad, PasscodeDots } from '@/components/ui/Fields';
import { usePinInput } from '@/hooks/usePinInput';
import { Screen, TitleHeader } from '@/components/ui/Screen';
import { INACTIVITY_LOCK_MS } from '@/hooks/useAppLock';
import { biometricAvailable, disableBiometric, enableBiometric, PASSCODE_LENGTH, setPasscode } from '@/services/passcode';
import { stepUp } from '@/services/session';
import { toast } from '@/store/flows';
import { useSession } from '@/store/session';
import { colors } from '@/theme';

export default function Security() {
  const bioEnabled = useSession((s) => s.biometricEnabled);
  const bioLabel = useSession((s) => s.biometricLabel);
  const setBiometric = useSession((s) => s.setBiometric);
  const [bioAvailable, setBioAvailable] = useState(false);
  const [changing, setChanging] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    biometricAvailable().then(setBioAvailable).catch(() => {});
  }, []);

  if (changing) return <ChangePasscode onDone={() => setChanging(false)} />;

  return (
    <Screen scroll header={<TitleHeader title={`Passcode & ${bioLabel}`} onBack={() => router.back()} />}>
      <List>
        <ListRow
          title={`Use ${bioLabel}`}
          subtitle={bioAvailable ? 'Unlock and approve payments. Passcode still works.' : `${bioLabel} isn’t set up on this phone.`}
          right={
            <Switch
              value={bioEnabled}
              disabled={!bioAvailable || busy}
              trackColor={{ true: colors.clay, false: colors.line }}
              accessibilityLabel={`Use ${bioLabel}`}
              onValueChange={async (on) => {
                setBusy(true);
                try {
                  if (on) {
                    if (!(await stepUp(`Enter your passcode to turn on ${bioLabel}.`))) return;
                    const ok = await enableBiometric();
                    setBiometric(ok);
                    if (ok) toast(`${bioLabel} is on`);
                  } else {
                    await disableBiometric();
                    setBiometric(false);
                  }
                } finally {
                  setBusy(false);
                }
              }}
            />
          }
        />
        <ListRow
          title="Change passcode"
          chevron
          last
          onPress={async () => {
            if (await stepUp('Enter your current passcode.')) setChanging(true);
          }}
        />
      </List>
      <AppText variant="small">
        MAX locks when you leave the app and after {INACTIVITY_LOCK_MS / 60_000} minutes without a tap. After 5 wrong passcodes it pauses for longer each time; it never erases your wallet.
      </AppText>
    </Screen>
  );
}

function ChangePasscode({ onDone }: { onDone: () => void }) {
  const [first, setFirst] = useState<string | null>(null);
  const [mismatch, setMismatch] = useState(false);
  const firstRef = useRef<string | null>(null);
  const { code, press, set: setCode } = usePinInput(PASSCODE_LENGTH);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const matched = first !== null && code.length === PASSCODE_LENGTH && code === first;

  function onKey(k: string) {
    if (busy) return;
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

  return (
    <Screen
      header={<TitleHeader title="Change passcode" icon="close" onBack={onDone} />}
      footer={
        <Button
          label="Save passcode"
          disabled={!matched}
          loading={busy}
          onPress={async () => {
            setBusy(true);
            try {
              await setPasscode(code);
              toast('Passcode changed');
              onDone();
            } catch {
              setError('Couldn’t save the new passcode. Your old one still works.');
              setBusy(false);
            }
          }}
        />
      }>
      <Heading center title={first === null ? 'New passcode' : 'Confirm passcode'} lead={first === null ? 'Choose 6 digits.' : 'Enter the same 6 digits again.'} />
      <PasscodeDots length={PASSCODE_LENGTH} filled={code.length} error={mismatch} />
      {mismatch ? (
        <AppText accessibilityRole="alert" size={13} color={colors.danger} style={{ textAlign: 'center' }}>
          Those didn’t match. Start again.
        </AppText>
      ) : null}
      {error ? <Banner tone="danger">{error}</Banner> : null}
      <View style={{ marginTop: 'auto' }}>
        <Keypad onKey={onKey} />
      </View>
    </Screen>
  );
}
