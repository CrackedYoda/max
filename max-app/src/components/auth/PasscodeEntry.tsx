import * as Haptics from 'expo-haptics';
import { useEffect, useRef, useState } from 'react';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { LinkButton } from '@/components/ui/Button';
import { Keypad, PasscodeDots } from '@/components/ui/Fields';
import { usePinInput } from '@/hooks/usePinInput';
import { PASSCODE_LENGTH, type VerifyResult, getLockout } from '@/services/passcode';
import { colors } from '@/theme';

/**
 * Shared passcode entry for unlock and step-up: dots, keypad, lockout countdown.
 * `verify` does the actual check (and any side effects on success).
 */
export function PasscodeEntry({
  verify,
  onBiometric,
  biometricLabel,
}: {
  verify: (code: string) => Promise<VerifyResult>;
  onBiometric?: () => void;
  biometricLabel?: string;
}) {
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState(false);
  const [lockedUntil, setLockedUntil] = useState(0);
  const [now, setNow] = useState(() => Date.now());
  const busy = useRef(false);

  useEffect(() => {
    getLockout()
      .then((l) => setLockedUntil(l.lockedUntil))
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (lockedUntil <= Date.now()) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [lockedUntil]);

  const { code, press, set: setCode } = usePinInput(PASSCODE_LENGTH);
  const locked = lockedUntil > now;
  const secs = Math.ceil((lockedUntil - now) / 1000);

  async function submit(full: string) {
    if (busy.current) return;
    busy.current = true;
    try {
      const res = await verify(full);
      if (res.ok) return;
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error).catch(() => {});
      setError(true);
      if (res.reason === 'locked') {
        setLockedUntil(res.retryAt);
        setNow(Date.now());
        setMessage(null);
      } else if (res.reason === 'wrong') {
        setMessage(res.attemptsBeforeLock <= 2 ? `Wrong passcode. ${res.attemptsBeforeLock} ${res.attemptsBeforeLock === 1 ? 'try' : 'tries'} before a short lockout.` : 'Wrong passcode. Try again.');
      } else {
        setMessage('No passcode is set on this phone.');
      }
      setTimeout(() => {
        setCode('');
        setError(false);
      }, 450);
    } finally {
      busy.current = false;
    }
  }

  function onKey(k: string) {
    if (locked || busy.current) return;
    setMessage(null);
    const full = press(k);
    if (full) void submit(full);
  }

  return (
    <View style={{ gap: 20, flex: 1 }}>
      <PasscodeDots length={PASSCODE_LENGTH} filled={code.length} error={error} />
      <AppText accessibilityLiveRegion="polite" size={13} weight="medium" color={locked || error ? colors.danger : colors.muted} style={{ textAlign: 'center', minHeight: 20 }}>
        {locked ? `Too many tries. Try again in ${secs >= 60 ? `${Math.ceil(secs / 60)} min` : `${secs}s`}.` : message ?? ''}
      </AppText>
      <View style={{ marginTop: 'auto', opacity: locked ? 0.4 : 1 }}>
        <Keypad onKey={onKey} />
      </View>
      {onBiometric ? <LinkButton label={`Use ${biometricLabel ?? 'Face ID'}`} onPress={onBiometric} weight="semibold" color={colors.clayText} /> : null}
    </View>
  );
}
