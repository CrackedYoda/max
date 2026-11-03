import { Stack } from 'expo-router';

import { colors } from '@/theme';

export default function OnboardingLayout() {
  return (
    <Stack screenOptions={{ headerShown: false, contentStyle: { backgroundColor: colors.ivory } }}>
      <Stack.Screen name="welcome" />
      <Stack.Screen name="backup" />
      <Stack.Screen name="verify" />
      <Stack.Screen name="import" />
      <Stack.Screen name="passcode" />
    </Stack>
  );
}
