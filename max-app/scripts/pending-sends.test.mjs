// security-review S03 (mobile): a payment whose hash MAX hasn't recorded must
// never be followed by a fresh signature, for the same payment or a new one.
// Run from max-app: npm run test:lifecycle
import { test, beforeEach } from 'node:test';
import assert from 'node:assert/strict';

import { MaxNetworkError } from '@max/sdk';

import { fake as store } from './test-stubs/expo-secure-store.mjs';
import { fakeMax } from './test-stubs/api-client.mjs';

const pending = await import('@/services/pending');
const secure = await import('@/services/secure');

const signed = (hash) => ({ namespace: 'evm', raw: `raw-${hash}`, hash });

beforeEach(() => {
  store.reset();
  fakeMax.transfers = {};
});

test('the same payment reuses its saved signed transaction instead of signing again', async () => {
  await pending.remember('send', 'pay_1', 'base', signed('0x1'));
  // e.g. a lock cleared the draft after broadcast, and the person pressed Send again
  assert.deepEqual(await pending.beforeSigning('send', 'pay_1'), signed('0x1'));
});

test('an earlier unrecorded payment is recorded before a new one is signed', async () => {
  const submitted = [];
  fakeMax.transfers.submit = async (id, hash) => submitted.push([id, hash]);
  await pending.remember('send', 'pay_old', 'base', signed('0xold'));
  assert.equal(await pending.beforeSigning('send', 'pay_new'), null);
  assert.deepEqual(submitted, [['pay_old', '0xold']]);
  assert.deepEqual(await secure.readPending(), []);
});

test('a new payment is refused while an earlier one cannot be recorded', async () => {
  fakeMax.transfers.submit = async () => {
    throw new MaxNetworkError('offline');
  };
  await pending.remember('send', 'pay_old', 'base', signed('0xold'));
  await assert.rejects(pending.beforeSigning('send', 'pay_new'), pending.UnrecordedPaymentError);
  assert.equal((await secure.readPending()).length, 1);
});

test('an unrecorded rename fee does not block a send, and vice versa', async () => {
  fakeMax.transfers.submit = async () => {
    throw new MaxNetworkError('offline');
  };
  await pending.remember('rename', 'fee_1', 'solana', { ...signed('sig'), namespace: 'solana', blockhash: 'bh', lastValidBlockHeight: 9 });
  assert.equal(await pending.beforeSigning('send', 'pay_new'), null);
});
