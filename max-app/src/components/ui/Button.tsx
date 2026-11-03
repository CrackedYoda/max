import type { ReactNode } from 'react';
import { ActivityIndicator, Pressable, type StyleProp, View, type ViewStyle } from 'react-native';

import { colors, radii, sizes } from '@/theme';

import { AppText } from './AppText';
import { Icon, type IconName } from './Icon';

type Variant = 'primary' | 'secondary' | 'dark' | 'danger' | 'ghost' | 'soft';

const palette: Record<Variant, { bg: string; fg: string; border?: string; pressed: string }> = {
  primary: { bg: colors.clay, fg: colors.surface, pressed: colors.clayPressed },
  secondary: { bg: colors.surface, fg: colors.ink, border: colors.line, pressed: colors.sunken },
  dark: { bg: colors.ink, fg: colors.surface, pressed: '#2A2A28' },
  danger: { bg: colors.surface, fg: colors.danger, border: colors.line, pressed: colors.dangerSoft },
  ghost: { bg: 'transparent', fg: colors.ink, pressed: colors.sunken },
  soft: { bg: colors.claySoft, fg: colors.clayText, pressed: '#EED8CE' },
};

type Props = {
  label: string;
  onPress?: () => void;
  variant?: Variant;
  disabled?: boolean;
  loading?: boolean;
  icon?: IconName;
  size?: 'lg' | 'md' | 'sm';
  accessibilityHint?: string;
  style?: StyleProp<ViewStyle>;
  testID?: string;
};

/** Canvas CTA: 56px, 16px radius, clay. Disabled is the canvas's sunken grey, not a faded clay. */
export function Button({ label, onPress, variant = 'primary', disabled, loading, icon, size = 'lg', accessibilityHint, style, testID }: Props) {
  const p = palette[variant];
  const inactive = disabled || loading;
  const height = size === 'lg' ? sizes.cta : size === 'md' ? 48 : 40;
  const grey = disabled && !loading;
  return (
    <Pressable
      testID={testID}
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityHint={accessibilityHint}
      accessibilityState={{ disabled: !!inactive, busy: !!loading }}
      disabled={inactive}
      onPress={onPress}
      style={({ pressed }) => [
        {
          minHeight: height,
          borderRadius: size === 'sm' ? radii.chip : size === 'md' ? 14 : radii.control,
          backgroundColor: grey ? colors.line : pressed ? p.pressed : p.bg,
          borderWidth: p.border && !grey ? 1 : 0,
          borderColor: p.border,
          alignItems: 'center',
          justifyContent: 'center',
          flexDirection: 'row',
          gap: 8,
          paddingHorizontal: size === 'sm' ? 14 : 18,
        },
        style,
      ]}>
      {loading ? (
        <ActivityIndicator color={p.fg} />
      ) : (
        <>
          {icon ? <Icon name={icon} size={size === 'lg' ? 20 : 18} color={grey ? colors.muted : p.fg} /> : null}
          <AppText weight="semibold" size={size === 'lg' ? 16 : size === 'md' ? 15 : 14} color={grey ? colors.muted : p.fg}>
            {label}
          </AppText>
        </>
      )}
    </Pressable>
  );
}

export function IconButton({
  icon,
  label,
  onPress,
  bordered = false,
  color = colors.ink,
}: {
  icon: IconName;
  label: string;
  onPress: () => void;
  bordered?: boolean;
  color?: string;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      onPress={onPress}
      hitSlop={4}
      style={({ pressed }) => ({
        width: sizes.touch,
        height: sizes.touch,
        borderRadius: 12,
        borderWidth: bordered ? 1 : 0,
        borderColor: colors.line,
        backgroundColor: pressed ? colors.sunken : bordered ? colors.surface : 'transparent',
        alignItems: 'center',
        justifyContent: 'center',
      })}>
      <Icon name={icon} color={color} />
    </Pressable>
  );
}

/** Text-only action (e.g. "Skip for now", "Change destinations"). */
export function LinkButton({ label, onPress, color = colors.ink, weight = 'medium', size = 15, center = true }: {
  label: string;
  onPress: () => void;
  color?: string;
  weight?: 'medium' | 'semibold';
  size?: number;
  center?: boolean;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      onPress={onPress}
      hitSlop={8}
      style={({ pressed }) => ({ minHeight: sizes.touch, justifyContent: 'center', alignItems: center ? 'center' : 'flex-start', opacity: pressed ? 0.6 : 1 })}>
      <AppText weight={weight} size={size} color={color}>
        {label}
      </AppText>
    </Pressable>
  );
}

export function Row({ children, gap = 8, style }: { children: ReactNode; gap?: number; style?: StyleProp<ViewStyle> }) {
  return <View style={[{ flexDirection: 'row', alignItems: 'center', gap }, style]}>{children}</View>;
}
