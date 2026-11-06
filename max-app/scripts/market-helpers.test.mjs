// Pure-helper checks for max-app's marketplace code (no RN, no network).
// Run from max-app: node --test scripts/market-helpers.test.mjs
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { fileURLToPath } from 'node:url';

const APP = fileURLToPath(new URL('..', import.meta.url)).replace(/\/$/, '');
const m = await import(`${APP}/src/utils/money.ts`);
const tx = await import(`${APP}/src/services/saleTx.ts`);
const env = await import(`${APP}/src/config/env.ts`);
const { createRequire } = await import('node:module');
const req = createRequire(`${APP}/package.json`);
const sdk = await import(`${APP}/node_modules/@max/sdk/dist/index.js`);
const web3 = req('@solana/web3.js');
const spl = req('@solana/spl-token');

test('seller receives 90%: price - floor(price*1000/10000), BigInt', () => {
  assert.equal(m.sellerReceives(250_000_000n, 1000), 225_000_000n);
  assert.equal(m.feeOf(1n, 1000), 0n);
  assert.equal(m.sellerReceives(1n, 1000), 1n);
  assert.equal(m.feeOf(19n, 1000), 1n); // floor(1.9)
  assert.equal(m.sellerReceives(19n, 1000), 18n);
  assert.equal(m.sellerReceives(123_456_789n, 1000), 123_456_789n - 12_345_678n);
  assert.equal(m.sellerPercent(1000), '90');
  assert.equal(m.sellerPercent(250), '97.5');
  assert.equal(m.displayUnits(m.sellerReceives(m.toBaseUnits('1234.5', 6), 1000), 6), '1,111.05');
});

test('toBaseUnits / display', () => {
  assert.equal(m.toBaseUnits('25.50', 6), 25_500_000n);
  assert.equal(m.toBaseUnits('25,5', 6), 25_500_000n);
  assert.equal(m.toBaseUnits('1.0000001', 6), null);
  assert.equal(m.toBaseUnits('abc', 6), null);
  assert.equal(m.displayDecimal('1250000.5'), '1,250,000.5');
  assert.equal(m.decimalsFrom('25.5', '25500000'), 6);
});

test('pinnedPriceBase uses trusted decimals and refuses a disagreeing server value', () => {
  assert.deepEqual(m.pinnedPriceBase('250', 6), { base: '250000000' });
  assert.deepEqual(m.pinnedPriceBase('250', 6, '250000000'), { base: '250000000' });
  assert.ok('error' in m.pinnedPriceBase('250', 6, '250000001'));
  assert.ok('error' in m.pinnedPriceBase('250', 6, '2500000000')); // API claims 7 decimals
  assert.ok('error' in m.pinnedPriceBase('0', 6));
  assert.ok('error' in m.pinnedPriceBase('1.0000001', 6));
});

test('trustedDecimals: pin cross-checked with chain', () => {
  assert.deepEqual(m.trustedDecimals(6, 6), { decimals: 6, error: null });
  assert.deepEqual(m.trustedDecimals(undefined, 6), { decimals: 6, error: null });
  assert.deepEqual(m.trustedDecimals(6, undefined), { decimals: 6, error: null });
  assert.equal(m.trustedDecimals(6, 9).decimals, null);
  assert.ok(m.trustedDecimals(6, 9).error);
});

test('INSUFFICIENT_FUNDS copy shows need vs have', () => {
  const s = m.insufficientFundsCopy({ needAsset: '250000000', haveAsset: '10000000', needNative: '5000', haveNative: '1000000' }, { decimals: 6, symbol: 'USDC' });
  assert.equal(s, 'Not enough funds: the paying wallet needs 250 USDC but holds 10 USDC.');
  const n = m.insufficientFundsCopy({ needAsset: '1', haveAsset: '5', needNative: '2039280', haveNative: '0' }, { decimals: 6, symbol: 'USDC' });
  assert.equal(n, 'Not enough funds: the paying wallet needs 0.00203928 SOL for network fees but holds 0 SOL.');
  // decimals unknown: inferred from the agreed amount
  const i = m.insufficientFundsCopy({ needAsset: '25500000', haveAsset: '0' }, { symbol: 'USDC', amount: '25.5' });
  assert.equal(i, 'Not enough funds: the paying wallet needs 25.5 USDC but holds 0 USDC.');
  assert.equal(m.insufficientFundsCopy(undefined, { symbol: 'USDC' }), null);
});

