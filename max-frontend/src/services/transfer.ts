"use client";

import { displayHandle, type Instructions, type Payment } from "@max/sdk";
import {
  createAssociatedTokenAccountIdempotentInstruction,
  createTransferCheckedInstruction,
} from "@solana/spl-token";
import { Connection, PublicKey, SystemProgram, Transaction } from "@solana/web3.js";
import bs58 from "bs58";
import { numberToHex } from "viem";
import * as viemChains from "viem/chains";

import { getEnv } from "@/config/env";

import {
  checkEvmAttempt,
  checkSolanaAttempt,
  sendEvmOnce,
  sendSolanaOnce,
  type AttemptPurpose,
  type CheckResult,
  type Eip1193,
  type EvmReader,
  type EvmTx,
  type SendAttempt,
  type SendResult,
} from "./sendAttempts";
import { isRejection, WalletError, type ConnectedWallet } from "./wallets";

type Phantom = { signTransaction(tx: Transaction): Promise<Transaction> };

/**
 * The wallet may have broadcast the payment, but its response was lost.
 * Don't sign again: show recovery for `attempt` until the chain or MAX
 * settles what happened (see sendAttempts.ts).
 */
export class SendOutcomeUnknown extends WalletError {
  constructor(readonly attempt: SendAttempt) {
    super("Your wallet may have sent this payment. Checking before anything is sent again.");
  }
}

/**
 * Sign and broadcast exactly what MAX's instructions describe, from the wallet
 * the payment was prepared for, at most once per payment. Returns the
 * transaction hash/signature. Throws WalletError when nothing was sent (a
 * fresh try is safe) and SendOutcomeUnknown when it may have been.
 */
export async function signAndSend(payment: Payment, wallet: ConnectedWallet, purpose: AttemptPurpose, summary: string): Promise<string> {
  const ins = payment.instructions;
  if (!ins) throw new WalletError("This payment has no signing instructions.");
  if (!sameAddress(wallet.address, payment.senderAddress)) {
    throw new WalletError(`Connect ${short(payment.senderAddress)} to sign this payment.`);
  }
  const meta = { paymentId: payment.id, chain: payment.chain.key, summary, purpose };
  if (ins.evm) return sendEvm(ins.evm, wallet, meta);
  if (ins.solana) return sendSolana(ins.solana, payment.chain.key, wallet, meta);
  throw new WalletError("Unsupported payment instructions.");
}

/** "0.1 ETH to max.yoda": what a recovery screen says may have been sent. */
export function paymentSummary(p: Payment, to = displayHandle(p.recipient.handle)): string {
  const symbol = p.asset.symbol ?? (p.asset.kind === "native" ? p.chain.nativeCoin : "tokens");
  return `${p.amount.display} ${symbol} to ${to}`;
}

type Meta = { paymentId: string; chain: string; summary: string; purpose: AttemptPurpose };

function outcome(res: SendResult, rejected: string, failed: string): string {
  switch (res.kind) {
    case "sent":
      return res.txHash;
    case "not_sent":
      throw new WalletError(isRejection(res.error) ? rejected : failed, isRejection(res.error));
    case "unknown":
    case "pending":
      throw new SendOutcomeUnknown(res.attempt);
  }
}

async function sendEvm(e: NonNullable<Instructions["evm"]>, wallet: ConnectedWallet, meta: Meta): Promise<string> {
  const provider = wallet.provider as Eip1193;
  await ensureChain(provider, e.chainId, wallet.label);
  const res = await sendEvmOnce({ ...meta, sender: wallet.address, chainId: e.chainId, to: e.to, value: e.value, data: e.data }, provider);
  return outcome(res, `Rejected in ${wallet.label}. Nothing was sent.`, `${wallet.label} couldn’t send the transaction. Nothing was sent.`);
}

async function ensureChain(provider: Eip1193, chainId: number, label: string): Promise<void> {
  const current = Number(await provider.request({ method: "eth_chainId" }));
  if (current === chainId) return;
  const hex = numberToHex(chainId);
  try {
    await provider.request({ method: "wallet_switchEthereumChain", params: [{ chainId: hex }] });
  } catch (err) {
    if ((err as { code?: number }).code !== 4902) {
      throw new WalletError(isRejection(err) ? `Switch networks in ${label} to continue.` : `${label} couldn’t switch networks.`, isRejection(err));
    }
    const chain = Object.values(viemChains).find((c) => (c as { id?: number }).id === chainId) as viemChains.Chain | undefined;
    if (!chain) throw new WalletError(`Add chain ${chainId} to ${label}, then try again.`);
    await provider.request({
      method: "wallet_addEthereumChain",
      params: [{
        chainId: hex,
        chainName: chain.name,
        nativeCurrency: chain.nativeCurrency,
        rpcUrls: chain.rpcUrls.default.http,
        blockExplorerUrls: chain.blockExplorers ? [chain.blockExplorers.default.url] : undefined,
      }],
    });
  }
}

