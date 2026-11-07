import { z } from 'zod';

const schema = z.object({
  apiUrl: z.url(),
  webUrl: z.url(),
  rpc: z.record(z.string(), z.url()),
  /**
   * `chain:mint[:decimals]` entries mirroring max-core MARKET_ASSETS. The
   * decimals pin is what a market price is converted with before signing, so
   * it never comes from the API; it is cross-checked against the mint on chain.
   */
  marketAssets: z.array(z.object({ chain: z.string().min(1), asset: z.string().min(1), decimals: z.number().int().min(0).max(30).optional() })),
  /**
   * EXPO_PUBLIC_MARKET_TREASURY: the MAX Solana treasury (max-core
   * TREASURY_SOLANA). When set, a sale whose fee goes anywhere else is
   * refused before signing.
   */
  marketTreasury: z.string().regex(/^[1-9A-HJ-NP-Za-km-z]{32,44}$/, 'EXPO_PUBLIC_MARKET_TREASURY must be a Solana address').optional(),
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
    marketAssets: parseMarketAssets(process.env.EXPO_PUBLIC_MARKET_ASSETS),
    apiUrl: process.env.EXPO_PUBLIC_MAX_API_URL ?? 'http://localhost:8080',
    webUrl: process.env.EXPO_PUBLIC_WEB_URL ?? 'http://localhost:3000',
    rpc,
    marketTreasury: process.env.EXPO_PUBLIC_MARKET_TREASURY?.trim() || undefined,
  });
  return cached;
}

/** `solana-devnet:<mint>:6,…` → entries; malformed pairs are dropped. */
export function parseMarketAssets(raw: string | undefined): { chain: string; asset: string; decimals?: number }[] {
  return (raw ?? '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
    .map((pair) => {
      const [chain = '', asset = '', dec] = pair.split(':');
      return { chain, asset, decimals: dec && /^\d+$/.test(dec) ? Number(dec) : undefined };
    })
    .filter((a) => a.chain && a.asset);
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
