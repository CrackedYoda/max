import { z } from 'zod';

const schema = z.object({
  apiUrl: z.url(),
  webUrl: z.url(),
  rpc: z.record(z.string(), z.url()),
});

export type Env = z.infer<typeof schema>;

let cached: Env | null = null;

/**
 * Public configuration, validated once. EXPO_PUBLIC_* values are inlined at build
 * time, so each must be referenced literally. A chain is usable in the app only
 * when it has an RPC here: balances and broadcasting go straight to the chain.
 */
export function getEnv(): Env {
  if (cached) return cached;
  const rpc: Record<string, string> = {};
  const entries: [string, string | undefined][] = [
    ['ethereum', process.env.EXPO_PUBLIC_RPC_ETHEREUM],
    ['base', process.env.EXPO_PUBLIC_RPC_BASE],
    ['arbitrum', process.env.EXPO_PUBLIC_RPC_ARBITRUM],
    ['optimism', process.env.EXPO_PUBLIC_RPC_OPTIMISM],
    ['polygon', process.env.EXPO_PUBLIC_RPC_POLYGON],
    ['solana', process.env.EXPO_PUBLIC_RPC_SOLANA],
    ['sepolia', process.env.EXPO_PUBLIC_RPC_SEPOLIA],
    ['base-sepolia', process.env.EXPO_PUBLIC_RPC_BASE_SEPOLIA],
    ['solana-devnet', process.env.EXPO_PUBLIC_RPC_SOLANA_DEVNET],
  ];
  for (const [k, v] of entries) if (v) rpc[k] = v;
  cached = schema.parse({
    apiUrl: process.env.EXPO_PUBLIC_MAX_API_URL ?? 'http://localhost:8080',
    webUrl: process.env.EXPO_PUBLIC_WEB_URL ?? 'http://localhost:3000',
    rpc,
  });
  return cached;
}

/**
 * Well-known USDC deployments. A token is always identified by its exact
 * contract/mint; the symbol is only a label.
 */
export const USDC: Record<string, string> = {
  ethereum: '0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48',
  base: '0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913',
  arbitrum: '0xaf88d065e77c8cC2239327C5EDb3A432268e5831',
  optimism: '0x0b2C639c533813f4Aa9D7837CAf62653d097Ff85',
  polygon: '0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359',
  solana: 'EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v',
  sepolia: '0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238',
  'base-sepolia': '0x036CbD53842c5426634e7929541eC2318f3dCF7e',
  'solana-devnet': '4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU',
};
