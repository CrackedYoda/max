import { isMaxApiError, MaxNetworkError, type Namespace } from '@max/sdk';
import type { QueryClient } from '@tanstack/react-query';
import { AppState } from 'react-native';

import { max } from '@/api/client';
import { errorMessage } from '@/error/messages';
import { walletSigner, type KeyNamespace } from '@/services/keys';
import * as passcode from '@/services/passcode';
import * as pending from '@/services/pending';
import * as secure from '@/services/secure';
import * as vault from '@/services/vault';
import { toast, useOnboarding, useRenameDraft, useSendDraft, useStepUp } from '@/store/flows';
import { getGeneration, useSession } from '@/store/session';

const S = () => useSession.getState();

/** Cold start: is there a wallet here? Never reads key material. */
export async function boot(): Promise<void> {
  try {
    const [exists, bio, label] = await Promise.all([secure.hasVault(), passcode.isBiometricEnabled(), passcode.biometricLabel()]);
    S().setBiometric(bio, label);
    S().setVault(exists ? 'locked' : 'none');
  } catch {
    // Keychain unreadable (e.g. device locked during a background launch): stay locked.
    S().setVault('locked');
  }
}

// ---- unlock / lock ----
//
// An unlock belongs to the generation it started in, captured before the
// passcode/Face ID check. lock() bumps the generation even while locked, so a
// check still running when the app goes to the background can never commit.

export async function unlockWithPasscode(code: string): Promise<passcode.VerifyResult> {
  const gen = getGeneration();
  const res = await passcode.verifyPasscode(code);
  if (res.ok) await afterUnlock(gen);
  return res;
}

export async function unlockWithBiometric(): Promise<'ok' | 'failed' | 'invalidated' | 'disabled'> {
  const gen = getGeneration();
  const res = await passcode.verifyBiometric();
  if (res === 'invalidated' || res === 'disabled') S().setBiometric(false);
  if (res === 'ok') await afterUnlock(gen);
  return res;
}

/**
 * Loads keys and marks the wallet unlocked, unless the app locked or went to
 * the background since `gen`. Not 'active': the Face ID prompt makes iOS
 * report 'inactive' while it shows.
 */
async function afterUnlock(gen: number): Promise<boolean> {
  const current = () => gen === getGeneration() && AppState.currentState !== 'background';
  const accounts = await vault.load(current);
  if (!accounts) return false;
  S().setAccounts(accounts);
  S().setVault('unlocked');
  void connectMax().then(() => resumePending(gen));
  return true;
}

/** Hands MAX the hashes of payments a lock or crash interrupted. */
async function resumePending(gen: number): Promise<void> {
  if (S().max !== 'ready' || gen !== getGeneration()) return;
  try {
    const n = await pending.resume(() => gen === getGeneration());
    if (n) toast(n === 1 ? 'A payment that was interrupted is now recorded.' : `${n} payments that were interrupted are now recorded.`);
  } catch {
    // Unreadable right now: the records stay for the next unlock.
  }
}

/**
 * Drops keys from memory and cancels any unlock still in progress. The MAX
 * session token stays, so unlocking is instant. Secret screens watch the
 * generation and erase what they show.
 */
export function lock(): void {
  const v = S().vault;
  if (v !== 'locked' && v !== 'unlocked') return;
  S().bumpGeneration();
  if (v !== 'unlocked') return;
  vault.unload();
  useSendDraft.getState().reset();
  useRenameDraft.getState().reset();
  useStepUp.getState().request?.resolve(false);
  useStepUp.getState().set(null);
  S().setVault('locked');
}

// ---- onboarding ----

export type NewWallet = { mnemonic: string } | { namespace: KeyNamespace; secret: string };

/**
 * Saves the passcode first, then the wallet: if the wallet write fails, the next
 * launch simply shows onboarding again (a passcode without a wallet is harmless;
 * a wallet without a passcode would be unopenable).
 */
export async function finishOnboarding(wallet: NewWallet, code: string, useBiometric: boolean): Promise<{ biometric: boolean }> {
  await passcode.setPasscode(code);
  await secure.clearBiometric();
  const accounts = await vault.create(wallet);
  useOnboarding.getState().clear();

  let biometric = false;
  if (useBiometric) {
    try {
      biometric = await passcode.enableBiometric();
    } catch {
      biometric = false;
    }
  }
  S().setBiometric(biometric);
  S().setAccounts(accounts);
  S().setPendingClaim(true);
  S().setVault('unlocked');
  void connectMax();
  return { biometric };
}

