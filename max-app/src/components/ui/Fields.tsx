import * as Clipboard from 'expo-clipboard';
import * as Haptics from 'expo-haptics';
import { useState, type ComponentRef, type ReactNode, type Ref } from 'react';
import { Pressable, TextInput, type TextInputProps, View } from 'react-native';

import { toast } from '@/store/flows';
import { colors, fonts } from '@/theme';

import { AppText } from './AppText';
import { Icon } from './Icon';

type FieldProps = TextInputProps & {
  label?: string;
  hint?: string;
  error?: string | null;
  valid?: boolean;
  mono?: boolean;
  ref?: Ref<ComponentRef<typeof TextInput>>;
};

/** Labelled input; the accent ring shows focus, red/green shows validation. */
export function TextField({ label, hint, error, valid, mono, style, multiline, ref, ...rest }: FieldProps) {
  const [focused, setFocused] = useState(false);
  const border = error ? colors.danger : valid ? colors.successStrong : focused ? colors.accent : colors.line;
  return (
    <View style={{ gap: 8 }}>
      {label ? <AppText variant="label">{label}</AppText> : null}
      <TextInput
        ref={ref}
        placeholderTextColor={colors.placeholder}
        autoCapitalize="none"
        autoCorrect={false}
        spellCheck={false}
        multiline={multiline}
        accessibilityLabel={label}
        accessibilityHint={hint}
        {...rest}
        onFocus={(e) => {
          setFocused(true);
          rest.onFocus?.(e);
        }}
        onBlur={(e) => {
          setFocused(false);
          rest.onBlur?.(e);
        }}
        style={[
          {
            borderWidth: focused || error || valid ? 2 : 1,
            borderColor: border,
            borderRadius: 16,
            backgroundColor: colors.surface,
            padding: 14,
            fontFamily: mono ? fonts.mono : fonts.regular,
            fontSize: 15,
            lineHeight: multiline ? 24 : undefined,
            color: colors.ink,
            textAlignVertical: multiline ? 'top' : 'center',
          },
          style,
        ]}
      />
      {error ? (
        <AppText accessibilityRole="alert" size={13} color={colors.danger}>
          {error}
        </AppText>
      ) : hint ? (
        <AppText variant="small">{hint}</AppText>
      ) : null}
    </View>
  );
}

/** `max.` prefix + mono input (Claim, Send). */
export function HandleField({
  value,
  onChangeText,
  label,
  size = 22,
  trailing,
  message,
  messageTone = 'muted',
  autoFocus,
}: {
  value: string;
  onChangeText: (v: string) => void;
  label: string;
  size?: number;
  trailing?: ReactNode;
  message?: string;
  messageTone?: 'muted' | 'success' | 'danger';
  autoFocus?: boolean;
}) {
  const [focused, setFocused] = useState(false);
  const msgColor = messageTone === 'success' ? colors.success : messageTone === 'danger' ? colors.danger : colors.muted;
  return (
    <View style={{ gap: 8 }}>
      <AppText variant="label" color={size < 20 ? colors.muted : colors.ink}>
        {label}
      </AppText>
      <View
        style={{
          flexDirection: 'row',
          alignItems: 'center',
          gap: 4,
          backgroundColor: colors.surface,
          borderWidth: focused ? 2 : 1,
          borderColor: focused ? colors.accent : colors.line,
          borderRadius: size < 20 ? 14 : 16,
          paddingHorizontal: focused ? 13 : 14,
        }}>
        <AppText style={{ fontFamily: fonts.monoSemibold, fontSize: size, color: colors.accent }} accessibilityElementsHidden importantForAccessibility="no">
          max.
        </AppText>
        <TextInput
          value={value}
          onChangeText={onChangeText}
          placeholder="yourname"
          placeholderTextColor={colors.placeholder}
          autoCapitalize="none"
          autoCorrect={false}
          spellCheck={false}
          autoComplete="off"
          autoFocus={autoFocus}
          accessibilityLabel={label}
          accessibilityHint={message}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          style={{ flex: 1, minWidth: 0, fontFamily: fonts.mono, fontSize: size, color: colors.ink, paddingVertical: 12 }}
        />
        {trailing}
      </View>
      {message !== undefined ? (
        <AppText accessibilityLiveRegion="polite" weight="medium" size={13} color={msgColor} style={{ minHeight: 20 }}>
          {message}
        </AppText>
      ) : null}
    </View>
  );
}

