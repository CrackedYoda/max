// Lock/unlock lifecycle, secure deletion and pending-send recovery (no RN, no network).
// Run from max-app: npm run test:lifecycle
import { test, beforeEach } from 'node:test';
import assert from 'node:assert/strict';

import { MaxApiError, MaxNetworkError } from '@max/sdk';

import { AppState } from './test-stubs/react-native.mjs';
import { fake as store } from './test-stubs/expo-secure-store.mjs';
import { fakeMax } from './test-stubs/api-client.mjs';
import { fakeKeys } from './test-stubs/keys.mjs';
import { fakePasscode } from './test-stubs/passcode.mjs';

const session = await import('@/services/session');
const secure = await import('@/services/secure');
const vault = await import('@/services/vault');
const pending = await import('@/services/pending');
const { useSession } = await import('@/store/session');
const { useStepUp, useSendDraft } = await import('@/store/flows');

const S = () => useSession.getState();
const initial = useSession.getState();
const fakeQc = { clear() {} };

function deferred() {
  let resolve;
  const promise = new Promise((r) => (resolve = r));
  return { promise, resolve };
}

const signed = (hash) => ({ hash, raw: `raw-${hash}`, namespace: 'evm' });

beforeEach(async () => {
  store.reset();
  vault.unload();
  useSession.setState({ ...initial, vault: 'locked' });
  useStepUp.setState({ request: null });
  AppState.currentState = 'active';
  fakePasscode.verify = async () => ({ ok: true });
  fakePasscode.verifyBiometric = async () => 'ok';
  fakeKeys.derive = async () => ({ evm: 'evm-secret', solanaSeed: 'sol-seed' });
  fakeMax.transfers = {};
  fakeMax.auth = { logout: async () => {} };
  await secure.saveVault({ v: 1, mnemonic: 'test phrase', imported: [] });
});

// ---- S09: a late unlock never undoes a lock ----

test('unlock commits when nothing interrupts it', async () => {
  const res = await session.unlockWithPasscode('123456');
  assert.equal(res.ok, true);
  assert.equal(S().vault, 'unlocked');
  assert.equal(vault.isLoaded(), true);
});

test('Face ID prompt (iOS reports inactive) still unlocks', async () => {
  AppState.currentState = 'inactive';
  assert.equal(await session.unlockWithBiometric(), 'ok');
  assert.equal(S().vault, 'unlocked');
});

test('backgrounded while the passcode is verifying: stays locked, no keys in memory', async () => {
  const check = deferred();
  fakePasscode.verify = () => check.promise;
  const unlocking = session.unlockWithPasscode('123456');
  AppState.currentState = 'background';
  session.lock(); // what useAppLock does on 'background'
  check.resolve({ ok: true });
  await unlocking;
  assert.equal(S().vault, 'locked');
  assert.equal(vault.isLoaded(), false);
});

test('backgrounded during Face ID: stays locked', async () => {
  const check = deferred();
  fakePasscode.verifyBiometric = () => check.promise;
  const unlocking = session.unlockWithBiometric();
  session.lock();
  check.resolve('ok');
  await unlocking;
  assert.equal(S().vault, 'locked');
  assert.equal(vault.isLoaded(), false);
});

test('locked during key derivation: derived keys are never installed', async () => {
  const derive = deferred();
  fakeKeys.derive = () => derive.promise;
  const unlocking = session.unlockWithPasscode('123456');
  await new Promise((r) => setImmediate(r)); // let it reach derivation
  session.lock();
  derive.resolve({ evm: 'evm-secret', solanaSeed: 'sol-seed' });
  await unlocking;
  assert.equal(S().vault, 'locked');
  assert.equal(vault.isLoaded(), false);
});

test('in the background with no lock event yet: a finished check does not unlock', async () => {
  const check = deferred();
  fakePasscode.verify = () => check.promise;
  const unlocking = session.unlockWithPasscode('123456');
  AppState.currentState = 'background';
  check.resolve({ ok: true });
  await unlocking;
  assert.equal(S().vault, 'locked');
  assert.equal(vault.isLoaded(), false);
});

