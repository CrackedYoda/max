import { z } from "zod";

const schema = z.object({
  apiUrl: z.url(),
  rpc: z.record(z.string(), z.url()),
  /**
   * `chain:asset[:decimals]` entries mirroring max-core MARKET_ASSETS. Offers on
   * unlisted names use the first; `decimals` pins the token's decimals so a
   * price is never converted with decimals that came from the API.
   */
  marketAssets: z.array(z.object({ chain: z.string().min(1), asset: z.string().min(1), decimals: z.number().int().min(0).max(30).optional() })),
});

export type Env = z.infer<typeof schema>;

let cached: Env | null = null;

/**
 * Public configuration, validated once. NEXT_PUBLIC_* values are inlined at
 * build time, so each must be referenced literally here.
 */
export function getEnv(): Env {
  if (cached) return cached;
  const rpc: Record<string, string> = {};
  const entries: [string, string | undefined][] = [
    ["ethereum", process.env.NEXT_PUBLIC_RPC_ETHEREUM],
    ["base", process.env.NEXT_PUBLIC_RPC_BASE],
    ["arbitrum", process.env.NEXT_PUBLIC_RPC_ARBITRUM],
    ["optimism", process.env.NEXT_PUBLIC_RPC_OPTIMISM],
    ["polygon", process.env.NEXT_PUBLIC_RPC_POLYGON],
    ["solana", process.env.NEXT_PUBLIC_RPC_SOLANA],
    ["sepolia", process.env.NEXT_PUBLIC_RPC_SEPOLIA],
    ["base-sepolia", process.env.NEXT_PUBLIC_RPC_BASE_SEPOLIA],
    ["solana-devnet", process.env.NEXT_PUBLIC_RPC_SOLANA_DEVNET],
  ];
  for (const [k, v] of entries) if (v) rpc[k] = v;
  const marketAssets = (process.env.NEXT_PUBLIC_MARKET_ASSETS ?? "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean)
    .map((pair) => {
      const [chain = "", asset = "", dec] = pair.split(":");
      return { chain, asset, decimals: dec && /^\d+$/.test(dec) ? Number(dec) : undefined };
    })
    .filter((a) => a.chain && a.asset);
  cached = schema.parse({ apiUrl: process.env.NEXT_PUBLIC_MAX_API_URL ?? "http://localhost:8080", rpc, marketAssets });
  return cached;
}
