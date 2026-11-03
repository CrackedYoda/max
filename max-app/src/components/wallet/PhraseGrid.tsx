import { Pressable, View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Icon } from '@/components/ui/Icon';
import { colors, fonts } from '@/theme';

/**
 * Numbered recovery words in two columns. While hidden, the words aren't
 * rendered at all (not merely blurred), so nothing leaks to accessibility or snapshots.
 */
export function PhraseGrid({ phrase, hidden, onReveal }: { phrase: string; hidden: boolean; onReveal: () => void }) {
  const words = phrase.split(' ');
  return (
    <View style={{ backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, borderRadius: 20, padding: 16 }}>
      <View style={{ flexDirection: 'row', flexWrap: 'wrap', justifyContent: 'space-between', rowGap: 8 }} accessibilityElementsHidden={hidden} importantForAccessibility={hidden ? 'no-hide-descendants' : 'auto'}>
        {words.map((w, i) => (
          <View
            key={i}
            accessible
            accessibilityLabel={hidden ? undefined : `${i + 1}. ${w}`}
            style={{ width: '48.5%', flexDirection: 'row', alignItems: 'center', gap: 10, paddingVertical: 10, paddingHorizontal: 12, borderRadius: 12, backgroundColor: colors.ivory }}>
            <AppText size={12} color={colors.muted} style={{ width: 18 }}>
              {i + 1}
            </AppText>
            <AppText style={{ fontFamily: fonts.mono, fontSize: 14 }}>{hidden ? '•••••' : w}</AppText>
          </View>
        ))}
      </View>
      {hidden ? (
        <Pressable
          accessibilityRole="button"
          accessibilityLabel="Tap to reveal your recovery phrase"
          accessibilityHint="Make sure no one can see your screen"
          onPress={onReveal}
          style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0, borderRadius: 20, backgroundColor: 'rgba(255,255,255,0.82)', alignItems: 'center', justifyContent: 'center', gap: 8 }}>
          <Icon name="eye" size={28} />
          <AppText weight="semibold" size={15}>
            Tap to reveal
          </AppText>
          <AppText size={13} color={colors.muted}>
            Make sure no one can see your screen
          </AppText>
        </Pressable>
      ) : null}
    </View>
  );
}

export function PhraseWarnings() {
  return (
    <View style={{ paddingVertical: 14, paddingHorizontal: 16, borderRadius: 14, backgroundColor: colors.dangerSoft, gap: 8 }}>
      {['Never share it. MAX will never ask for it.', 'Don’t screenshot it or paste it into notes or chat.', 'Lose it and lose this phone, and the funds are gone.'].map((t) => (
        <AppText key={t} size={13} color={colors.dangerText} style={{ lineHeight: 19 }}>
          {t}
        </AppText>
      ))}
    </View>
  );
}
