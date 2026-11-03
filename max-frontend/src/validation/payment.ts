import { z } from "zod";

export const amountSchema = z
  .string()
  .trim()
  .regex(/^\d*\.?\d+$/, "Enter an amount like 0.25")
  .refine((v) => Number(v) > 0, "Amount must be more than zero");

const evmAddress = /^0x[0-9a-fA-F]{40}$/;
const solanaAddress = /^[1-9A-HJ-NP-Za-km-z]{32,44}$/;

/** A token is identified by its exact contract/mint, never a symbol. */
export function tokenAddressError(namespace: string, value: string): string | null {
  const v = value.trim();
  if (!v) return "Paste the token’s full contract or mint address.";
  if (namespace === "evm" && !evmAddress.test(v)) return "EVM token contracts are 0x followed by 40 hex characters.";
  if (namespace === "solana" && !solanaAddress.test(v)) return "That isn’t a valid Solana mint address.";
  return null;
}
