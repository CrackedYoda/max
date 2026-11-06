import assert from "node:assert/strict";
import { test, describe } from "node:test";
import { Keypair, PublicKey, VersionedTransaction } from "@solana/web3.js";
import {
  ASSOCIATED_TOKEN_PROGRAM_ID, TOKEN_PROGRAM_ID, getAssociatedTokenAddressSync,
} from "@solana/spl-token";

import { assertSaleInstructionsMatch, AuthorizationError, type MarketInstructions, type Sale } from "../src/index.js";
import { buildSolanaSaleTransaction } from "../src/solana.js";

const MINT = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v";
const MEMO = "MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr";
const k = () => Keypair.generate().publicKey.toBase58();
const ata = (o: string) => getAssociatedTokenAddressSync(new PublicKey(MINT), new PublicKey(o)).toBase58();

function fixture(opts: { createAtas?: boolean } = {}) {
  const buyer = k(), seller = k(), treasury = k();
  const sale: Sale = {
    id: "5f0c1f1e-0000-4000-8000-000000000001", maxId: "m", buyerAddress: buyer, sellerPayoutAddress: seller, treasuryAddress: treasury,
    chain: "solana", asset: MINT, symbol: "USDC", price: "100", priceBaseUnits: "100000000", feeBps: 250, fee: "2.5", sellerReceives: "97.5",
    status: "locked", confirmations: 0, createdAt: "2026-11-05T00:00:00Z",
  };
  const create = (owner: string) => ({
    kind: "create_ata_idempotent" as const,
    accounts: { payer: buyer, ata: ata(owner), owner, mint: MINT, systemProgram: "11111111111111111111111111111111", tokenProgram: TOKEN_PROGRAM_ID.toBase58() },
  });
  const transfer = (dest: string, amount: string) => ({
    kind: "transfer_checked" as const,
    accounts: { source: ata(buyer), destination: dest, mint: MINT, authority: buyer }, amount, decimals: 6,
  });
  const ins: MarketInstructions = {
    solana: [
      ...(opts.createAtas === false ? [] : [create(treasury), create(seller)]),
      transfer(ata(treasury), "2500000"), transfer(ata(seller), "97500000"),
      { kind: "memo", memo: `max:sale:${sale.id}` },
    ],
  };
  return { sale, ins, buyer, seller, treasury };
}
const clone = <T,>(v: T): T => JSON.parse(JSON.stringify(v));
const bad = (sale: Sale, ins: MarketInstructions, re: RegExp) => assert.throws(() => assertSaleInstructionsMatch(sale, ins), (e) => e instanceof AuthorizationError && re.test(e.message));

describe("assertSaleInstructionsMatch", () => {
  test("accepts the API's instructions, with or without ATA creation", () => {
    for (const createAtas of [true, false]) {
      const f = fixture({ createAtas });
      assertSaleInstructionsMatch(f.sale, f.ins);
    }
  });
  test("rejects a wrong fee", () => {
    const f = fixture(); f.ins.solana![2]!.amount = "2400000"; bad(f.sale, f.ins, /treasury/i);
  });
  test("rejects a wrong seller amount", () => {
    const f = fixture(); f.ins.solana![3]!.amount = "97400000"; bad(f.sale, f.ins, /seller/i);
  });
  test("rejects the seller leg paying someone else", () => {
    const f = fixture(); f.ins.solana![3]!.accounts!.destination = ata(k()); bad(f.sale, f.ins, /seller/i);
  });
  test("rejects the fee leg paying someone else", () => {
    const f = fixture(); f.ins.solana![2]!.accounts!.destination = ata(k()); bad(f.sale, f.ins, /treasury/i);
  });
  test("rejects a missing or wrong memo", () => {
    const f = fixture(); f.ins.solana!.pop(); bad(f.sale, f.ins, /memo/i);
    const g = fixture(); g.ins.solana![4]!.memo = "max:sale:other"; bad(g.sale, g.ins, /memo/i);
  });
  test("rejects wrong mint, decimals, source and authority", () => {
    let f = fixture(); f.ins.solana![2]!.accounts!.mint = k(); bad(f.sale, f.ins, /mint/i);
    f = fixture(); f.ins.solana![2]!.decimals = 9; bad(f.sale, f.ins, /decimals/i);
    f = fixture(); f.ins.solana![3]!.accounts!.source = ata(k()); bad(f.sale, f.ins, /buyer/i);
    f = fixture(); f.ins.solana![3]!.accounts!.authority = k(); bad(f.sale, f.ins, /buyer/i);
  });
  test("rejects extra transfers and unknown instruction kinds", () => {
    const f = fixture(); f.ins.solana!.push(clone(f.ins.solana![3]!)); bad(f.sale, f.ins, /transfer/i);
    const g = fixture(); g.ins.solana!.push({ kind: "set_authority" } as never); bad(g.sale, g.ins, /unexpected/i);
  });
  test("rejects a create-ATA that funds or creates the wrong account", () => {
    const f = fixture(); f.ins.solana![0]!.accounts!.ata = ata(k()); bad(f.sale, f.ins, /create/i);
  });
  test("rejects missing instructions and inconsistent sale amounts", () => {
    const f = fixture(); bad(f.sale, {}, /instructions/i);
    const g = fixture(); g.sale.fee = "3"; bad(g.sale, g.ins, /fee/i);
  });
});

describe("buildSolanaSaleTransaction", () => {
  test("builds a v0 transaction with the instructions in order", () => {
    const f = fixture();
    const blockhash = k(); // any 32-byte base58 string
    const tx = buildSolanaSaleTransaction(f.ins, f.buyer, blockhash);
    assert.ok(tx instanceof VersionedTransaction);
    assert.equal(tx.version, 0);
    const keys = tx.message.staticAccountKeys;
    assert.equal(keys[0]!.toBase58(), f.buyer);
    const ixs = tx.message.compiledInstructions;
    assert.equal(ixs.length, 5);
    const pid = (i: number) => keys[ixs[i]!.programIdIndex]!.toBase58();
    assert.deepEqual([0, 1, 2, 3, 4].map(pid), [
      ASSOCIATED_TOKEN_PROGRAM_ID.toBase58(), ASSOCIATED_TOKEN_PROGRAM_ID.toBase58(),
      TOKEN_PROGRAM_ID.toBase58(), TOKEN_PROGRAM_ID.toBase58(), MEMO,
    ]);
    // create-idempotent is discriminator 1; transferChecked is 12 with amount + decimals.
    assert.equal(ixs[0]!.data[0], 1);
    const t = ixs[2]!.data;
    assert.equal(t[0], 12);
    assert.equal(Buffer.from(t.subarray(1, 9)).readBigUInt64LE(), 2500000n);
    assert.equal(t[9], 6);
    assert.equal(Buffer.from(ixs[4]!.data).toString(), `max:sale:${f.sale.id}`);
  });
  test("refuses instructions whose payer is not the signer", () => {
    const f = fixture();
    assert.throws(() => buildSolanaSaleTransaction(f.ins, k(), k()), /payer|buyer/i);
  });
  test("refuses missing Solana instructions", () => {
    assert.throws(() => buildSolanaSaleTransaction({}, k(), k()), /instructions/i);
  });
});

describe("dependency-free ATA derivation", () => {
  test("matches @solana/spl-token for many owners and a mint", async () => {
    const { associatedTokenAddress } = await import("../src/solana-address.js");
    for (let i = 0; i < 60; i++) {
      const owner = k();
      assert.equal(associatedTokenAddress(owner, MINT), ata(owner));
    }
  });
});
