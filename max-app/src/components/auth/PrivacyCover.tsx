import { StyleSheet, View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { colors, fonts } from '@/theme';

/** Covers the UI whenever the app isn't active, so the app switcher shows no balances. */
export function PrivacyCover() {
  return (
    <View
      style={[StyleSheet.absoluteFill, { backgroundColor: colors.ivory, alignItems: 'center', justifyContent: 'center' }]}
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants">
      <AppText style={{ fontFamily: fonts.monoSemibold, fontSize: 34, color: colors.accent }}>max.</AppText>
    </View>
  );
}
