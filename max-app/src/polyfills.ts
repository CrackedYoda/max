// Imported first from the root layout. @noble/@scure, @solana/web3.js and the MAX
// SDK expect a few web globals that React Native doesn't provide everywhere.
import 'react-native-get-random-values';

import { Buffer } from 'buffer';
import * as ExpoCrypto from 'expo-crypto';

const g = globalThis as unknown as {
  Buffer?: typeof Buffer;
  crypto: { randomUUID?: () => string };
};
if (!g.Buffer) g.Buffer = Buffer;

// The SDK mints idempotency keys with crypto.randomUUID.
if (typeof g.crypto.randomUUID !== 'function') g.crypto.randomUUID = () => ExpoCrypto.randomUUID();

// The SDK bounds each request with AbortSignal.timeout.
const AS = AbortSignal as unknown as { timeout?: (ms: number) => AbortSignal };
if (typeof AS.timeout !== 'function') {
  AS.timeout = (ms: number) => {
    const ctrl = new AbortController();
    setTimeout(() => ctrl.abort(new Error('The request timed out.')), ms);
    return ctrl.signal;
  };
}
