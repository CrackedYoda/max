import { TextStyle } from 'react-native';

/** Tokens from the MAX design canvas (same values as the web frontend). */
export const colors = {
  ivory: '#FAF9F5',
  surface: '#FFFFFF',
  sunken: '#F0EEE6',
  ink: '#141413',
  body: '#4A4943',
  muted: '#6B6A63',
  placeholder: '#8A897F',
  line: '#E8E6DC',
  lineStrong: '#C9C6B9',

  clay: '#B95A37',
  clayPressed: '#A9502F',
  accent: '#D97757',
  clayText: '#9A4728',
  claySoft: '#F5E6DF',

  success: '#23614A',
  successStrong: '#2F7D5B',
  successText: '#1E5440',
  successSoft: '#E3F0E9',
  danger: '#B33A3A',
  dangerText: '#7A2626',
  dangerSoft: '#F6E3E3',

  night: '#141413',
  nightRaised: '#2E2D2B',
  nightText: '#F5F4EE',
  nightMuted: '#B9B7AD',
  nightAccent: '#E08A6D',

  scrim: 'rgba(20,20,19,0.45)',
} as const;

export const fonts = {
  display: 'Fraunces_500Medium',
  regular: 'Inter_400Regular',
  medium: 'Inter_500Medium',
  semibold: 'Inter_600SemiBold',
  mono: 'JetBrainsMono_400Regular',
  monoSemibold: 'JetBrainsMono_600SemiBold',
} as const;

export const spacing = {
  gutter: 20,
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 20,
  xxl: 24,
} as const;

export const radii = {
  control: 16,
  field: 14,
  chip: 12,
  card: 20,
  sheet: 28,
  pill: 999,
} as const;

export const sizes = {
  cta: 56,
  touch: 44,
} as const;

export const type = {
  display: { fontFamily: fonts.display, fontSize: 40, lineHeight: 43, letterSpacing: -0.8, color: colors.ink },
  h1: { fontFamily: fonts.display, fontSize: 32, lineHeight: 35, color: colors.ink },
  h2: { fontFamily: fonts.display, fontSize: 30, lineHeight: 34, color: colors.ink },
  title: { fontFamily: fonts.semibold, fontSize: 17, color: colors.ink },
  section: { fontFamily: fonts.semibold, fontSize: 15, color: colors.ink },
  lead: { fontFamily: fonts.regular, fontSize: 15, lineHeight: 23, color: colors.muted },
  body: { fontFamily: fonts.regular, fontSize: 15, lineHeight: 21, color: colors.ink },
  bodyStrong: { fontFamily: fonts.semibold, fontSize: 15, lineHeight: 21, color: colors.ink },
  label: { fontFamily: fonts.semibold, fontSize: 13, color: colors.ink },
  small: { fontFamily: fonts.regular, fontSize: 13, lineHeight: 19, color: colors.muted },
  caption: { fontFamily: fonts.regular, fontSize: 12, lineHeight: 17, color: colors.muted },
  mono: { fontFamily: fonts.mono, fontSize: 14, color: colors.ink },
} satisfies Record<string, TextStyle>;
