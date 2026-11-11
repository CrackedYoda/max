import { router, useNavigation } from 'expo-router';
import { useCallback, useEffect, useRef, useState } from 'react';

import { stepUp } from '@/services/session';
import { getGeneration, useSession } from '@/store/session';

/**
 * Screens that show secrets ask for Face ID/passcode themselves on open, so no
 * route into them (deep link, back stack) skips the check.
 *
 * The approval belongs to the lock generation it was given in. A lock, or the
 * screen losing focus, revokes it for good: the hook returns false (screens
 * erase what they show when it does) and the screen closes rather than ask
 * again. Coming back to it means opening it again.
 */
export function useStepUpGate(reason: string): boolean {
  const generation = useSession((s) => s.generation);
  const [grant, setGrant] = useState<number | null>(null);
  const asked = useRef(false);
  const revoked = useRef(false);
  const focused = useRef(true);
  const mounted = useRef(true);

  const leave = useCallback(() => {
    revoked.current = true;
    setGrant(null);
    // Unfocused: closing now would pop whatever screen is on top. Close on return instead.
    if (!mounted.current || !focused.current) return;
    if (router.canGoBack()) router.back();
    else router.replace('/ids');
  }, []);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    if (asked.current) return;
    asked.current = true;
    const gen = getGeneration();
    stepUp(reason).then((passed) => {
      if (passed && !revoked.current && gen === getGeneration()) setGrant(gen);
      else leave();
    });
  }, [reason, leave]);

  // A lock bumps the generation: revoke, whether the check is done or still showing.
  useEffect(
    () =>
      useSession.subscribe((s, prev) => {
        if (s.generation !== prev.generation && asked.current) leave();
      }),
    [leave],
  );

  // Navigation events, not a focus effect: StrictMode's simulated remount must not revoke.
  const navigation = useNavigation();
  useEffect(() => {
    const offBlur = navigation.addListener('blur', () => {
      focused.current = false;
      if (asked.current) leave();
    });
    const offFocus = navigation.addListener('focus', () => {
      focused.current = true;
      if (revoked.current) leave();
    });
    return () => {
      offBlur();
      offFocus();
    };
  }, [navigation, leave]);

  return grant !== null && grant === generation;
}
