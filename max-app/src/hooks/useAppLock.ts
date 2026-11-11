import * as ScreenCapture from 'expo-screen-capture';
import { useEffect, useRef, useState } from 'react';
import { AppState, type AppStateStatus, Platform } from 'react-native';

import { lock } from '@/services/session';
import { useSession } from '@/store/session';

export const INACTIVITY_LOCK_MS = 5 * 60_000;

/**
 * Locks on background and after inactivity (Noma useAppLock), and reports
 * whether the privacy cover should show (any non-active state: the app switcher).
 */
export function useAppLock(): { covered: boolean; touch: () => void } {
  const [covered, setCovered] = useState(AppState.currentState !== 'active');
  const lastTouch = useRef(0);

  useEffect(() => {
    lastTouch.current = Date.now();
    if (Platform.OS === 'ios') {
      // Native blur in the app-switcher snapshot; the JS cover alone can be too late.
      ScreenCapture.enableAppSwitcherProtectionAsync(0.9).catch(() => {});
    }
    const sub = AppState.addEventListener('change', (s: AppStateStatus) => {
      setCovered(s !== 'active');
      if (s === 'background') lock();
      if (s === 'active') lastTouch.current = Date.now();
    });
    const timer = setInterval(() => {
      // Only an open wallet times out: lock() while locked would cancel an unlock being typed.
      if (useSession.getState().vault === 'unlocked' && Date.now() - lastTouch.current > INACTIVITY_LOCK_MS) lock();
    }, 15_000);
    return () => {
      sub.remove();
      clearInterval(timer);
    };
  }, []);

  return {
    covered,
    touch: () => {
      lastTouch.current = Date.now();
    },
  };
}

/** Blocks screenshots and recording while a screen showing secrets is mounted. */
export function useSecureScreen(key: string): void {
  useEffect(() => {
    if (Platform.OS === 'web') return;
    ScreenCapture.preventScreenCaptureAsync(key).catch(() => {});
    return () => {
      ScreenCapture.allowScreenCaptureAsync(key).catch(() => {});
    };
  }, [key]);
}
