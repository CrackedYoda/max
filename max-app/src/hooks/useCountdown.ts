import { useEffect, useState } from 'react';

/** h:mm:ss / mm:ss until `iso`, ticking every second; label null once passed. */
export function useCountdown(iso: string | undefined): { label: string | null; secondsLeft: number } {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!iso) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [iso]);
  if (!iso) return { label: null, secondsLeft: 0 };
  const left = Math.floor((new Date(iso).getTime() - now) / 1000);
  if (!(left > 0)) return { label: null, secondsLeft: 0 };
  const h = Math.floor(left / 3600);
  const m = Math.floor((left % 3600) / 60);
  const s = left % 60;
  const pad = (n: number) => String(n).padStart(2, '0');
  return { label: h ? `${h}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`, secondsLeft: left };
}