test('usd() for the registration gate', () => {
  assert.equal(m.usd(100), '$100');
  assert.equal(m.usd(12.5), '$12.50');
  assert.equal(m.usd(1234.567), '$1,234.57');
  assert.equal(m.usd('100'), null);
});

test('EXPO_PUBLIC_MARKET_ASSETS parsing', () => {
  assert.deepEqual(env.parseMarketAssets('solana-devnet:4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU:6, bad, x:y'), [
    { chain: 'solana-devnet', asset: '4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU', decimals: 6 },
    { chain: 'x', asset: 'y', decimals: undefined },
  ]);
  assert.deepEqual(env.parseMarketAssets(undefined), []);
});

test('memo lookup matches only this sale and only successful txs', () => {
  const id = '0b5c7d2e-1111-4222-8333-444455556666';
  const sigs = [
    { signature: 'failed', err: { InstructionError: [0, 'x'] }, memo: `[45] max:sale:${id}` },
    { signature: 'other', err: null, memo: '[45] max:sale:0b5c7d2e-1111-4222-8333-444455556667' },
    { signature: 'prefix', err: null, memo: `[46] max:sale:${id}x` },
    { signature: 'nomemo', err: null, memo: null },
    { signature: 'good', err: null, memo: `[3] abc; [45] max:sale:${id}` },
  ];
  assert.equal(tx.matchSaleSignature(sigs, id), 'good');
  assert.equal(tx.matchSaleSignature(sigs.slice(0, 4), id), null);
});

// A realistic sale + instructions, built like max-core does.
function fixture() {
  const buyer = web3.Keypair.generate();
  const seller = web3.Keypair.generate().publicKey;
  const treasury = web3.Keypair.generate().publicKey;
  const mint = new web3.PublicKey('4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU');
  const ata = (o) => spl.getAssociatedTokenAddressSync(mint, o, true).toBase58();
  const TOKEN = spl.TOKEN_PROGRAM_ID.toBase58(), SYS = web3.SystemProgram.programId.toBase58();
  const id = '0b5c7d2e-1111-4222-8333-444455556666';
  const sale = {
    id, maxId: 'm', buyerAddress: buyer.publicKey.toBase58(), sellerPayoutAddress: seller.toBase58(), treasuryAddress: treasury.toBase58(),
    chain: 'solana-devnet', asset: mint.toBase58(), symbol: 'USDC', price: '250', priceBaseUnits: '250000000', feeBps: 1000, fee: '25', sellerReceives: '225',
    status: 'locked', confirmations: 0, createdAt: '2026-11-06T00:00:00Z',
  };
  const create = (owner) => ({ kind: 'create_ata_idempotent', accounts: { payer: sale.buyerAddress, ata: ata(owner), owner: owner.toBase58(), mint: sale.asset, systemProgram: SYS, tokenProgram: TOKEN } });
  const xfer = (dest, amount) => ({ kind: 'transfer_checked', accounts: { source: ata(buyer.publicKey), mint: sale.asset, destination: ata(dest), authority: sale.buyerAddress }, amount, decimals: 6 });
  const instructions = { solana: [create(treasury), create(seller), xfer(treasury, '25000000'), xfer(seller, '225000000'), { kind: 'memo', memo: `max:sale:${id}` }] };
  return { buyer, sale, instructions, mint };
}

