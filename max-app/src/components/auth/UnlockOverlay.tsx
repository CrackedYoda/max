import { useEffect, useRef } from 'react';
import { StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText } from '@/components/ui/AppText';
import { unlockWithBiometric, unlockWithPasscode } from '@/services/session';
import { useSession } from '@/store/session';
import { colors, spacing } from '@/theme';

import { PasscodeEntry } from './PasscodeEntry';

/**
 * Full-screen lock. App screens stay mounted underneath (hidden from
 * accessibility) so unlocking returns you where you were.
 */
export function UnlockOverlay() {
  const insets = useSafeAreaInsets();
  const bio = useSession((s) => s.biometricEnabled);
  const label = useSession((s) => s.biometricLabel);
  const prompted = useRef(false);

  useEffect(() => {
    if (bio && !prompted.current) {
      prompted.current = true;
      void unlockWithBiometric();
    }
  }, [bio]);

  return (
    <View
      accessibilityViewIsModal
      style={[StyleSheet.absoluteFill, { backgroundColor: colors.ivory, paddingTop: insets.top + 56, paddingBottom: insets.bottom + 16, paddingHorizontal: spacing.gutter, gap: 24 }]}>
      <View style={{ alignItems: 'center', gap: 16 }}>
        <AppText style={{ fontFamily: 'JetBrainsMono_600SemiBold', fontSize: 22, color: colors.accent }}>max.</AppText>
        <AppText variant="h1" accessibilityRole="header">
          Enter passcode
        </AppText>
        <AppText variant="lead">Your keys stay locked until you do.</AppText>
      </View>
      <PasscodeEntry verify={unlockWithPasscode} onBiometric={bio ? () => void unlockWithBiometric() : undefined} biometricLabel={label} />
    </View>
  );
}
