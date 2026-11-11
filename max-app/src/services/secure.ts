import * as SecureStore from 'expo-secure-store';
import { z } from 'zod';

/**
 * The ONLY module that touches SecureStore (Noma pattern). Every item is
 * WHEN_UNLOCKED_THIS_DEVICE_ONLY: unreadable while the phone is locked and never
 * migrated to another device through a backup. Records are zod-validated on read;
 * a corrupt record is treated as absent.
 *
 * The vault (recovery phrase / imported keys) is read into memory only after the
 * in-app passcode or Face ID check, and dropped again on lock.
 */

const BASE_OPTS: SecureStore.SecureStoreOptions = {
  keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY,
};

// requireAuthentication needs its own service: the key must not be shared with
// non-authenticated items.
const BIO_OPTS: SecureStore.SecureStoreOptions = {
  ...BASE_OPTS,
  keychainService: 'max.biometric',
  requireAuthentication: true,
  authenticationPrompt: 'Unlock MAX',
};

const KEYS = {
  vault: 'max.vault.v1',
  session: 'max.session.v1',
  passcode: 'max.passcode.v1',
  lockout: 'max.lockout.v1',
  bioFlag: 'max.bio_enabled.v1',
  bioSecret: 'max.bio_secret.v1',
  pending: 'max.pending.v1',
} as const;

const importedKeySchema = z.object({
  id: z.string().min(1),
  namespace: z.enum(['evm', 'solana']),
  /** EVM: 0x-hex secp256k1 key. Solana: base58 64-byte secret key. */
  secret: z.string().min(1),
});
export type ImportedKey = z.infer<typeof importedKeySchema>;

export const vaultSchema = z.object({
  v: z.literal(1),
  mnemonic: z.string().nullable(),
  imported: z.array(importedKeySchema),
});
export type VaultRecord = z.infer<typeof vaultSchema>;

const sessionSchema = z.object({
  v: z.literal(1),
  token: z.string().min(1),
  accountId: z.string().min(1),
});
export type SessionRecord = z.infer<typeof sessionSchema>;

const passcodeSchema = z.object({ v: z.literal(1), salt: z.string().min(16), verifier: z.string().min(16) });
export type PasscodeRecord = z.infer<typeof passcodeSchema>;

const lockoutSchema = z.object({ failures: z.number().int().nonnegative(), lockedUntil: z.number().int().nonnegative() });
export type LockoutState = z.infer<typeof lockoutSchema>;

export class StorageError extends Error {
  override name = 'StorageError';
}

async function readJson<T>(key: string, schema: z.ZodType<T>): Promise<T | null> {
  const raw = await SecureStore.getItemAsync(key, BASE_OPTS);
  if (raw == null) return null;
  try {
    const parsed = schema.safeParse(JSON.parse(raw));
    if (parsed.success) return parsed.data;
  } catch {
    // fall through
  }
  // Never silently delete the vault: a parse failure there must surface, not lose funds.
  if (key !== KEYS.vault) await SecureStore.deleteItemAsync(key, BASE_OPTS);
  return null;
}

async function writeJson(key: string, value: unknown): Promise<void> {
  try {
    await SecureStore.setItemAsync(key, JSON.stringify(value), BASE_OPTS);
  } catch {
    throw new StorageError('Couldn’t save to this phone’s secure storage.');
  }
}

