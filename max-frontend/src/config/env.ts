import { z } from "zod";

const schema = z.object({
  apiUrl: z.url(),
  rpc: z.record(z.string(), z.url()),
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
  cached = schema.parse({ apiUrl: process.env.NEXT_PUBLIC_MAX_API_URL ?? "http://localhost:8080", rpc });
  return cached;
}