test('assertSaleInstructionsMatch with trusted expectations; refuses a different price/asset/buyer', () => {
  const { buyer, sale, instructions } = fixture();
  const expect = { buyer: buyer.publicKey.toBase58(), asset: sale.asset, priceBaseUnits: m.pinnedPriceBase('250', 6).base };
  sdk.assertSaleInstructionsMatch(sale, instructions, expect);
  assert.throws(() => sdk.assertSaleInstructionsMatch(sale, instructions, { ...expect, priceBaseUnits: '249000000' }), sdk.AuthorizationError);
  assert.throws(() => sdk.assertSaleInstructionsMatch(sale, instructions, { ...expect, asset: 'So11111111111111111111111111111111111111112' }), sdk.AuthorizationError);
  assert.throws(() => sdk.assertSaleInstructionsMatch(sale, instructions, { ...expect, buyer: web3.Keypair.generate().publicKey.toBase58() }), sdk.AuthorizationError);
  const tampered = structuredClone(instructions);
  tampered.solana[3].amount = '225000001';
  assert.throws(() => sdk.assertSaleInstructionsMatch(sale, tampered, expect), sdk.AuthorizationError);
});

test('saleInstructions keeps the API order and signs as one multi-instruction tx', () => {
  const { buyer, sale, instructions } = fixture();
  const ixs = tx.saleInstructions(instructions.solana, buyer.publicKey);
  const ATA = spl.ASSOCIATED_TOKEN_PROGRAM_ID.toBase58(), TOKEN = spl.TOKEN_PROGRAM_ID.toBase58(), MEMO = tx.MEMO_PROGRAM_ID.toBase58();
  assert.deepEqual(ixs.map((i) => i.programId.toBase58()), [ATA, ATA, TOKEN, TOKEN, MEMO]);
  // transfer_checked data: [12, u64 amount LE, decimals]
  const amt = (i) => i.data.readBigUInt64LE(1);
  assert.equal(ixs[2].data[0], 12);
  assert.equal(amt(ixs[2]), 25_000_000n);
  assert.equal(amt(ixs[3]), 225_000_000n);
  assert.equal(ixs[3].data[9], 6);
  assert.equal(ixs[4].data.toString('utf8'), `max:sale:${sale.id}`);
  // Built and signed the way chain.signSale does (fixed blockhash instead of RPC).
  const t = new web3.Transaction();
  t.add(...ixs);
  t.recentBlockhash = web3.Keypair.generate().publicKey.toBase58();
  t.feePayer = buyer.publicKey;
  t.sign(buyer);
  assert.ok(t.verifySignatures());
  assert.equal(t.instructions.length, 5);
  assert.ok(t.serialize().length < 1232);
});

test('saleInstructions refuses instructions not paid by the payer', () => {
  const { instructions } = fixture();
  const other = web3.Keypair.generate().publicKey;
  assert.throws(() => tx.saleInstructions(instructions.solana, other), /paying account/);
  assert.throws(() => tx.saleInstructions([], other), /no Solana instructions/);
  assert.throws(() => tx.saleInstructions([{ kind: 'system_transfer' }], other), /Unknown instruction/);
});

test('idempotency key rotates only for a sale that ended unpaid with no signature here', () => {
  assert.equal(tx.shouldRotateSaleKey({ status: 'expired' }, false), true);
  assert.equal(tx.shouldRotateSaleKey({ status: 'cancelled' }, false), true);
  assert.equal(tx.shouldRotateSaleKey({ status: 'expired', txHash: 'sig' }, false), false); // a payment may match late
  assert.equal(tx.shouldRotateSaleKey({ status: 'expired' }, true), false); // our signature may have landed
  for (const s of ['pending_review', 'locked', 'submitted', 'confirming', 'completed', 'manual_review', 'reversed']) {
    assert.equal(tx.shouldRotateSaleKey({ status: s }, false), false, s);
  }
});

test('sale chain must equal the tapped chain', () => {
  tx.assertSaleChain('solana-devnet', 'solana-devnet');
  assert.throws(() => tx.assertSaleChain('solana', 'solana-devnet'), /different network/);
});