/** Keychain/Keystore wording for "there was nothing to delete". */
const MISSING_ITEM = /not found|could not be found|doesn['’]t exist|does not exist|errSecItemNotFound|-25300/i;

/**
 * Deletes one item and makes sure it's gone. A missing item counts as deleted;
 * any other failure throws, so callers never report a clean state while the
 * item is still on the phone. Items behind requireAuthentication can't be read
 * back without a biometric prompt, so only the delete call vouches for them.
 */
async function safeDelete(key: string, opts: SecureStore.SecureStoreOptions): Promise<void> {
  try {
    await SecureStore.deleteItemAsync(key, opts);
  } catch (err) {
    if (!MISSING_ITEM.test(err instanceof Error ? err.message : String(err))) {
      throw new StorageError('Couldn’t delete data from this phone’s secure storage.');
    }
  }
  if (opts.requireAuthentication) return;
  let left: string | null;
  try {
    left = await SecureStore.getItemAsync(key, opts);
  } catch {
    throw new StorageError('Couldn’t confirm data was deleted from this phone’s secure storage.');
  }
  if (left != null) throw new StorageError('Couldn’t delete data from this phone’s secure storage.');
}

/** Runs every delete, then throws once if any of them failed. */
async function deleteAll(deletes: Promise<void>[]): Promise<void> {
  const failed = (await Promise.allSettled(deletes)).filter((r) => r.status === 'rejected');
  if (failed.length) throw new StorageError(`Couldn’t delete ${failed.length === 1 ? 'one item' : `${failed.length} items`} from this phone’s secure storage.`);
}

// ---- vault ----

/** Presence check without parsing secrets into a long-lived object. */
export async function hasVault(): Promise<boolean> {
  return (await SecureStore.getItemAsync(KEYS.vault, BASE_OPTS)) != null;
}

export const readVault = () => readJson(KEYS.vault, vaultSchema);

export async function saveVault(v: VaultRecord): Promise<void> {
  vaultSchema.parse(v);
  await writeJson(KEYS.vault, v);
  // Read back: never tell the user a wallet exists unless the phrase really persisted.
  const back = await readVault();
  if (!back || back.mnemonic !== v.mnemonic || back.imported.length !== v.imported.length) {
    throw new StorageError('Couldn’t verify the wallet was saved. Nothing was created.');
  }
}

// ---- MAX session token ----

export const readSession = () => readJson(KEYS.session, sessionSchema);
export const saveSession = (s: SessionRecord) => writeJson(KEYS.session, sessionSchema.parse(s));
export const clearSession = () => safeDelete(KEYS.session, BASE_OPTS);

// ---- passcode verifier & lockout ----

export const readPasscodeRecord = () => readJson(KEYS.passcode, passcodeSchema);
export const savePasscodeRecord = (r: PasscodeRecord) => writeJson(KEYS.passcode, r);
export const readLockout = () => readJson(KEYS.lockout, lockoutSchema);
export const saveLockout = (s: LockoutState) => writeJson(KEYS.lockout, s);

// ---- biometric unlock ----

export async function readBiometricDigest(): Promise<string | null> {
  return SecureStore.getItemAsync(KEYS.bioFlag, BASE_OPTS);
}

/** Stores a random secret behind the OS biometric gate; reading it back proves presence. */
export async function saveBiometricSecret(secret: string, digest: string): Promise<void> {
  try {
    await SecureStore.setItemAsync(KEYS.bioSecret, secret, BIO_OPTS);
    await SecureStore.setItemAsync(KEYS.bioFlag, digest, BASE_OPTS);
  } catch {
    throw new StorageError('Couldn’t turn on Face ID on this phone.');
  }
}

/** Shows the OS biometric prompt. Null when cancelled, failed or invalidated. */
export async function readBiometricSecret(): Promise<string | null> {
  try {
    return await SecureStore.getItemAsync(KEYS.bioSecret, BIO_OPTS);
  } catch {
    return null;
  }
}

/** The flag goes first: without it biometric unlock is off, whatever happens to the secret. */
export async function clearBiometric(): Promise<void> {
  await safeDelete(KEYS.bioFlag, BASE_OPTS);
  await safeDelete(KEYS.bioSecret, BIO_OPTS);
}

// ---- payments signed on this phone but not yet recorded with MAX ----

const pendingSchema = z.array(
  z.object({
    kind: z.enum(['send', 'rename']),
    paymentId: z.string().min(1),
    chainKey: z.string().min(1),
    signed: z.object({
      hash: z.string().min(1),
      raw: z.string().min(1),
      namespace: z.enum(['evm', 'solana']),
      blockhash: z.string().optional(),
      lastValidBlockHeight: z.number().optional(),
    }),
    at: z.number().int().nonnegative(),
  }),
);
export type PendingPayment = z.infer<typeof pendingSchema>[number];

export const readPending = async () => (await readJson(KEYS.pending, pendingSchema)) ?? [];
export const savePending = (list: PendingPayment[]) => (list.length ? writeJson(KEYS.pending, pendingSchema.parse(list)) : safeDelete(KEYS.pending, BASE_OPTS));

// ---- removal ----

/** Deletes the recovery phrase and keys. Throws if they may still be on the phone. */
export const deleteVault = () => safeDelete(KEYS.vault, BASE_OPTS);

/** Removes everything bound to the wallet from this phone; throws if anything stayed. */
export async function clearAll(): Promise<void> {
  await deleteAll([
    safeDelete(KEYS.vault, BASE_OPTS),
    safeDelete(KEYS.session, BASE_OPTS),
    safeDelete(KEYS.passcode, BASE_OPTS),
    safeDelete(KEYS.lockout, BASE_OPTS),
    safeDelete(KEYS.pending, BASE_OPTS),
    clearBiometric(),
  ]);
}