test('a cancelled unlock finishing late never unloads a newer unlock', async () => {
  const derive = deferred();
  fakeKeys.derive = () => derive.promise;
  const stale = session.unlockWithPasscode('123456');
  await new Promise((r) => setImmediate(r));
  session.lock();
  fakeKeys.derive = async () => ({ evm: 'evm-secret', solanaSeed: 'sol-seed' });
  await session.unlockWithPasscode('123456');
  derive.resolve({ evm: 'evm-secret', solanaSeed: 'sol-seed' });
  await stale;
  assert.equal(S().vault, 'unlocked');
  assert.equal(vault.isLoaded(), true);
});

test('lock while already locked still cancels, but outside a wallet it does nothing', () => {
  const gen = S().generation;
  session.lock();
  assert.equal(S().generation, gen + 1);
  useSession.setState({ vault: 'none' });
  session.lock();
  assert.equal(S().generation, gen + 1);
});

// ---- lock revokes step-up and clears drafts ----

test('lock fails a step-up that is showing, even if it is approved afterwards', async () => {
  await session.unlockWithPasscode('123456');
  const asking = session.stepUp('Reveal');
  const req = useStepUp.getState().request;
  assert.ok(req);
  session.lock();
  assert.equal(useStepUp.getState().request, null);
  req.resolve(true); // a stale approval arriving late
  assert.equal(await asking, false);
});

test('Face ID step-up that returns after a lock is refused', async () => {
  await session.unlockWithPasscode('123456');
  useSession.setState({ biometricEnabled: true });
  const check = deferred();
  fakePasscode.verifyBiometric = () => check.promise;
  const asking = session.stepUp('Sign');
  session.lock();
  check.resolve('ok');
  assert.equal(await asking, false);
});

test('lock clears the send draft and its signature', async () => {
  await session.unlockWithPasscode('123456');
  useSendDraft.getState().setSigned(signed('0xabc'));
  session.lock();
  assert.equal(useSendDraft.getState().signed, null);
});

// ---- secure deletion ----

test('deleting an item that is already gone is fine', async () => {
  store.failDelete.add('max.session.v1');
  store.deleteError = 'The specified item could not be found in the keychain.';
  await secure.clearSession();
});

test('a real storage failure on delete is reported', async () => {
  await secure.saveSession({ v: 1, token: 't', accountId: 'a' });
  store.failDelete.add('max.session.v1');
  await assert.rejects(secure.clearSession(), secure.StorageError);
});

test('a delete that silently left the item behind is reported', async () => {
  await secure.saveSession({ v: 1, token: 't', accountId: 'a' });
  store.keepOnDelete.add('max.session.v1');
  await assert.rejects(secure.clearSession(), secure.StorageError);
});

test('clearAll tries every item, then reports the failure', async () => {
  await secure.saveSession({ v: 1, token: 't', accountId: 'a' });
  store.failDelete.add('max.passcode.v1');
  await secure.savePasscodeRecord({ v: 1, salt: 'x'.repeat(16), verifier: 'y'.repeat(16) });
  await assert.rejects(secure.clearAll(), /one item/);
  assert.equal(await secure.hasVault(), false);
  assert.equal(await secure.readSession(), null);
});

test('removeWallet keeps the wallet when the vault itself could not be deleted', async () => {
  await session.unlockWithPasscode('123456');
  store.failDelete.add('max.vault.v1');
  await assert.rejects(session.removeWallet(fakeQc), secure.StorageError);
  assert.equal(await secure.hasVault(), true);
  assert.equal(S().vault, 'unlocked');
  assert.equal(vault.isLoaded(), true);
});

