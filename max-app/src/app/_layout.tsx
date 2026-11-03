import '@/polyfills';

import { Fraunces_500Medium } from '@expo-google-fonts/fraunces/500Medium';
import { Inter_400Regular } from '@expo-google-fonts/inter/400Regular';
import { Inter_500Medium } from '@expo-google-fonts/inter/500Medium';
import { Inter_600SemiBold } from '@expo-google-fonts/inter/600SemiBold';
import { JetBrainsMono_400Regular } from '@expo-google-fonts/jetbrains-mono/400Regular';
import { JetBrainsMono_600SemiBold } from '@expo-google-fonts/jetbrains-mono/600SemiBold';
import { focusManager, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useFonts } from 'expo-font';
import { Stack } from 'expo-router';
import * as SplashScreen from 'expo-splash-screen';
import { StatusBar } from 'expo-status-bar';
import { useEffect } from 'react';
import { AppState, View } from 'react-native';
import { GestureHandlerRootView } from 'react-native-gesture-handler';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { PrivacyCover } from '@/components/auth/PrivacyCover';
import { StepUpModal } from '@/components/auth/StepUpModal';
import { UnlockOverlay } from '@/components/auth/UnlockOverlay';
import { Toasts } from '@/components/ui/Toasts';
import { ErrorBoundary } from '@/error/ErrorBoundary';
import { useAppLock } from '@/hooks/useAppLock';
import { boot } from '@/services/session';
import { useSession } from '@/store/session';
import { colors } from '@/theme';

SplashScreen.preventAutoHideAsync().catch(() => {});

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: true } },
});

// React Query's "window focus" is the app becoming active.
focusManager.setEventListener((handleFocus) => {
  const sub = AppState.addEventListener('change', (s) => handleFocus(s === 'active'));
  return () => sub.remove();
});

export default function RootLayout() {
  const [fontsLoaded, fontError] = useFonts({
    Fraunces_500Medium,
    Inter_400Regular,
    Inter_500Medium,
    Inter_600SemiBold,
    JetBrainsMono_400Regular,
    JetBrainsMono_600SemiBold,
  });
  const vault = useSession((s) => s.vault);

  useEffect(() => {
    void boot();
  }, []);

  const ready = (fontsLoaded || !!fontError) && vault !== 'booting';
  useEffect(() => {
    if (ready) SplashScreen.hideAsync().catch(() => {});
  }, [ready]);

  if (!ready) return null;

  return (
    <ErrorBoundary>
      <GestureHandlerRootView style={{ flex: 1 }}>
        <SafeAreaProvider>
          <QueryClientProvider client={queryClient}>
            <RootNavigator />
          </QueryClientProvider>
        </SafeAreaProvider>
      </GestureHandlerRootView>
    </ErrorBoundary>
  );
}

function RootNavigator() {
  const vault = useSession((s) => s.vault);
  const unlockedOnce = useSession((s) => s.unlockedOnce);
  const { covered, touch } = useAppLock();

  // Locking keeps app screens mounted under the overlay; a cold-start lock never
  // mounts them before the first unlock.
  const appAccessible = vault === 'unlocked' || (vault === 'locked' && unlockedOnce);
  const locked = vault === 'locked';

  return (
    <View style={{ flex: 1, backgroundColor: colors.ivory }} onTouchStart={touch}>
      <StatusBar style="dark" />
      <View style={{ flex: 1 }} importantForAccessibility={locked ? 'no-hide-descendants' : 'auto'} accessibilityElementsHidden={locked}>
        <Stack screenOptions={{ headerShown: false, contentStyle: { backgroundColor: colors.ivory } }}>
          <Stack.Screen name="index" />
          <Stack.Protected guard={vault === 'none'}>
            <Stack.Screen name="(onboarding)" />
          </Stack.Protected>
          <Stack.Protected guard={appAccessible}>
            <Stack.Screen name="(app)" />
          </Stack.Protected>
        </Stack>
      </View>
      {locked ? <UnlockOverlay /> : null}
      <StepUpModal />
      <Toasts />
      {covered ? <PrivacyCover /> : null}
    </View>
  );
}
