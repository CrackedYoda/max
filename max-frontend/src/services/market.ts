"use client";

import type { SaleExpectations, SaleView } from "@max/sdk";
import { buildVerifiedSolanaSaleTransaction } from "@max/sdk/solana";
import { Connection, PublicKey } from "@solana/web3.js";

import { getEnv } from "@/config/env";

import { clearAttempt, saveAttempt, type SignAttempt } from "./saleAttempts";
import { isRejection, WalletError, type ConnectedWallet } from "./wallets";

export type { SignAttempt };

type Phantom = { signAndSendTransaction(tx: unknown): Promise<{ signature: string }> };

function connection(chain: string): Connection {
  const rpc = getEnv().rpc[chain];
  if (!rpc) throw new WalletError(`No Solana RPC configured for ${chain}.`);
  return new Connection(rpc, "confirmed");
}

/** A mint's decimals, read from the chain itself rather than from MAX. */
export async function mintDecimals(chain: string, mint: string): Promise<number> {
  const supply = await connection(chain).getTokenSupply(new PublicKey(mint));
  return supply.value.decimals;
}

/**
 * Verify the sale's instructions against what the user agreed to, then sign
 * and broadcast them with the connected wallet. `expect` must come from the
 * UI (the listing or offer the user clicked), never from the sale response.
 * Returns the transaction signature.
 */
export async function signSale(view: SaleView, wallet: ConnectedWallet, expect: Required<Pick<SaleExpectations, "asset" | "priceBaseUnits">>): Promise<string> {
  if (wallet.namespace !== "solana") throw new WalletError("Connect a Solana wallet to pay.");
  if (wallet.address !== view.sale.buyerAddress) {
    throw new WalletError(`Connect ${short(view.sale.buyerAddress)} in Phantom to pay for this sale.`);
  }
  const conn = connection(view.sale.chain);
  const { blockhash, lastValidBlockHeight } = await conn.getLatestBlockhash("confirmed");
  // Throws AuthorizationError unless the transfers pay exactly the agreed
  // price, in the agreed asset, from this wallet. Nothing is signed then.
  // With NEXT_PUBLIC_MARKET_TREASURY set, the fee must go to that treasury.
  const treasury = getEnv().marketTreasury;
  const tx = buildVerifiedSolanaSaleTransaction(view, wallet.address, blockhash, { buyer: wallet.address, ...expect, ...(treasury ? { treasury } : {}) });
  // Persist the attempt before the wallet sees it: if the dialog closes or the
  // tab reloads mid-send, the next open must wait this blockhash out.
  saveAttempt(view.sale.id, { chain: view.sale.chain, blockhash, lastValidBlockHeight });
  try {
    const { signature } = await (wallet.provider as unknown as Phantom).signAndSendTransaction(tx);
    return signature;
  } catch (err) {
    if (isRejection(err)) {
      clearAttempt(view.sale.id); // declined in the wallet: nothing was signed or sent
      throw new WalletError("Rejected in Phantom. Nothing was sent.", true);
    }
    // Anything else may have happened after the broadcast: never treat it as "not paid".
    throw new MaybeSentError(
      `Phantom reported an error${view.sale.chain.includes("devnet") ? " (is Phantom set to Devnet?)" : ""}, but the payment may still have been sent.`,
      { chain: view.sale.chain, blockhash, lastValidBlockHeight },
    );
  }
}

/** The wallet failed after signing started: the transaction may or may not be on chain. */
export class MaybeSentError extends Error {
  override readonly name = "MaybeSentError";
  constructor(message: string, readonly attempt: SignAttempt) {
    super(message);
  }
}

/**
 * Whether a signed attempt can no longer land: the finalized block height is
 * past its lastValidBlockHeight. Throws when the RPC can't be asked.
 */
export async function attemptExpired(attempt: SignAttempt): Promise<boolean> {
  const height = await connection(attempt.chain).getBlockHeight("finalized");
  return height > attempt.lastValidBlockHeight;
}

/**
 * Look for this sale's payment among the buyer's recent transactions (the
 * memo is `max:sale:<id>`). Returns its signature, or null if none is found.
 * Throws when the RPC can't be asked.
 */
export async function findSaleSignature(view: SaleView): Promise<string | null> {
  const memo = `max:sale:${view.sale.id}`;
  const sigs = await connection(view.sale.chain).getSignaturesForAddress(new PublicKey(view.sale.buyerAddress), { limit: 20 }, "confirmed");
  // The RPC prefixes memos with their length ("[45] max:sale:…"). Failed txs moved no money.
  return sigs.find((s) => !s.err && s.memo?.includes(memo))?.signature ?? null;
}

function short(a: string): string {
  return a.length > 12 ? `${a.slice(0, 6)}…${a.slice(-4)}` : a;
}
