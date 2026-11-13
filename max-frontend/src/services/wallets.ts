"use client";

import type { WalletSigner } from "@max/sdk";
import bs58 from "bs58";
import { toHex } from "viem";

// ---- provider shapes (only what MAX uses) ----

type Eip1193 = { request(args: { method: string; params?: unknown[] | object }): Promise<unknown> };

type PhantomSolana = {
  isPhantom?: boolean;
  publicKey?: { toBase58(): string } | null;
  connect(opts?: { onlyIfTrusted?: boolean }): Promise<{ publicKey: { toBase58(): string } }>;
  disconnect(): Promise<void>;
  signMessage(message: Uint8Array, display?: "utf8"): Promise<{ signature: Uint8Array }>;
  signAndSendTransaction(tx: unknown): Promise<{ signature: string }>;
};

type Eip6963Detail = { info: { uuid: string; name: string; icon: string; rdns: string }; provider: Eip1193 };

declare global {
  interface Window {
    phantom?: { solana?: PhantomSolana };
    solana?: PhantomSolana;
    ethereum?: Eip1193;
  }
}

export type WalletKind = "phantom" | "evm";

export type ConnectedWallet = WalletSigner & {
  kind: WalletKind;
  label: string;
  /** Underlying provider, for signing transactions. */
  provider: PhantomSolana | Eip1193;
};

/** A user-facing failure; `rejected` means the user declined in their wallet. */
export class WalletError extends Error {
  constructor(message: string, readonly rejected = false) {
    super(message);
  }
}

function isRejection(err: unknown): boolean {
  const e = err as { code?: number; message?: string } | undefined;
  return e?.code === 4001 || /reject|denied|cancel/i.test(e?.message ?? "");
}

/** The wallet's own reason, appended to our message so failures can be diagnosed. */
function walletReason(err: unknown): string {
  console.error("wallet error", err);
  const m = (err as { message?: unknown } | undefined)?.message;
  return typeof m === "string" && m.trim() ? `: ${m.trim().replace(/\.$/, "")}` : "";
}

// ---- discovery ----

export function phantomProvider(): PhantomSolana | null {
  if (typeof window === "undefined") return null;
  const p = window.phantom?.solana ?? window.solana;
  return p?.isPhantom ? p : null;
}

export type EvmProviderInfo = { id: string; name: string; icon?: string; provider: Eip1193 };

/** EIP-6963 discovery, falling back to window.ethereum. */
export function discoverEvmProviders(onChange: (list: EvmProviderInfo[]) => void): () => void {
  if (typeof window === "undefined") return () => {};
  const found = new Map<string, EvmProviderInfo>();
  const emit = () => onChange([...found.values()]);
  const handler = (e: Event) => {
    const d = (e as CustomEvent<Eip6963Detail>).detail;
    if (!d?.info?.uuid) return;
    found.set(d.info.uuid, { id: d.info.rdns || d.info.uuid, name: d.info.name, icon: d.info.icon, provider: d.provider });
    emit();
  };
  window.addEventListener("eip6963:announceProvider", handler);
  window.dispatchEvent(new Event("eip6963:requestProvider"));
  const fallback = setTimeout(() => {
    if (found.size === 0 && window.ethereum) {
      found.set("injected", { id: "injected", name: "Browser wallet", provider: window.ethereum });
      emit();
    }
  }, 300);
  return () => {
    window.removeEventListener("eip6963:announceProvider", handler);
    clearTimeout(fallback);
  };
}

// ---- connect ----

export async function connectPhantom(): Promise<ConnectedWallet> {
  const p = phantomProvider();
  if (!p) throw new WalletError("Phantom isn’t installed in this browser.");
  let address: string;
  try {
    address = (await p.connect()).publicKey.toBase58();
  } catch (err) {
    throw new WalletError(isRejection(err) ? "Connection was cancelled in Phantom." : "Couldn’t connect to Phantom.", isRejection(err));
  }
  return {
    kind: "phantom",
    label: "Phantom",
    namespace: "solana",
    address,
    provider: p,
    signMessage: async (message) => {
      try {
        const { signature } = await p.signMessage(new TextEncoder().encode(message), "utf8");
        return bs58.encode(signature);
      } catch (err) {
        throw new WalletError(isRejection(err) ? "Signature was rejected in Phantom. Nothing was signed." : `Phantom couldn’t sign the message${walletReason(err)}.`, isRejection(err));
      }
    },
  };
}

export async function connectEvm(info: EvmProviderInfo): Promise<ConnectedWallet> {
  let accounts: string[];
  try {
    accounts = (await info.provider.request({ method: "eth_requestAccounts" })) as string[];
  } catch (err) {
    throw new WalletError(isRejection(err) ? `Connection was cancelled in ${info.name}.` : `Couldn’t connect to ${info.name}.`, isRejection(err));
  }
  const address = accounts[0];
  if (!address) throw new WalletError(`${info.name} returned no account.`);
  return {
    kind: "evm",
    label: info.name,
    namespace: "evm",
    address,
    provider: info.provider,
    signMessage: async (message) => {
      try {
        return (await info.provider.request({ method: "personal_sign", params: [toHex(message), address] })) as string;
      } catch (err) {
        throw new WalletError(isRejection(err) ? `Signature was rejected in ${info.name}. Nothing was signed.` : `${info.name} couldn’t sign the message${walletReason(err)}.`, isRejection(err));
      }
    },
  };
}

export { isRejection };

/**
 * Connects a browser wallet for `namespace`: Phantom for Solana, MetaMask (or
 * the first EIP-6963 wallet) for EVM. Reuses `current` when it already fits.
 */
export async function connectFor(namespace: string, evm: EvmProviderInfo[], current?: ConnectedWallet | null): Promise<ConnectedWallet> {
  if (current && current.namespace === namespace) return current;
  if (namespace === "solana") return connectPhantom();
  const p = evm.find((x) => /metamask/i.test(x.name)) ?? evm[0];
  if (!p) throw new WalletError("No EVM wallet found. Install MetaMask to continue.");
  return connectEvm(p);
}

const short = (a: string) => (a.length > 12 ? `${a.slice(0, 4)}…${a.slice(-4)}` : a);
const sameAddress = (ns: string, a: string, b: string) => (ns === "evm" ? a.toLowerCase() === b.toLowerCase() : a === b);

/**
 * A wallet linked to the signed-in account, ready to sign: the one already
 * connected in this tab when it is linked, otherwise Phantom or the first EVM
 * wallet, whichever kind the account has linked.
 */
export async function connectLinkedWallet(
  linked: { namespace: string; address: string }[],
  evm: EvmProviderInfo[],
  current?: ConnectedWallet | null,
): Promise<ConnectedWallet> {
  const isLinked = (w: ConnectedWallet) => linked.some((x) => x.namespace === w.namespace && sameAddress(w.namespace, x.address, w.address));
  if (current && isLinked(current)) return current;
  const hasSol = linked.some((x) => x.namespace === "solana");
  const hasEvm = linked.some((x) => x.namespace === "evm");
  let w: ConnectedWallet;
  if (hasSol && phantomProvider()) w = await connectPhantom();
  else if (hasEvm && evm.length) w = await connectFor("evm", evm);
  else throw new WalletError("Open a wallet that’s linked to your account (Phantom or MetaMask) to sign.");
  if (!isLinked(w)) {
    const want = linked.find((x) => x.namespace === w.namespace) ?? linked[0];
    throw new WalletError(`${w.label} is on ${short(w.address)}, which isn’t linked to your account.${want ? ` Switch to ${short(want.address)}.` : ""}`);
  }
  return w;
}