async function sendSolana(s: NonNullable<Instructions["solana"]>, chainKey: string, wallet: ConnectedWallet, meta: Meta): Promise<string> {
  const rpc = getEnv().rpc[chainKey];
  if (!rpc) throw new WalletError(`No Solana RPC configured for ${chainKey}.`);
  const from = new PublicKey(s.from);
  const tx = new Transaction();

  if (s.kind === "system_transfer") {
    tx.add(SystemProgram.transfer({ fromPubkey: from, toPubkey: new PublicKey(s.to), lamports: BigInt(s.lamports!) }));
  } else {
    const mint = new PublicKey(s.mint!);
    const program = new PublicKey(s.tokenProgram!);
    const dest = new PublicKey(s.destinationTokenAccount!);
    // Idempotent create: harmless if the recipient's token account already exists.
    tx.add(createAssociatedTokenAccountIdempotentInstruction(from, dest, new PublicKey(s.to), mint, program));
    tx.add(createTransferCheckedInstruction(new PublicKey(s.sourceTokenAccount!), mint, dest, from, BigInt(s.amount!), s.decimals!, [], program));
  }

  const conn = new Connection(rpc, "confirmed");
  const { blockhash, lastValidBlockHeight } = await conn.getLatestBlockhash("confirmed");
  tx.recentBlockhash = blockhash;
  tx.feePayer = from;
  // Sign first so the signature and bytes are saved before anything is
  // broadcast; a retry resends those same bytes.
  const res = await sendSolanaOnce(meta, {
    sign: async () => {
      const signed = await (wallet.provider as unknown as Phantom).signTransaction(tx);
      if (!signed.signature) throw new Error("Phantom returned an unsigned transaction");
      return { signature: bs58.encode(signed.signature), raw: toBase64(signed.serialize()), lastValidBlockHeight };
    },
    sendRaw: async (raw) => void (await conn.sendRawTransaction(fromBase64(raw))),
  });
  return outcome(res, "Rejected in Phantom. Nothing was sent.", `Phantom couldn’t sign it${chainKey.includes("devnet") ? ". Is Phantom set to Devnet?" : "."} Nothing was sent.`);
}

/** Ask the chain what happened to an attempt whose outcome is unknown. */
export async function checkAttempt(a: SendAttempt): Promise<CheckResult> {
  const rpc = getEnv().rpc[a.chain];
  if (!rpc) throw new Error(`No RPC configured for ${a.chain}.`);
  if (a.namespace === "solana") {
    const conn = new Connection(rpc, "confirmed");
    return checkSolanaAttempt(a, {
      hasSignature: async (sig) => !!(await conn.getSignatureStatuses([sig], { searchTransactionHistory: true })).value[0],
      blockHeight: () => conn.getBlockHeight("confirmed"),
      sendRaw: async (raw) => void (await conn.sendRawTransaction(fromBase64(raw))),
    });
  }
  return checkEvmAttempt(a, evmReader(rpc));
}

function evmReader(url: string): EvmReader {
  let id = 0;
  const call = async (method: string, params: unknown[]): Promise<unknown> => {
    const res = await fetch(url, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ jsonrpc: "2.0", id: ++id, method, params }) });
    const body = (await res.json()) as { result?: unknown; error?: { message?: string } };
    if (body.error || !res.ok) throw new Error(body.error?.message ?? `RPC ${res.status}`);
    return body.result;
  };
  return {
    nonce: async (address, tag) => Number(BigInt((await call("eth_getTransactionCount", [address, tag])) as string)),
    blockNumber: async () => Number(BigInt((await call("eth_blockNumber", [])) as string)),
    blockTransactions: async (n) => {
      const block = (await call("eth_getBlockByNumber", [numberToHex(n), true])) as { transactions?: (EvmTx & { nonce: string })[] } | null;
      return (block?.transactions ?? []).map((t) => ({ ...t, nonce: Number(BigInt(t.nonce)) }));
    },
  };
}

function toBase64(bytes: Uint8Array): string {
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s);
}

function fromBase64(b64: string): Uint8Array {
  return Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
}

function sameAddress(a: string, b: string): boolean {
  return a.startsWith("0x") ? a.toLowerCase() === b.toLowerCase() : a === b;
}

function short(a: string): string {
  return a.length > 12 ? `${a.slice(0, 6)}…${a.slice(-4)}` : a;
}
