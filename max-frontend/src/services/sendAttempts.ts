/**
 * Recovery for wallet sends whose outcome is unknown (security-review S03).
 *
 * A wallet or RPC can broadcast a transaction and then lose the response.
 * Signing again would pay twice, so every send is recorded here BEFORE the
 * wallet is asked to broadcast, and stays until MAX has its hash or the
 * chain shows nothing was sent:
 *
 * - Solana is signed first, so its signature and bytes are saved before
 *   broadcasting and a retry resends those same bytes. Once the blockhash has
 *   expired with no signature on-chain, it can never land.
 * - EVM wallets only sign-and-send, so the sender's nonce is saved first. If
 *   it is consumed, the transaction holding it is found on-chain; if it never
 *   moves, nothing was sent.
 *
 * This module has no runtime imports so it can be tested with plain node.
 */

export type AttemptPurpose = { kind: "send" } | { kind: "rename"; maxId: string; requestId: string };

type AttemptBase = {
  paymentId: string;
  chain: string;
  /** Shown while recovering, e.g. "0.1 ETH to max.yoda". */
  summary: string;
  purpose: AttemptPurpose;
  createdAt: number;
};

export type EvmAttempt = AttemptBase & {
  namespace: "evm";
  sender: string;
  chainId: number;
  /** The sender's pending nonce just before the wallet was asked to send. */
  nonce: number;
  /** The first block to search for it, and how far the search has got. */
  fromBlock: number;
  scannedTo?: number;
  to: string;
  value: string;
  data: string;
  /** Set once the wallet returned it. */
  txHash?: string;
};

export type SolanaAttempt = AttemptBase & {
  namespace: "solana";
  /** The signature, known before broadcasting. */
  txHash: string;
  /** The signed transaction, base64. */
  raw: string;
  lastValidBlockHeight: number;
};

export type SendAttempt = EvmAttempt | SolanaAttempt;

/** Nothing moved for this long after the attempt: the EVM wallet never sent it. */
export const EVM_QUIET_MS = 10 * 60 * 1000;
/** Blocks searched per check for the transaction that used the saved nonce. */
export const EVM_SCAN_BLOCKS = 300;

// ---- storage ----

// Per payment id. localStorage survives reloads and is shared by every tab of
// this origin; module memory backs it up when storage is missing or throws.
const memory = new Map<string, SendAttempt>();
const PREFIX = "max:send-attempt:";

const listeners = new Set<() => void>();

/** For useSyncExternalStore: called when this tab or another changes an attempt. */
export function subscribeAttempts(onChange: () => void): () => void {
  listeners.add(onChange);
  const onStorage = (e: StorageEvent) => {
    if (!e.key || e.key.startsWith(PREFIX)) onChange();
  };
  if (typeof window !== "undefined") window.addEventListener("storage", onStorage);
  return () => {
    listeners.delete(onChange);
    if (typeof window !== "undefined") window.removeEventListener("storage", onStorage);
  };
}

function notify(): void {
  for (const l of listeners) l();
}

function storage(): Storage | null {
  try {
    return typeof localStorage === "undefined" ? null : localStorage;
  } catch {
    return null;
  }
}

function valid(v: unknown): v is SendAttempt {
  const a = v as Partial<SendAttempt> | null;
  if (!a || typeof a.paymentId !== "string" || typeof a.chain !== "string" || typeof a.createdAt !== "number" || !a.purpose) return false;
  if (a.namespace === "solana") return typeof a.txHash === "string" && typeof a.raw === "string" && Number.isSafeInteger(a.lastValidBlockHeight);
  if (a.namespace === "evm") return typeof a.sender === "string" && Number.isSafeInteger(a.nonce) && Number.isSafeInteger(a.fromBlock);
  return false;
}

export function saveAttempt<T extends SendAttempt>(attempt: T): T {
  memory.set(attempt.paymentId, attempt);
  try {
    storage()?.setItem(PREFIX + attempt.paymentId, JSON.stringify(attempt));
  } catch {
    // memory still holds it
  }
  notify();
  return attempt;
}

