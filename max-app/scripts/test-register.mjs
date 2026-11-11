// Lets node --test import app modules: resolves the `@/` alias and swaps
// native-only modules for in-memory fakes in ./test-stubs.
// Run from max-app: node --import ./scripts/test-register.mjs --test scripts/<file>.test.mjs
import { register } from 'node:module';

register('./test-hooks.mjs', import.meta.url);
