// https://docs.expo.dev/guides/using-eslint/
const { defineConfig } = require('eslint/config');
const expoConfig = require('eslint-config-expo/flat');

module.exports = defineConfig([
  expoConfig,
  { ignores: ['dist/*', '.expo/*', 'node_modules/*'] },
  {
    rules: {
      // Keys and passcode salts come from expo-crypto only.
      'no-restricted-properties': ['error', { object: 'Math', property: 'random', message: 'Use expo-crypto (CSPRNG).' }],
      'no-restricted-imports': [
        'error',
        {
          paths: [
            { name: '@react-native-async-storage/async-storage', message: 'Secrets never go to AsyncStorage. Use src/services/secure.ts.' },
            { name: 'expo-secure-store', message: 'Only src/services/secure.ts touches SecureStore.' },
          ],
        },
      ],
      'no-console': ['error', { allow: ['warn'] }],
    },
  },
  { files: ['src/services/secure.ts'], rules: { 'no-restricted-imports': 'off' } },
]);
