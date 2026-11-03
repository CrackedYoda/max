"use client";

import type { Instructions, Payment } from "@max/sdk";
import {
  createAssociatedTokenAccountIdempotentInstruction,
  createTransferCheckedInstruction,
} from "@solana/spl-token";
import { Connection, PublicKey, SystemProgram, Transaction } from "@solana/web3.js";
import { numberToHex } from "viem";
import * as viemChains from "viem/chains";

import { getEnv } from "@/config/env";

import { isRejection, WalletError, type ConnectedWallet } from "./wallets";

type Eip1193 = { request(args: { method: string; params?: unknown[] }): Promise<unknown> };
type Phantom = { signAndSendTransaction(tx: Transaction): Promise<{ signature: string }> };

/**
 * Sign and broadcast exactly what MAX's instructions describe, from the wallet
 * the payment was prepared for. Returns the transaction hash/signature.
 */
export async function signAndSend(payment: Payment, wallet: ConnectedWallet): Promise<string> {
  const ins = payment.instructions;
  if (!ins) throw new WalletError("This payment has no signing instructions.");
  if (!sameAddress(wallet.address, payment.senderAddress)) {
    throw new WalletError(`Connect ${short(payment.senderAddress)} to sign this payment.`);
  }
  if (ins.evm) return sendEvm(ins.evm, wallet);
  if (ins.solana) return sendSolana(ins.solana, payment.chain.key, wallet);
  throw new WalletError("Unsupported payment instructions.");
}

async function sendEvm(e: NonNullable<Instructions["evm"]>, wallet: ConnectedWallet): Promise<string> {
  const provider = wallet.provider as Eip1193;
  await ensureChain(provider, e.chainId, wallet.label);
  try {
    return (await provider.request({
      method: "eth_sendTransaction",
      params: [{ from: wallet.address, to: e.to, value: e.value, data: e.data }],
    })) as string;
  } catch (err) {
    throw new WalletError(isRejection(err) ? `Rejected in ${wallet.label}. Nothing was sent.` : `${wallet.label} couldn’t send the transaction.`, isRejection(err));
  }
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

async function sendSolana(s: NonNullable<Instructions["solana"]>, chainKey: string, wallet: ConnectedWallet): Promise<string> {
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
  tx.recentBlockhash = (await conn.getLatestBlockhash("confirmed")).blockhash;
  tx.feePayer = from;
  try {
    const { signature } = await (wallet.provider as unknown as Phantom).signAndSendTransaction(tx);
    return signature;
  } catch (err) {
    throw new WalletError(
      isRejection(err) ? "Rejected in Phantom. Nothing was sent." : `Phantom couldn’t send it${chainKey.includes("devnet") ? " — is Phantom set to Devnet?" : "."}`,
      isRejection(err),
    );
  }
}

function sameAddress(a: string, b: string): boolean {
  return a.startsWith("0x") ? a.toLowerCase() === b.toLowerCase() : a === b;
}

function short(a: string): string {
  return a.length > 12 ? `${a.slice(0, 6)}…${a.slice(-4)}` : a;
}
