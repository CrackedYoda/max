import { StyleSheet, Text, type TextProps, type TextStyle } from 'react-native';

import { colors, fonts, type as typeScale } from '@/theme';

type Variant = keyof typeof typeScale;
type Weight = keyof typeof fonts;

type Props = TextProps & { variant?: Variant; weight?: Weight; color?: string; size?: number };

/** Each weight is its own font family; `fontWeight` alone does nothing on Android. */
export function AppText({ variant = 'body', weight, color, size, style, ...rest }: Props) {
  // A larger font passed via style must not inherit the variant's tighter line height (it clips).
  const override = StyleSheet.flatten(style) as TextStyle | undefined;
  const fontSize = override?.fontSize ?? size;
  const lineHeight = override?.lineHeight ?? (fontSize ? Math.round(fontSize * 1.25) : undefined);
  return (
    <Text
      maxFontSizeMultiplier={1.6}
      {...rest}
      style={[
        { color: colors.ink },
        typeScale[variant],
        weight ? { fontFamily: fonts[weight] } : null,
        size ? { fontSize: size } : null,
        color ? { color } : null,
        style,
        fontSize ? { lineHeight } : null,
      ]}
    />
  );
}

/** `max.` in accent + the name, in JetBrains Mono. */
export function Handle({ name, size = 16, accent = colors.accent, color = colors.ink }: { name: string; size?: number; accent?: string; color?: string }) {
  const bare = name.replace(/^max\./, '');
  return (
    <Text maxFontSizeMultiplier={1.4} accessibilityLabel={`max.${bare}`} style={{ fontFamily: fonts.monoSemibold, fontSize: size, color }}>
      <Text style={{ color: accent }}>max.</Text>
      {bare}
    </Text>
  );
}

export function Mono({ children, size = 13, color = colors.ink, selectable }: { children: React.ReactNode; size?: number; color?: string; selectable?: boolean }) {
  return (
    <Text selectable={selectable} maxFontSizeMultiplier={1.4} style={{ fontFamily: fonts.mono, fontSize: size, color }}>
      {children}
    </Text>
  );
}