export function loadAttempt(paymentId: string): SendAttempt | null {
  try {
    const st = storage();
    if (st) {
      const parsed: unknown = JSON.parse(st.getItem(PREFIX + paymentId) ?? "null");
      if (valid(parsed)) return parsed;
    }
  } catch {
    // fall back to memory
  }
  return memory.get(paymentId) ?? null;
}

/** Every unresolved attempt for a purpose ("send", or "rename" of one ID), oldest first. */
export function listAttempts(match: (p: AttemptPurpose) => boolean): SendAttempt[] {
  const found = new Map<string, SendAttempt>(memory);
  try {
    const st = storage();
    for (let i = 0; st && i < st.length; i++) {
      const k = st.key(i);
      if (!k?.startsWith(PREFIX)) continue;
      const parsed: unknown = JSON.parse(st.getItem(k) ?? "null");
      if (valid(parsed)) found.set(parsed.paymentId, parsed);
    }
  } catch {
    // memory only
  }
  return [...found.values()].filter((a) => match(a.purpose)).sort((a, b) => a.createdAt - b.createdAt);
}

/** Only once MAX has recorded the hash, or the chain shows nothing was sent. */
export function clearAttempt(paymentId: string): void {
  memory.delete(paymentId);
  try {
    storage()?.removeItem(PREFIX + paymentId);
  } catch {
    // nothing else to do
  }
  notify();
}

// ---- sending ----

export type SendResult =
  | { kind: "sent"; txHash: string }
  /** Nothing was broadcast (rejected, or failed before sending); a fresh try is safe. */
  | { kind: "not_sent"; error: unknown }
  /** It may have been broadcast. Recover instead of signing again. */
  | { kind: "unknown"; attempt: SendAttempt; error: unknown }
  /** An earlier attempt for this payment is unresolved. */
  | { kind: "pending"; attempt: SendAttempt };

export type Eip1193 = { request(args: { method: string; params?: unknown[] }): Promise<unknown> };

type AttemptMeta = Pick<AttemptBase, "paymentId" | "chain" | "summary" | "purpose">;

/**
 * EIP-1193's userRejectedRequest. Only this proves the wallet sent nothing:
 * any other error, whatever its message says, may follow a broadcast.
 */
export function isUserRejection(err: unknown): boolean {
  return (err as { code?: unknown } | null)?.code === 4001;
}

export async function sendEvmOnce(
  meta: AttemptMeta & { sender: string; chainId: number; to: string; value: string; data: string },
  provider: Eip1193,
  now = Date.now(),
): Promise<SendResult> {
  const existing = loadAttempt(meta.paymentId);
  if (existing) return { kind: "pending", attempt: existing };

  let attempt: EvmAttempt;
  try {
    const nonce = toNumber(await provider.request({ method: "eth_getTransactionCount", params: [meta.sender, "pending"] }));
    const fromBlock = toNumber(await provider.request({ method: "eth_blockNumber" }));
    attempt = saveAttempt({ ...meta, namespace: "evm", nonce, fromBlock, createdAt: now });
  } catch (error) {
    return { kind: "not_sent", error };
  }

  let txHash: string;
  try {
    txHash = (await provider.request({
      method: "eth_sendTransaction",
      params: [{ from: meta.sender, to: meta.to, value: meta.value, data: meta.data }],
    })) as string;
  } catch (error) {
    if (isUserRejection(error)) {
      clearAttempt(meta.paymentId);
      return { kind: "not_sent", error };
    }
    return { kind: "unknown", attempt, error };
  }
  if (!txHash) return { kind: "unknown", attempt, error: new Error("wallet returned no transaction hash") };
  saveAttempt({ ...attempt, txHash });
  return { kind: "sent", txHash };
}

export type SolanaSigned = { signature: string; raw: string; lastValidBlockHeight: number };

