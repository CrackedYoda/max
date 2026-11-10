import type { Namespace } from "./types.js";

const B58 = "[1-9A-HJ-NP-Za-km-z]";
const BECH32 = "[02-9ac-hj-np-z]";

const shapes: Record<Namespace, RegExp[]> = {
  evm: [/^0x[0-9a-fA-F]{40}$/],
  solana: [new RegExp(`^${B58}{32,44}$`)],
  bitcoin: [new RegExp(`^[13]${B58}{25,34}$`), new RegExp(`^bc1${BECH32}{11,71}$`, "i")],
  litecoin: [new RegExp(`^[LM3]${B58}{26,33}$`), new RegExp(`^ltc1${BECH32}{11,71}$`, "i")],
  tron: [new RegExp(`^T${B58}{33}$`)],
};

/**
 * Cheap, offline shape check for a pasted address, for live form feedback.
 * The API is authoritative: it verifies checksums and rejects typos.
 */
export function looksLikeAddress(ns: Namespace, address: string): boolean {
  const a = address.trim();
  return (shapes[ns] ?? []).some((re) => re.test(a));
}
