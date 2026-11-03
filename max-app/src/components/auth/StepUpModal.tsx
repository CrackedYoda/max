import { StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText } from '@/components/ui/AppText';
import { TitleHeader } from '@/components/ui/Screen';
import { verifyPasscode } from '@/services/passcode';
import { useStepUp } from '@/store/flows';
import { colors, spacing } from '@/theme';

import { PasscodeEntry } from './PasscodeEntry';

/**
 * "Confirm it's you" with the passcode, when Face ID is off or failed.
 *
 * Rendered as an in-tree overlay rather than an RN <Modal>: UIKit refuses to
 * present a modal from the root while a native modal screen (the review sheet)
 * is up. The root renders one host; screens presented modally render their own.
 */
export function StepUpModal() {
  const request = useStepUp((s) => s.request);
  const set = useStepUp((s) => s.set);
  const insets = useSafeAreaInsets();
  if (!request) return null;

  const finish = (ok: boolean) => {
    request.resolve(ok);
    set(null);
  };

  return (
    <View
      accessibilityViewIsModal
      style={[StyleSheet.absoluteFill, { backgroundColor: colors.ivory, paddingTop: insets.top + 12, paddingBottom: insets.bottom + 16, paddingHorizontal: spacing.gutter, gap: 20 }]}>
      <TitleHeader title="Confirm it’s you" icon="close" onBack={() => finish(false)} />
      <AppText variant="lead" style={{ textAlign: 'center' }}>
        {request.reason}
      </AppText>
      <PasscodeEntry
        verify={async (code) => {
          const res = await verifyPasscode(code);
          if (res.ok) finish(true);
          return res;
        }}
      />
    </View>
  );
}