export async function sendSolanaOnce(
  meta: AttemptMeta,
  steps: { sign(): Promise<SolanaSigned>; sendRaw(raw: string): Promise<void> },
  now = Date.now(),
): Promise<SendResult> {
  const existing = loadAttempt(meta.paymentId);
  if (existing) return { kind: "pending", attempt: existing };

  let signed: SolanaSigned;
  try {
    signed = await steps.sign(); // signs only: nothing is broadcast yet
  } catch (error) {
    return { kind: "not_sent", error };
  }
  const attempt = saveAttempt<SolanaAttempt>({
    ...meta, namespace: "solana", txHash: signed.signature, raw: signed.raw,
    lastValidBlockHeight: signed.lastValidBlockHeight, createdAt: now,
  });
  try {
    await steps.sendRaw(signed.raw);
  } catch (error) {
    return { kind: "unknown", attempt, error };
  }
  return { kind: "sent", txHash: signed.signature };
}

// ---- reconciling ----

export type CheckResult =
  | { kind: "landed"; txHash: string }
  | { kind: "not_sent" }
  | { kind: "pending" };

export type EvmTx = { hash: string; from: string; nonce: number; to: string | null; value: string; input: string };

export interface EvmReader {
  nonce(address: string, tag: "latest" | "pending"): Promise<number>;
  blockNumber(): Promise<number>;
  blockTransactions(block: number): Promise<EvmTx[]>;
}

export async function checkEvmAttempt(a: EvmAttempt, r: EvmReader, now = Date.now()): Promise<CheckResult> {
  if (a.txHash) return { kind: "landed", txHash: a.txHash };

  if ((await r.nonce(a.sender, "latest")) > a.nonce) {
    // Something used the nonce: find it. Only the payment's own call counts.
    const head = await r.blockNumber();
    const start = Math.max(a.fromBlock, (a.scannedTo ?? a.fromBlock - 1) + 1);
    const end = Math.min(head, start + EVM_SCAN_BLOCKS - 1);
    for (let n = start; n <= end; n++) {
      const tx = (await r.blockTransactions(n)).find((t) => sameHex(t.from, a.sender) && t.nonce === a.nonce);
      if (!tx) continue;
      return matchesEvmAttempt(tx, a) ? { kind: "landed", txHash: tx.hash } : { kind: "not_sent" };
    }
    if (end >= start) saveAttempt({ ...a, scannedTo: end });
    return { kind: "pending" };
  }
  if ((await r.nonce(a.sender, "pending")) > a.nonce) return { kind: "pending" }; // waiting in the mempool
  return now - a.createdAt >= EVM_QUIET_MS ? { kind: "not_sent" } : { kind: "pending" };
}

function matchesEvmAttempt(tx: EvmTx, a: EvmAttempt): boolean {
  return sameHex(tx.to ?? "", a.to) && BigInt(tx.value || "0x0") === BigInt(a.value || "0x0") && sameHex(tx.input || "0x", a.data || "0x");
}

export interface SolanaReader {
  /** Whether the chain has the signature at all (a failed transaction counts: it landed). */
  hasSignature(signature: string): Promise<boolean>;
  blockHeight(): Promise<number>;
  sendRaw(raw: string): Promise<void>;
}

export async function checkSolanaAttempt(a: SolanaAttempt, r: SolanaReader): Promise<CheckResult> {
  if (await r.hasSignature(a.txHash)) return { kind: "landed", txHash: a.txHash };
  if ((await r.blockHeight()) > a.lastValidBlockHeight) {
    // Expired, so it can't land from now on; look once more in case it landed just before.
    return (await r.hasSignature(a.txHash)) ? { kind: "landed", txHash: a.txHash } : { kind: "not_sent" };
  }
  try {
    await r.sendRaw(a.raw); // the same bytes: harmless if they already landed
  } catch {
    // checked again next time
  }
  return { kind: "pending" };
}

function toNumber(v: unknown): number {
  const n = typeof v === "string" ? Number(BigInt(v)) : Number(v);
  if (!Number.isSafeInteger(n)) throw new Error(`unexpected RPC number ${String(v)}`);
  return n;
}

function sameHex(a: string, b: string): boolean {
  return a.toLowerCase() === b.toLowerCase();
}
