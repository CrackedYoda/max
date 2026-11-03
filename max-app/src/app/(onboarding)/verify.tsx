import { Redirect, router } from 'expo-router';
import { useState } from 'react';
import { Pressable, View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Heading } from '@/components/ui/Blocks';
import { Button } from '@/components/ui/Button';
import { Screen, StepHeader } from '@/components/ui/Screen';
import { useSecureScreen } from '@/hooks/useAppLock';
import { recoveryWords } from '@/services/keys';
import { useOnboarding } from '@/store/flows';
import { colors, fonts } from '@/theme';
import { randomInt, shuffle } from '@/utils/random';

type Question = { n: number; options: string[]; answer: string };

function makeQuestions(phrase: string): Question[] {
  const words = phrase.split(' ');
  const positions = new Set<number>();
  while (positions.size < 3) positions.add(randomInt(words.length));
  return [...positions]
    .sort((a, b) => a - b)
    .map((i) => {
      const answer = words[i]!;
      const decoys = new Set<string>();
      while (decoys.size < 2) {
        const w = recoveryWords[randomInt(recoveryWords.length)]!;
        if (!words.includes(w)) decoys.add(w);
      }
      return { n: i + 1, answer, options: shuffle([answer, ...decoys]) };
    });
}

export default function Verify() {
  useSecureScreen('verify');
  const phrase = useOnboarding((s) => s.phrase);
  const [questions] = useState(() => (phrase ? makeQuestions(phrase) : []));
  const [picks, setPicks] = useState<Record<number, string>>({});
  if (!phrase) return <Redirect href="/welcome" />;

  const done = questions.every((q) => picks[q.n] === q.answer);

  return (
    <Screen
      scroll
      gap={22}
      header={<StepHeader step={2} total={3} onBack={() => router.back()} />}
      footer={
        <>
          <Button label="Continue" disabled={!done} onPress={() => router.push('/passcode')} />
          <AppText variant="small" style={{ textAlign: 'center' }}>
            A wrong pick shows in red. You can go back and look again.
          </AppText>
        </>
      }>
      <Heading title="Check your backup" lead="Pick the right word for each position, from what you wrote down." />
      {questions.map((q) => (
        <View key={q.n} accessibilityRole="radiogroup" accessibilityLabel={`Word number ${q.n}`} style={{ padding: 16, borderWidth: 1, borderColor: colors.line, borderRadius: 18, backgroundColor: colors.surface, gap: 12 }}>
          <AppText weight="semibold" size={14} accessibilityElementsHidden>
            Word #{q.n}
          </AppText>
          <View style={{ flexDirection: 'row', gap: 8 }}>
            {q.options.map((o) => {
              const on = picks[q.n] === o;
              const right = o === q.answer;
              return (
                <Pressable
                  key={o}
                  accessibilityRole="radio"
                  accessibilityState={{ checked: on }}
                  accessibilityLabel={on ? `${o}, ${right ? 'correct' : 'wrong'}` : o}
                  onPress={() => setPicks((p) => ({ ...p, [q.n]: o }))}
                  style={{
                    flex: 1,
                    minHeight: 44,
                    paddingHorizontal: 6,
                    borderRadius: 12,
                    alignItems: 'center',
                    justifyContent: 'center',
                    borderWidth: on ? 2 : 1,
                    borderColor: !on ? colors.line : right ? colors.successStrong : colors.danger,
                    backgroundColor: !on ? colors.ivory : right ? colors.successSoft : colors.dangerSoft,
                  }}>
                  <AppText
                    numberOfLines={1}
                    adjustsFontSizeToFit
                    style={{ fontFamily: on ? fonts.monoSemibold : fonts.mono, fontSize: 13, color: !on ? colors.ink : right ? colors.successText : colors.dangerText }}>
                    {o}
                  </AppText>
                </Pressable>
              );
            })}
          </View>
        </View>
      ))}
    </Screen>
  );
}
