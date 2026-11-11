// Fake key derivation: tests control timing through `fakeKeys.derive`.
export const fakeKeys = {
  derive: async () => ({ evm: 'evm-secret', solanaSeed: 'sol-seed' }),
};

export const deriveFromPhrase = (phrase) => fakeKeys.derive(phrase);
export const evmKey = (secret) => ({ namespace: 'evm', address: `0xEVM-${secret}`, secret });
export const solanaKeyFromSeed = (seed) => ({ namespace: 'solana', address: `SOL-${seed}`, seed });
export const solanaKeyFromSecret = (secret) => ({ namespace: 'solana', address: `SOL-${secret}`, secret });
export const walletSigner = (key) => ({ key });
