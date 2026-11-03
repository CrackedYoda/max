import type { ReactNode } from 'react';
import { KeyboardAvoidingView, Platform, ScrollView, type StyleProp, View, type ViewStyle } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { colors, spacing } from '@/theme';

import { AppText } from './AppText';
import { IconButton } from './Button';

type Props = {
  children: ReactNode;
  /** Pinned to the bottom (CTA area), outside the scroll view. */
  footer?: ReactNode;
  /** Pinned above the content (back button / progress). */
  header?: ReactNode;
  scroll?: boolean;
  gap?: number;
  style?: StyleProp<ViewStyle>;
  /** Tab screens already sit above a tab bar. */
  bottomInset?: boolean;
  topPadding?: number;
};

/** Ivory page with safe areas, optional scroll and a pinned footer (Noma Screen). */
export function Screen({ children, footer, header, scroll = false, gap = spacing.xl, style, bottomInset = true, topPadding = 12 }: Props) {
  const insets = useSafeAreaInsets();
  const content = <View style={[{ gap, paddingHorizontal: spacing.gutter, flexGrow: 1 }, style]}>{children}</View>;
  return (
    <KeyboardAvoidingView behavior={Platform.OS === 'ios' ? 'padding' : undefined} style={{ flex: 1, backgroundColor: colors.ivory }}>
      <View style={{ flex: 1, paddingTop: insets.top + topPadding }}>
        {header ? <View style={{ paddingHorizontal: spacing.gutter, paddingBottom: 12 }}>{header}</View> : null}
        {scroll ? (
          <ScrollView contentContainerStyle={{ flexGrow: 1, paddingBottom: 24 }} keyboardShouldPersistTaps="handled" showsVerticalScrollIndicator={false}>
            {content}
          </ScrollView>
        ) : (
          content
        )}
        {footer ? (
          <View style={{ paddingHorizontal: spacing.gutter, paddingTop: 12, paddingBottom: (bottomInset ? insets.bottom : 0) + 12, gap: 10 }}>{footer}</View>
        ) : (
          <View style={{ height: bottomInset ? insets.bottom : 0 }} />
        )}
      </View>
    </KeyboardAvoidingView>
  );
}

/** Back button + segmented step progress, as on the onboarding artboards. */
export function StepHeader({ step, total, onBack }: { step: number; total: number; onBack?: () => void }) {
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8, marginLeft: -12 }}>
      {onBack ? <IconButton icon="back" label="Back" onPress={onBack} /> : <View style={{ width: 44 }} />}
      <View
        accessibilityRole="progressbar"
        accessibilityLabel={`Step ${step} of ${total}`}
        accessibilityValue={{ min: 1, max: total, now: step }}
        style={{ flex: 1, flexDirection: 'row', gap: 6, paddingRight: 44 }}>
        {Array.from({ length: total }, (_, i) => (
          <View key={i} style={{ flex: 1, height: 4, borderRadius: 999, backgroundColor: i < step ? colors.accent : colors.line }} />
        ))}
      </View>
    </View>
  );
}

/** Back/close + centered title (Import, Send). */
export function TitleHeader({ title, onBack, icon = 'back', right }: { title: string; onBack?: () => void; icon?: 'back' | 'close'; right?: ReactNode }) {
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginHorizontal: -12 }}>
      {onBack ? <IconButton icon={icon} label={icon === 'close' ? 'Close' : 'Back'} onPress={onBack} /> : <View style={{ width: 44 }} />}
      <AppText variant="title" accessibilityRole="header">
        {title}
      </AppText>
      {right ?? <View style={{ width: 44 }} />}
    </View>
  );
}
