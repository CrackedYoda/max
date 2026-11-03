import { useCallback, useRef, useState } from 'react';

/**
 * Digit entry that never drops fast taps: the current value lives in a ref, so
 * taps landing before a re-render still build on each other. `press` returns
 * the full code when this tap completed it, so callers react in their handler.
 */
export function usePinInput(length: number) {
  const ref = useRef('');
  const [code, setCodeState] = useState('');

  const set = useCallback((v: string) => {
    ref.current = v;
    setCodeState(v);
  }, []);

  const press = useCallback(
    (k: string): string | null => {
      if (k === 'del') {
        set(ref.current.slice(0, -1));
        return null;
      }
      if (ref.current.length >= length) return null;
      const next = ref.current + k;
      set(next);
      return next.length === length ? next : null;
    },
    [length, set],
  );

  return { code, press, set };
}
