import { AuthorizationError } from "./errors.js";
import type { MarketInstructions, Sale, SolanaIx } from "./market-types.js";
import {
  ASSOCIATED_TOKEN_PROGRAM,
  MEMO_PROGRAM,
  SYSTEM_PROGRAM,
  TOKEN_PROGRAM,
  associatedTokenAddress,
} from "./solana-address.js";
import { parseUnits } from "./units.js";

export { ASSOCIATED_TOKEN_PROGRAM, MEMO_PROGRAM, SYSTEM_PROGRAM, TOKEN_PROGRAM };

const fail = (msg: string): never => {
  throw new AuthorizationError(`sale instructions do not match the sale: ${msg}`);
};

/**
 * Check, before signing, that the Solana instructions the API returned pay
 * exactly what the sale says: the fee to the treasury's token account and the
 * seller's share to the seller's, both from the buyer, with the sale's memo.
 *
 * This is dependency-free. Transfers carry only token accounts, so the
 * treasury, seller and buyer token accounts are derived here from the owners
 * on the sale (`treasuryAddress`, `sellerPayoutAddress`, `buyerAddress`) and
 * the mint; a malicious API cannot point a transfer at another account and
 * still pass. The sale carries decimal fee and seller amounts, so the
 * expected base units are those parsed with the decimals the price implies,
 * and fee + seller must equal `priceBaseUnits`. Anything the API adds that is
 * not a create-ATA for one of those accounts, a transfer or the memo is
 * refused. Throws `AuthorizationError`.
 */
export function assertSaleInstructionsMatch(sale: Sale, instructions: MarketInstructions | undefined): void {
  const ixs = instructions?.solana;
  if (!ixs || ixs.length === 0) fail("no Solana instructions");
  const list = ixs as SolanaIx[];

  const mint = sale.asset;
  const decimals = decimalsOf(sale);
  let fee: bigint, seller: bigint;
  try {
    fee = parseUnits(sale.fee, decimals);
    seller = parseUnits(sale.sellerReceives, decimals);
  } catch {
    return fail("the sale's fee or seller amount is not a valid amount");
  }
  if (fee + seller !== BigInt(sale.priceBaseUnits)) fail("fee plus seller amount does not equal the price");

  let treasuryAta: string, sellerAta: string, buyerAta: string;
  try {
    treasuryAta = associatedTokenAddress(sale.treasuryAddress, mint);
    sellerAta = associatedTokenAddress(sale.sellerPayoutAddress, mint);
    buyerAta = associatedTokenAddress(sale.buyerAddress, mint);
  } catch (e) {
    return fail((e as Error).message);
  }
  if (treasuryAta === sellerAta) fail("seller and treasury share a token account");

  const owners = new Map([[treasuryAta, sale.treasuryAddress], [sellerAta, sale.sellerPayoutAddress]]);
  const creates = new Set<string>();
  let toTreasury = 0, toSeller = 0, memos = 0;

  for (const ix of list) {
    const a = ix.accounts ?? {};
    switch (ix.kind) {
      case "create_ata_idempotent": {
        const owner = owners.get(a.ata ?? "");
        if (!owner || a.owner !== owner || a.mint !== mint || a.payer !== sale.buyerAddress || a.tokenProgram !== TOKEN_PROGRAM || a.systemProgram !== SYSTEM_PROGRAM || creates.has(a.ata!)) {
          fail("create token account is not for the seller's or treasury's account");
        }
        creates.add(a.ata!);
        break;
      }
      case "transfer_checked": {
        if (a.source !== buyerAta || a.authority !== sale.buyerAddress) fail("transfer is not from the buyer");
        if (a.mint !== mint) fail("transfer mint is not the sale's asset");
        if ((ix.decimals ?? 0) !== decimals) fail("transfer decimals do not match the asset");
        if (a.destination === treasuryAta) {
          if (ix.amount !== fee.toString()) fail("treasury fee amount is wrong");
          toTreasury++;
        } else if (a.destination === sellerAta) {
          if (ix.amount !== seller.toString()) fail("seller amount is wrong");
          toSeller++;
        } else fail("transfer pays an account that is neither the treasury nor the seller");
        break;
      }
      case "memo":
        if (ix.memo !== `max:sale:${sale.id}`) fail("memo is not this sale's");
        memos++;
        break;
      default:
        fail(`unexpected instruction "${String((ix as { kind?: unknown }).kind)}"`);
    }
  }
  if (toTreasury !== 1) fail(`expected exactly one transfer to the treasury, found ${toTreasury}`);
  if (toSeller !== 1) fail(`expected exactly one transfer to the seller, found ${toSeller}`);
  if (memos !== 1) fail(`expected exactly one memo, found ${memos}`);
}

/** The asset's decimals, read off the price: `price` × 10^d = `priceBaseUnits`. */
function decimalsOf(sale: Sale): number {
  const base = BigInt(sale.priceBaseUnits);
  for (let d = 0; d <= 18; d++) {
    try {
      if (parseUnits(sale.price, d) === base) return d;
    } catch {
      /* more places than d */
    }
  }
  return fail("the sale's price does not match its base units");
}