// ---- MAX sign-in: the phone holds the keys, so it can always re-prove ownership ----

let connecting: Promise<void> | null = null;

export function connectMax(): Promise<void> {
  if (!connecting) connecting = doConnect().finally(() => (connecting = null));
  return connecting;
}

async function doConnect(): Promise<void> {
  const gen = getGeneration();
  const stale = () => gen !== getGeneration();
  S().setMax('connecting');
  try {
    const client = max();
    let accountId: string | null = null;

    const saved = await secure.readSession();
    if (saved) {
      client.http.setSessionToken(saved.token);
      try {
        accountId = (await client.auth.me()).id;
      } catch (err) {
        if (!isMaxApiError(err)) throw err; // offline: keep the token, report unavailable
        client.http.clearSession();
        await secure.clearSession();
      }
    }
    if (stale()) return;

    if (!accountId) {
      const key = vault.primaryKey('evm') ?? vault.primaryKey('solana');
      if (!key) throw new Error('No account on this phone can sign in.');
      const res = await client.auth.login(walletSigner(key), { tokenDelivery: 'body' });
      if (!res.sessionToken) throw new Error('MAX didn’t return a session for this app.');
      if (stale()) return;
      await secure.saveSession({ v: 1, token: res.sessionToken, accountId: res.account.id });
      accountId = res.account.id;
    }

    await linkMissingWallets();
    if (stale()) return;
    S().setAccountId(accountId);
    S().setMax('ready');
  } catch (err) {
    if (stale()) return;
    S().setMax('unavailable', err instanceof MaxNetworkError ? 'Can’t reach MAX. Your wallet still works; names and payment history need a connection.' : errorMessage(err));
  }
}

/** Links every key on this phone so any of them can receive by name and sign in. */
async function linkMissingWallets(): Promise<void> {
  const client = max();
  const linked = await client.wallets.list();
  const has = (ns: Namespace, address: string) =>
    linked.some((w) => w.namespace === ns && (ns === 'evm' ? w.address.toLowerCase() === address.toLowerCase() : w.address === address));
  for (const key of vault.allKeys()) {
    if (has(key.namespace, key.address)) continue;
    try {
      await client.auth.linkWallet(walletSigner(key));
    } catch (err) {
      // Already linked to a different MAX account: leave it there, don't block the app.
      if (!isMaxApiError(err, 'WALLET_ALREADY_LINKED')) throw err;
    }
  }
}

/** After importing another account into an unlocked wallet. */
export async function refreshAccounts(): Promise<void> {
  S().setAccounts(vault.accountInfos());
  if (S().max === 'ready') {
    try {
      await linkMissingWallets();
    } catch (err) {
      S().setMax('unavailable', errorMessage(err));
    }
  }
}

// ---- step-up: "is it really you?" before signing or revealing secrets ----

/** A lock while the check is showing makes it fail: approval never outlives a lock. */
export async function stepUp(reason: string): Promise<boolean> {
  const gen = getGeneration();
  const current = () => gen === getGeneration() && S().vault === 'unlocked';
  if (S().biometricEnabled) {
    const res = await passcode.verifyBiometric();
    if (res === 'ok') return current();
    if (res === 'invalidated' || res === 'disabled') S().setBiometric(false);
  }
  if (!current()) return false;
  return new Promise<boolean>((resolve) => {
    useStepUp.getState().request?.resolve(false);
    useStepUp.getState().set({ reason, resolve: (ok) => resolve(ok && current()) });
  });
}

// ---- removal ----

/**
 * Deletes the wallet from this phone. Funds stay on-chain, reachable only by
 * the phrase. Throws a StorageError if anything stayed: when the vault itself
 * couldn't be deleted the wallet is left as it was; when only other items
 * stayed, the wallet is gone but the caller must say the cleanup was partial.
 */
export async function removeWallet(qc: QueryClient): Promise<void> {
  S().bumpGeneration();
  try {
    await max().auth.logout();
  } catch {
    max().http.clearSession();
  }
  try {
    await secure.deleteVault();
  } catch (err) {
    void connectMax(); // signed out above; the wallet is still here
    throw err;
  }
  const rest = secure.clearAll().catch((err: unknown) => err);
  vault.unload();
  qc.clear();
  useSendDraft.getState().reset();
  useRenameDraft.getState().reset();
  useOnboarding.getState().clear();
  useStepUp.getState().request?.resolve(false);
  useStepUp.getState().set(null);
  S().reset();
  const failed = await rest;
  if (failed) throw failed;
}
