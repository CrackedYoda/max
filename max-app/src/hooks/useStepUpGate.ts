import { router } from 'expo-router';
import { useEffect, useRef, useState } from 'react';

import { stepUp } from '@/services/session';

/**
 * Screens that show secrets ask for Face ID/passcode themselves on open, so no
 * route into them (deep link, back stack) skips the check.
 */
export function useStepUpGate(reason: string): boolean {
  const [ok, setOk] = useState(false);
  const asked = useRef(false);
  useEffect(() => {
    if (asked.current) return;
    asked.current = true;
    stepUp(reason).then((passed) => {
      if (passed) setOk(true);
      else if (router.canGoBack()) router.back();
      else router.replace('/ids');
    });
  }, [reason]);
  return ok;
}