test('removeWallet with only leftovers: wallet gone, partial cleanup still reported', async () => {
  await session.unlockWithPasscode('123456');
  await secure.saveSession({ v: 1, token: 't', accountId: 'a' });
  store.keepOnDelete.add('max.session.v1');
  await assert.rejects(session.removeWallet(fakeQc), secure.StorageError);
  assert.equal(await secure.hasVault(), false);
  assert.equal(S().vault, 'none');
  assert.equal(vault.isLoaded(), false);
});

// ---- pending sends survive a lock or crash ----

test('a remembered hash is kept in secure storage until forgotten', async () => {
  await pending.remember('send', 'pay_1', 'base', signed('0x1'));
  await pending.remember('rename', 'pay_2', 'solana', { ...signed('sig2'), namespace: 'solana', blockhash: 'bh', lastValidBlockHeight: 9 });
  assert.deepEqual((await secure.readPending()).map((p) => [p.kind, p.paymentId, p.signed.hash]), [
    ['send', 'pay_1', '0x1'],
    ['rename', 'pay_2', 'sig2'],
  ]);
  await pending.forget('pay_1');
  assert.deepEqual((await secure.readPending()).map((p) => p.paymentId), ['pay_2']);
  await pending.forget('pay_2');
  assert.equal(store.items.size, 1); // only the vault: an empty list deletes the item
});

test('concurrent remembers do not overwrite each other', async () => {
  await Promise.all(['a', 'b', 'c'].map((id) => pending.remember('send', id, 'base', signed(`0x${id}`))));
  assert.deepEqual((await secure.readPending()).map((p) => p.paymentId).sort(), ['a', 'b', 'c']);
});

test('the pending record outlives a lock', async () => {
  await session.unlockWithPasscode('123456');
  await pending.remember('send', 'pay_1', 'base', signed('0x1'));
  session.lock();
  assert.equal((await secure.readPending()).length, 1);
});

test('resume hands MAX the known hash and forgets it once recorded', async () => {
  const submitted = [];
  fakeMax.transfers.submit = async (id, hash) => submitted.push([id, hash]);
  await pending.remember('send', 'pay_1', 'base', signed('0x1'));
  assert.equal(await pending.resume(() => true), 1);
  assert.deepEqual(submitted, [['pay_1', '0x1']]);
  assert.deepEqual(await secure.readPending(), []);
});

test('resume keeps the hash while MAX is unreachable', async () => {
  fakeMax.transfers.submit = async () => {
    throw new MaxNetworkError('offline');
  };
  await pending.remember('send', 'pay_1', 'base', signed('0x1'));
  assert.equal(await pending.resume(() => true), 0);
  assert.equal((await secure.readPending()).length, 1);
});

test('resume forgets when MAX refuses but already has a hash, keeps it otherwise', async () => {
  fakeMax.transfers.submit = async () => {
    throw new MaxApiError('CONFLICT', 'nope', 409);
  };
  fakeMax.transfers.get = async (id) => (id === 'done' ? { status: 'SUBMITTED', txHash: '0x1' } : { status: 'AWAITING_SENDER_SIGNATURE' });
  await pending.remember('send', 'done', 'base', signed('0x1'));
  await pending.remember('send', 'open', 'base', signed('0x2'));
  assert.equal(await pending.resume(() => true), 1);
  assert.deepEqual((await secure.readPending()).map((p) => p.paymentId), ['open']);
});

test('resume stops when the app locks', async () => {
  let current = true;
  fakeMax.transfers.submit = async () => {
    current = false;
  };
  await pending.remember('send', 'a', 'base', signed('0xa'));
  await pending.remember('send', 'b', 'base', signed('0xb'));
  assert.equal(await pending.resume(() => current), 1);
  assert.deepEqual((await secure.readPending()).map((p) => p.paymentId), ['b']);
});

test('removing the wallet also drops pending records', async () => {
  await session.unlockWithPasscode('123456');
  await pending.remember('send', 'pay_1', 'base', signed('0x1'));
  await session.removeWallet(fakeQc);
  assert.deepEqual(await secure.readPending(), []);
});
