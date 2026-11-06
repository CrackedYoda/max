// `@max/sdk/solana`: turns a sale's instructions into a Solana transaction.
// This subpath needs the optional peer dependencies @solana/web3.js and
// @solana/spl-token; the main entry does not.
import {
  PublicKey,
  TransactionInstruction,
  TransactionMessage,
  VersionedTransaction,
} from "@solana/web3.js";
import {
  ASSOCIATED_TOKEN_PROGRAM_ID,
  TOKEN_PROGRAM_ID,
  createAssociatedTokenAccountIdempotentInstruction,
  createTransferCheckedInstruction,
} from "@solana/spl-token";

import type { MarketInstructions } from "./market-types.js";
import { MEMO_PROGRAM } from "./solana-address.js";

export { assertSaleInstructionsMatch } from "./sale-instructions.js";

const MEMO_PROGRAM_ID = new PublicKey(MEMO_PROGRAM);

function need(accounts: Record<string, string> | undefined, key: string, kind: string): PublicKey {
  const v = accounts?.[key];
  if (!v) throw new Error(`${kind} instruction is missing "${key}"`);
  return new PublicKey(v);
}

/**
 * Build the unsigned v0 transaction for a sale from the API's `instructions`,
 * in the API's order: create-ATA-idempotent (as needed), the fee transfer, the
 * seller transfer, then the memo. `payer` is the buyer and must be the account
 * the instructions pay from. Run `assertSaleInstructionsMatch` first, then
 * sign with the buyer's wallet.
 */
export function buildSolanaSaleTransaction(ins: MarketInstructions, payer: string, recentBlockhash: string): VersionedTransaction {
  const list = ins.solana;
  if (!list || list.length === 0) throw new Error("sale has no Solana instructions");
  const payerKey = new PublicKey(payer);
  const same = (k: PublicKey, what: string) => {
    if (!k.equals(payerKey)) throw new Error(`${what} is not the payer (buyer)`);
  };

  const instructions = list.map((ix): TransactionInstruction => {
    switch (ix.kind) {
      case "create_ata_idempotent": {
        same(need(ix.accounts, "payer", ix.kind), "create-ATA payer");
        const tokenProgram = need(ix.accounts, "tokenProgram", ix.kind);
        return createAssociatedTokenAccountIdempotentInstruction(
          payerKey,
          need(ix.accounts, "ata", ix.kind),
          need(ix.accounts, "owner", ix.kind),
          need(ix.accounts, "mint", ix.kind),
          tokenProgram,
          ASSOCIATED_TOKEN_PROGRAM_ID,
        );
      }
      case "transfer_checked": {
        same(need(ix.accounts, "authority", ix.kind), "transfer authority");
        if (ix.amount === undefined) throw new Error("transfer_checked instruction is missing the amount");
        return createTransferCheckedInstruction(
          need(ix.accounts, "source", ix.kind),
          need(ix.accounts, "mint", ix.kind),
          need(ix.accounts, "destination", ix.kind),
          payerKey,
          BigInt(ix.amount),
          ix.decimals ?? 0,
          [],
          TOKEN_PROGRAM_ID,
        );
      }
      case "memo":
        if (!ix.memo) throw new Error("memo instruction is empty");
        return new TransactionInstruction({ programId: MEMO_PROGRAM_ID, keys: [], data: Buffer.from(ix.memo, "utf8") });
      default:
        throw new Error(`unknown instruction kind "${String((ix as { kind?: unknown }).kind)}"`);
    }
  });

  return new VersionedTransaction(new TransactionMessage({ payerKey, recentBlockhash, instructions }).compileToV0Message());
}
