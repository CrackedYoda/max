import { existsSync } from 'node:fs';
import { fileURLToPath, pathToFileURL } from 'node:url';

const SRC = fileURLToPath(new URL('../src/', import.meta.url));
const STUB = (name) => new URL(`./test-stubs/${name}.mjs`, import.meta.url).href;

/** Modules that need a device (or a server) are replaced by fakes tests can drive. */
const STUBS = {
  'react-native': STUB('react-native'),
  'expo-secure-store': STUB('expo-secure-store'),
  'expo-crypto': STUB('expo-crypto'),
  '@/api/client': STUB('api-client'),
  '@/services/keys': STUB('keys'),
  '@/services/passcode': STUB('passcode'),
};

export async function resolve(specifier, context, next) {
  if (STUBS[specifier]) return { url: STUBS[specifier], shortCircuit: true };
  if (specifier.startsWith('@/')) {
    const base = SRC + specifier.slice(2);
    for (const file of [`${base}.ts`, `${base}.tsx`, `${base}/index.ts`]) {
      if (existsSync(file)) return { url: pathToFileURL(file).href, shortCircuit: true };
    }
  }
  return next(specifier, context);
}
