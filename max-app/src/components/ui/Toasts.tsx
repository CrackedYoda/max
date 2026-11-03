import { View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { useToasts } from '@/store/flows';
import { colors } from '@/theme';

import { AppText } from './AppText';

export function Toasts() {
  const toasts = useToasts((s) => s.toasts);
  const insets = useSafeAreaInsets();
  if (!toasts.length) return null;
  return (
    <View pointerEvents="none" style={{ position: 'absolute', left: 20, right: 20, top: insets.top + 8, gap: 8 }}>
      {toasts.map((t) => (
        <View key={t.id} accessibilityLiveRegion="polite" accessibilityRole="alert" style={{ alignSelf: 'center', backgroundColor: colors.ink, paddingHorizontal: 16, paddingVertical: 12, borderRadius: 14 }}>
          <AppText weight="medium" size={14} color={colors.nightText}>
            {t.text}
          </AppText>
        </View>
      ))}
    </View>
  );
}
