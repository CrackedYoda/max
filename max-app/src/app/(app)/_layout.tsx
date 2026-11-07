import { Stack } from 'expo-router';

import { ErrorBoundary } from '@/error/ErrorBoundary';
import { colors } from '@/theme';

export default function AppLayout() {
  return (
    <ErrorBoundary>
      <Stack screenOptions={{ headerShown: false, contentStyle: { backgroundColor: colors.ivory } }}>
        <Stack.Screen name="(tabs)" />
        <Stack.Screen name="claim" />
        <Stack.Screen name="review" options={{ presentation: 'transparentModal', animation: 'fade', gestureEnabled: false, contentStyle: { backgroundColor: 'transparent' } }} />
        <Stack.Screen name="scan" options={{ presentation: 'fullScreenModal' }} />
        <Stack.Screen name="destination" options={{ presentation: 'transparentModal', animation: 'fade', contentStyle: { backgroundColor: 'transparent' } }} />
        <Stack.Screen name="edit-id" options={{ presentation: 'modal' }} />
        <Stack.Screen name="reveal" />
        <Stack.Screen name="export-key" />
        <Stack.Screen name="security" />
        <Stack.Screen name="add-account" />
        <Stack.Screen name="rename" options={{ gestureEnabled: false }} />
        <Stack.Screen name="sell" />
        <Stack.Screen name="market/[name]" />
        <Stack.Screen name="market/offers" />
        <Stack.Screen name="verify" />
        <Stack.Screen name="verify/x" options={{ animation: 'none' }} />
      </Stack>
    </ErrorBoundary>
  );
}
