import type { SolanaIx } from '@max/sdk';
import {
  ASSOCIATED_TOKEN_PROGRAM_ID,
  createAssociatedTokenAccountIdempotentInstruction,
  createTransferCheckedInstruction,
  TOKEN_PROGRAM_ID,
} from '@solana/spl-token';
import { PublicKey, TransactionInstruction } from '@solana/web3.js';
import { Buffer } from 'buffer';

/**
 * Marketplace sale → Solana instructions, built with this app's own
 * @solana/web3.js (not the SDK's `@max/sdk/solana` subpath, whose peer copy of
 * web3.js would be a second instance in the bundle). Callers MUST run
 * `assertSaleInstructionsMatch` with trusted expectations first: this only
 * translates, in the API's order, and refuses anything not paid by `payer`.
 * No app imports, so it can be exercised under plain Node.
 */

export const MEMO_PROGRAM_ID = new PublicKey('MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr');

function need(accounts: Record<string, string> | undefined, key: string, kind: string): PublicKey {
  const v = accounts?.[key];
  if (!v) throw new Error(`${kind} instruction is missing "${key}"`);
  return new PublicKey(v);
}

export function saleInstructions(list: SolanaIx[] | undefined, payer: PublicKey): TransactionInstruction[] {
  if (!list || list.length === 0) throw new Error('This sale has no Solana instructions.');
  const same = (k: PublicKey, what: string) => {
    if (!k.equals(payer)) throw new Error(`${what} is not the paying account.`);
  };
  return list.map((ix) => {
    switch (ix.kind) {
      case 'create_ata_idempotent': {
        same(need(ix.accounts, 'payer', ix.kind), 'Create-account payer');
        return createAssociatedTokenAccountIdempotentInstruction(
          payer,
          need(ix.accounts, 'ata', ix.kind),
          need(ix.accounts, 'owner', ix.kind),
          need(ix.accounts, 'mint', ix.kind),
          need(ix.accounts, 'tokenProgram', ix.kind),
          ASSOCIATED_TOKEN_PROGRAM_ID,
        );
      }
      case 'transfer_checked': {
        same(need(ix.accounts, 'authority', ix.kind), 'Transfer authority');
        if (ix.amount === undefined || !/^\d+$/.test(ix.amount)) throw new Error('Transfer instruction has no valid amount.');
        return createTransferCheckedInstruction(
          need(ix.accounts, 'source', ix.kind),
          need(ix.accounts, 'mint', ix.kind),
          need(ix.accounts, 'destination', ix.kind),
          payer,
          BigInt(ix.amount),
          ix.decimals ?? 0,
          [],
          TOKEN_PROGRAM_ID,
        );
      }
      case 'memo':
        if (!ix.memo) throw new Error('Memo instruction is empty.');
        return new TransactionInstruction({ programId: MEMO_PROGRAM_ID, keys: [], data: Buffer.from(ix.memo, 'utf8') });
      default:
        throw new Error(`Unknown instruction "${String((ix as { kind?: unknown }).kind)}".`);
    }
  });
}

/**
 * Whether an idempotency key that produced `sale` must be rotated: the sale
 * ended unpaid (expired / cancelled, no transaction) and this phone holds no
 * signature for it. max-core replays the sale for a reused key, so keeping the
 * key would return the dead sale forever.
 */
export function shouldRotateSaleKey(sale: { status: string; txHash?: string }, signedHere: boolean): boolean {
  return !sale.txHash && !signedHere && (sale.status === 'expired' || sale.status === 'cancelled');
}

/** The chain a sale must settle on is the one the user tapped, never the server's say-so. */
export function assertSaleChain(saleChain: string, expected: string): void {
  if (saleChain !== expected) throw new Error('This sale is on a different network than the one you chose, so nothing was signed or sent.');
}

/** The memo every sale payment carries. */
export const saleMemo = (saleId: string) => `max:sale:${saleId}`;

/**
 * This sale's payment among a wallet's recent signatures. RPCs prefix memos
 * with their length ("[45] max:sale:…"); a failed transaction moved no money.
 */
export function matchSaleSignature(sigs: { signature: string; err: unknown; memo?: string | null }[], saleId: string): string | null {
  const memo = saleMemo(saleId);
  // Several memos are joined with "; ", each as "[len] text".
  const has = (m: string) => m.split('; ').some((part) => part.replace(/^\[\d+\] /, '') === memo);
  return sigs.find((s) => !s.err && !!s.memo && has(s.memo))?.signature ?? null;
}
