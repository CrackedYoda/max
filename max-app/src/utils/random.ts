import * as Crypto from 'expo-crypto';

/** Uniform integer in [0, n) from the CSPRNG (rejection sampling, no modulo bias). */
export function randomInt(n: number): number {
  const limit = Math.floor(0x100000000 / n) * n;
  for (;;) {
    const b = Crypto.getRandomBytes(4);
    const v = ((b[0]! << 24) | (b[1]! << 16) | (b[2]! << 8) | b[3]!) >>> 0;
    if (v < limit) return v % n;
  }
}

export function shuffle<T>(items: T[]): T[] {
  const a = [...items];
  for (let i = a.length - 1; i > 0; i--) {
    const j = randomInt(i + 1);
    [a[i], a[j]] = [a[j]!, a[i]!];
  }
  return a;
}
