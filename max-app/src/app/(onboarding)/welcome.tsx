import { router } from 'expo-router';
import { View } from 'react-native';

import { AppText, Handle } from '@/components/ui/AppText';
import { Pill } from '@/components/ui/Blocks';
import { Button } from '@/components/ui/Button';
import { Icon } from '@/components/ui/Icon';
import { Screen } from '@/components/ui/Screen';
import { generatePhrase } from '@/services/keys';
import { useOnboarding } from '@/store/flows';
import { colors, fonts } from '@/theme';

export default function Welcome() {
  const setPhrase = useOnboarding((s) => s.setPhrase);
  return (
    <Screen
      topPadding={28}
      gap={32}
      footer={
        <>
          <Button
            label="Create a new wallet"
            onPress={() => {
              setPhrase(generatePhrase());
              router.push('/backup');
            }}
          />
          <Button label="I already have a wallet" variant="secondary" onPress={() => router.push('/import')} />
          <View style={{ flexDirection: 'row', gap: 8, alignItems: 'flex-start', marginTop: 4 }}>
            <Icon name="shield" size={16} color={colors.successStrong} />
            <AppText variant="small" style={{ flex: 1 }}>
              Self-custody: keys are encrypted on this device. MAX can’t see them or recover them for you.
            </AppText>
          </View>
        </>
      }>
      <AppText style={{ fontFamily: fonts.monoSemibold, fontSize: 22, color: colors.accent }}>max.</AppText>
      <View style={{ gap: 16 }}>
        <AppText variant="display" accessibilityRole="header">
          A wallet you can pay by name.
        </AppText>
        <AppText variant="lead" size={16} style={{ lineHeight: 25 }}>
          One wallet for Ethereum, Base and Solana. Your keys stay on this phone, and your handle replaces long addresses.
        </AppText>
      </View>
      <View style={{ backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, borderRadius: 20, padding: 20, gap: 12 }}>
        <Handle name="yourname" size={26} />
        <View style={{ flexDirection: 'row', gap: 8, flexWrap: 'wrap' }}>
          <Pill label="Ethereum" />
          <Pill label="Base" />
          <Pill label="Solana" />
        </View>
      </View>
    </Screen>
  );
}