/** Numeric keypad (passcode, amounts). Large transparent keys as on the artboards. */
export function Keypad({ onKey, extra, keyHeight = 60 }: { onKey: (k: string) => void; extra?: '.' | null; keyHeight?: number }) {
  const keys = ['1', '2', '3', '4', '5', '6', '7', '8', '9', extra ?? '', '0', 'del'];
  return (
    <View style={{ flexDirection: 'row', flexWrap: 'wrap', rowGap: 6 }}>
      {keys.map((k, i) =>
        k === '' ? (
          <View key={i} style={{ width: '33.333%', height: keyHeight }} />
        ) : (
          <View key={i} style={{ width: '33.333%', paddingHorizontal: 3 }}>
            <Pressable
              accessibilityRole="button"
              accessibilityLabel={k === 'del' ? 'Delete' : k === '.' ? 'Decimal point' : k}
              onPress={() => {
                Haptics.selectionAsync().catch(() => {});
                onKey(k);
              }}
              style={({ pressed }) => ({
                height: keyHeight,
                borderRadius: 14,
                alignItems: 'center',
                justifyContent: 'center',
                backgroundColor: pressed ? colors.sunken : 'transparent',
              })}>
              {k === 'del' ? (
                <Icon name="delete" size={24} />
              ) : (
                <AppText weight="medium" size={keyHeight >= 60 ? 26 : 24}>
                  {k}
                </AppText>
              )}
            </Pressable>
          </View>
        ),
      )}
    </View>
  );
}

export function PasscodeDots({ length, filled, error }: { length: number; filled: number; error?: boolean }) {
  return (
    <View accessible accessibilityLiveRegion="polite" accessibilityLabel={`${filled} of ${length} digits entered`} style={{ flexDirection: 'row', justifyContent: 'center', gap: 16, paddingVertical: 8 }}>
      {Array.from({ length }, (_, i) => (
        <View
          key={i}
          style={{
            width: 16,
            height: 16,
            borderRadius: 8,
            backgroundColor: i < filled ? (error ? colors.danger : colors.ink) : 'transparent',
            borderWidth: i < filled ? 0 : 2,
            borderColor: error ? colors.danger : colors.lineStrong,
          }}
        />
      ))}
    </View>
  );
}

/** Copies a value and confirms with a toast. Full addresses only. */
export function CopyButton({ value, label, what = 'Copied', bordered }: { value: string; label: string; what?: string; bordered?: boolean }) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      hitSlop={4}
      onPress={async () => {
        await Clipboard.setStringAsync(value);
        Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success).catch(() => {});
        toast(what);
      }}
      style={({ pressed }) => ({
        width: 44,
        height: 44,
        borderRadius: 10,
        borderWidth: bordered ? 1 : 0,
        borderColor: colors.line,
        alignItems: 'center',
        justifyContent: 'center',
        backgroundColor: pressed ? colors.sunken : 'transparent',
      })}>
      <Icon name="copy" size={18} color={colors.muted} />
    </Pressable>
  );
}

/** Checkbox row ("I've written down my recovery phrase"). */
export function CheckRow({ checked, onChange, children }: { checked: boolean; onChange: (v: boolean) => void; children: string }) {
  return (
    <Pressable accessibilityRole="checkbox" accessibilityState={{ checked }} onPress={() => onChange(!checked)} style={{ flexDirection: 'row', gap: 12, alignItems: 'flex-start' }}>
      <View
        style={{
          width: 22,
          height: 22,
          borderRadius: 6,
          borderWidth: checked ? 0 : 2,
          borderColor: colors.lineStrong,
          backgroundColor: checked ? colors.clay : colors.surface,
          alignItems: 'center',
          justifyContent: 'center',
          marginTop: 1,
        }}>
        {checked ? <Icon name="check" size={16} color={colors.surface} strokeWidth={3} /> : null}
      </View>
      <AppText size={14} style={{ flex: 1, lineHeight: 20 }}>
        {children}
      </AppText>
    </Pressable>
  );
}
