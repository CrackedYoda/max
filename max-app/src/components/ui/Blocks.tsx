import type { ReactNode } from 'react';
import { ActivityIndicator, Pressable, type StyleProp, View, type ViewStyle } from 'react-native';

import { colors, radii } from '@/theme';

import { AppText } from './AppText';
import { Icon, type IconName } from './Icon';

export function Card({ children, style, padding = 16 }: { children: ReactNode; style?: StyleProp<ViewStyle>; padding?: number }) {
  return (
    <View style={[{ backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, borderRadius: 18, padding }, style]}>{children}</View>
  );
}

export function Heading({ title, lead, center }: { title: string; lead?: string; center?: boolean }) {
  return (
    <View style={{ gap: 10, alignItems: center ? 'center' : 'stretch' }}>
      <AppText variant="h1" accessibilityRole="header" style={center ? { textAlign: 'center' } : null}>
        {title}
      </AppText>
      {lead ? (
        <AppText variant="lead" style={center ? { textAlign: 'center' } : null}>
          {lead}
        </AppText>
      ) : null}
    </View>
  );
}

type Tone = 'neutral' | 'success' | 'danger' | 'clay';
const tones: Record<Tone, { bg: string; fg: string }> = {
  neutral: { bg: colors.sunken, fg: colors.body },
  success: { bg: colors.successSoft, fg: colors.successText },
  danger: { bg: colors.dangerSoft, fg: colors.dangerText },
  clay: { bg: colors.claySoft, fg: colors.clayText },
};

export function Pill({ label, tone = 'neutral', icon, small }: { label: string; tone?: Tone; icon?: IconName; small?: boolean }) {
  const t = tones[tone];
  return (
    <View
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        alignSelf: 'flex-start',
        gap: 6,
        paddingHorizontal: small ? 8 : 10,
        paddingVertical: small ? 2 : 6,
        borderRadius: radii.pill,
        backgroundColor: t.bg,
      }}>
      {icon ? <Icon name={icon} size={14} color={t.fg} /> : null}
      <AppText weight="semibold" size={small ? 11 : 12} color={t.fg}>
        {label}
      </AppText>
    </View>
  );
}

function BannerText({ children, tone = 'clay' }: { children: ReactNode; tone?: Tone }) {
  return (
    <AppText size={13} color={tones[tone].fg} style={{ lineHeight: 19 }}>
      {children}
    </AppText>
  );
}

export function Banner({ tone = 'neutral', title, children, icon }: { tone?: Tone; title?: string; children?: ReactNode; icon?: IconName }) {
  const t = tones[tone];
  return (
    <View
      accessibilityRole={tone === 'danger' ? 'alert' : undefined}
      style={{ flexDirection: 'row', gap: 10, padding: 14, borderRadius: 14, backgroundColor: t.bg, alignItems: 'flex-start' }}>
      {icon ? <Icon name={icon} size={18} color={t.fg} /> : null}
      <View style={{ flex: 1, gap: 4 }}>
        {title ? (
          <AppText weight="semibold" size={14} color={t.fg}>
            {title}
          </AppText>
        ) : null}
        {typeof children === 'string' ? (
          <AppText size={13} color={t.fg} style={{ lineHeight: 19 }}>
            {children}
          </AppText>
        ) : (
          children
        )}
      </View>
    </View>
  );
}
Banner.Text = BannerText;

/** Grouped list container with hairline separators, as on Receive / IDs. */
export function List({ children }: { children: ReactNode }) {
  return <View style={{ backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, borderRadius: 18, overflow: 'hidden' }}>{children}</View>;
}

