import * as Crypto from 'expo-crypto';
import * as LocalAuthentication from 'expo-local-authentication';

import * as secure from '@/services/secure';

/**
 * Local passcode + Face ID gate (Noma passcode.ts). The passcode never leaves
 * the phone; only a salted SHA-256 verifier is stored. A 6-digit space can't be
 * made brute-force resistant by a client-side KDF, so protection is the keychain
 * boundary plus escalating lockout.
 *
 * Unlike Noma, too many failures never wipe the wallet: with self-custody a wipe
 * could destroy funds for someone without their recovery phrase. Lockout keeps
 * escalating instead.
 */

export const PASSCODE_LENGTH = 6;
export const LOCKOUT_START = 5;

/** Seconds of lockout after `failures` consecutive wrong attempts. */
export function lockoutSeconds(failures: number): number {
  if (failures < LOCKOUT_START) return 0;
  const table = [30, 60, 120, 300, 900];
  return table[Math.min(failures - LOCKOUT_START, table.length - 1)]!;
}

function hex(bytes: Uint8Array): string {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
}

const sha256 = (input: string) => Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, input);

function constantTimeEqual(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return diff === 0;
}

const verifierFor = (salt: string, passcode: string) => sha256(`max.passcode.v1:${salt}:${passcode}`);

export async function setPasscode(passcode: string): Promise<void> {
  if (!/^\d{6}$/.test(passcode)) throw new Error('Passcode must be 6 digits.');
  const salt = hex(Crypto.getRandomBytes(32));
  await secure.savePasscodeRecord({ v: 1, salt, verifier: await verifierFor(salt, passcode) });
  await secure.saveLockout({ failures: 0, lockedUntil: 0 });
}

export type VerifyResult =
  | { ok: true }
  | { ok: false; reason: 'wrong'; attemptsBeforeLock: number }
  | { ok: false; reason: 'locked'; retryAt: number }
  | { ok: false; reason: 'no_passcode' };

export async function getLockout(): Promise<secure.LockoutState> {
  return (await secure.readLockout()) ?? { failures: 0, lockedUntil: 0 };
}

export async function verifyPasscode(passcode: string, now = Date.now()): Promise<VerifyResult> {
  const lock = await getLockout();
  if (lock.lockedUntil > now) return { ok: false, reason: 'locked', retryAt: lock.lockedUntil };

  const record = await secure.readPasscodeRecord();
  if (!record) return { ok: false, reason: 'no_passcode' };

  // Count the attempt before checking, so killing the app mid-check can't reset it.
  const failures = lock.failures + 1;
  await secure.saveLockout({ failures, lockedUntil: lock.lockedUntil });

  if (constantTimeEqual(await verifierFor(record.salt, passcode), record.verifier)) {
    await secure.saveLockout({ failures: 0, lockedUntil: 0 });
    return { ok: true };
  }

  const secs = lockoutSeconds(failures);
  if (secs > 0) {
    const retryAt = now + secs * 1000;
    await secure.saveLockout({ failures, lockedUntil: retryAt });
    return { ok: false, reason: 'locked', retryAt };
  }
  return { ok: false, reason: 'wrong', attemptsBeforeLock: LOCKOUT_START - failures };
}

// ---- biometrics ----

export async function biometricAvailable(): Promise<boolean> {
  try {
    return (await LocalAuthentication.hasHardwareAsync()) && (await LocalAuthentication.isEnrolledAsync());
  } catch {
    return false;
  }
}

export async function biometricLabel(): Promise<string> {
  try {
    const types = await LocalAuthentication.supportedAuthenticationTypesAsync();
    if (types.includes(LocalAuthentication.AuthenticationType.FACIAL_RECOGNITION)) return 'Face ID';
    if (types.includes(LocalAuthentication.AuthenticationType.FINGERPRINT)) return 'Touch ID';
  } catch {
    // fall through
  }
  return 'Biometrics';
}

/** Turns on biometric unlock: a random secret stored behind the OS biometric gate. */
export async function enableBiometric(): Promise<boolean> {
  if (!(await biometricAvailable())) return false;
  const confirm = await LocalAuthentication.authenticateAsync({
    promptMessage: 'Turn on Face ID for MAX',
    disableDeviceFallback: true,
    cancelLabel: 'Cancel',
  });
  if (!confirm.success) return false;
  const secret = hex(Crypto.getRandomBytes(32));
  await secure.saveBiometricSecret(secret, await sha256(secret));
  return true;
}

export const disableBiometric = () => secure.clearBiometric();

export async function isBiometricEnabled(): Promise<boolean> {
  return (await secure.readBiometricDigest()) !== null;
}

/**
 * Proves presence by reading the biometric-gated secret. An enrollment change
 * invalidates the key; biometrics are then turned off and the passcode is needed.
 */
export async function verifyBiometric(): Promise<'ok' | 'failed' | 'invalidated' | 'disabled'> {
  const digest = await secure.readBiometricDigest();
  if (!digest) return 'disabled';
  const secret = await secure.readBiometricSecret();
  if (secret === null) {
    if (!(await biometricAvailable())) {
      await secure.clearBiometric();
      return 'invalidated';
    }
    return 'failed';
  }
  if (!constantTimeEqual(await sha256(secret), digest)) {
    await secure.clearBiometric();
    return 'invalidated';
  }
  await secure.saveLockout({ failures: 0, lockedUntil: 0 });
  return 'ok';
}
