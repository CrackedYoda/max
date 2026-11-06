"use client";

import type { SaleExpectations, SaleView } from "@max/sdk";
import { buildVerifiedSolanaSaleTransaction } from "@max/sdk/solana";
import { Connection, PublicKey } from "@solana/web3.js";

import { getEnv } from "@/config/env";

import { isRejection, WalletError, type ConnectedWallet } from "./wallets";

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
  const { blockhash } = await conn.getLatestBlockhash("confirmed");
  // Throws AuthorizationError unless the transfers pay exactly the agreed
  // price, in the agreed asset, from this wallet. Nothing is signed then.
  const tx = buildVerifiedSolanaSaleTransaction(view, wallet.address, blockhash, { buyer: wallet.address, ...expect });
  try {
    const { signature } = await (wallet.provider as unknown as Phantom).signAndSendTransaction(tx);
    return signature;
  } catch (err) {
    throw new WalletError(
      isRejection(err) ? "Rejected in Phantom. Nothing was sent." : `Phantom couldn’t send it${view.sale.chain.includes("devnet") ? " — is Phantom set to Devnet?" : "."}`,
      isRejection(err),
    );
  }
}

function short(a: string): string {
  return a.length > 12 ? `${a.slice(0, 6)}…${a.slice(-4)}` : a;
}