export function ListRow({
  title,
  subtitle,
  right,
  onPress,
  last,
  danger,
  chevron,
  accessibilityLabel,
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  right?: ReactNode;
  onPress?: () => void;
  last?: boolean;
  danger?: boolean;
  chevron?: boolean;
  accessibilityLabel?: string;
}) {
  const body = (
    <>
      <View style={{ flex: 1, gap: 2 }}>
        {typeof title === 'string' ? (
          <AppText weight={danger ? 'medium' : 'semibold'} size={14} color={danger ? colors.danger : colors.ink}>
            {title}
          </AppText>
        ) : (
          title
        )}
        {typeof subtitle === 'string' ? <AppText variant="caption">{subtitle}</AppText> : subtitle}
      </View>
      {right}
      {chevron ? <Icon name="chevronRight" size={18} color={colors.muted} /> : null}
    </>
  );
  const style = {
    flexDirection: 'row' as const,
    alignItems: 'center' as const,
    gap: 12,
    minHeight: 48,
    paddingVertical: 10,
    paddingLeft: 16,
    paddingRight: chevron ? 14 : 8,
    borderBottomWidth: last ? 0 : 1,
    borderBottomColor: colors.sunken,
  };
  if (!onPress) return <View style={style}>{body}</View>;
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={accessibilityLabel} onPress={onPress} style={({ pressed }) => [style, pressed && { backgroundColor: colors.ivory }]}>
      {body}
    </Pressable>
  );
}

/** Label/value line in review sheets. */
export function ReviewRow({ label, children, last }: { label: string; children: ReactNode; last?: boolean }) {
  return (
    <View style={{ flexDirection: 'row', justifyContent: 'space-between', gap: 12, paddingVertical: 10, borderBottomWidth: last ? 0 : 1, borderBottomColor: colors.sunken }}>
      <AppText size={14} color={colors.muted}>
        {label}
      </AppText>
      <View style={{ flexShrink: 1, alignItems: 'flex-end' }}>{typeof children === 'string' ? <AppText size={14}>{children}</AppText> : children}</View>
    </View>
  );
}

export function SectionTitle({ children, right }: { children: string; right?: ReactNode }) {
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' }}>
      <AppText variant="section" accessibilityRole="header">
        {children}
      </AppText>
      {right}
    </View>
  );
}

export function Loading({ label = 'Loading' }: { label?: string }) {
  return (
    <View style={{ padding: 16, alignItems: 'center' }} accessibilityLabel={label}>
      <ActivityIndicator color={colors.muted} />
    </View>
  );
}

/** Two-option segmented control ("Me / An AI agent", "Recovery phrase / Private key"). */
export function Segmented<T extends string>({ value, options, onChange, label }: { value: T; options: { value: T; label: string }[]; onChange: (v: T) => void; label: string }) {
  return (
    <View accessibilityRole="tablist" accessibilityLabel={label} style={{ flexDirection: 'row', gap: 6, padding: 4, borderRadius: 14, backgroundColor: colors.sunken }}>
      {options.map((o) => {
        const on = o.value === value;
        return (
          <Pressable
            key={o.value}
            accessibilityRole="tab"
            accessibilityState={{ selected: on }}
            onPress={() => onChange(o.value)}
            style={{
              flex: 1,
              minHeight: 44,
              borderRadius: 10,
              alignItems: 'center',
              justifyContent: 'center',
              backgroundColor: on ? colors.surface : 'transparent',
              borderWidth: on ? 1 : 0,
              borderColor: colors.line,
            }}>
            <AppText weight={on ? 'semibold' : 'medium'} size={14} color={on ? colors.ink : colors.muted}>
              {o.label}
            </AppText>
          </Pressable>
        );
      })}
    </View>
  );
}

/** Selectable chip / radio card (network chips, EVM/Solana). */
export function Choice({ label, selected, onPress, pill, flex }: { label: string; selected: boolean; onPress: () => void; pill?: boolean; flex?: boolean }) {
  return (
    <Pressable
      accessibilityRole="radio"
      accessibilityState={{ checked: selected }}
      accessibilityLabel={label}
      onPress={onPress}
      style={{
        flex: flex ? 1 : undefined,
        minHeight: pill ? 40 : 48,
        paddingHorizontal: 14,
        borderRadius: pill ? radii.pill : 12,
        borderWidth: selected ? 2 : 1,
        borderColor: selected ? colors.accent : colors.line,
        backgroundColor: selected ? colors.claySoft : colors.surface,
        alignItems: 'center',
        justifyContent: 'center',
      }}>
      <AppText weight={selected ? 'semibold' : 'medium'} size={14}>
        {label}
      </AppText>
    </Pressable>
  );
}
