import * as Crypto from 'expo-crypto';

import {
  deriveFromPhrase,
  evmKey,
  solanaKeyFromSecret,
  solanaKeyFromSeed,
  type ChainKey,
  type EvmKey,
  type KeyNamespace,
  type SolanaKey,
} from '@/services/keys';
import * as secure from '@/services/secure';

/**
 * Accounts derived from the vault, held in memory only while unlocked. Locking
 * drops every reference; the next unlock re-reads the keychain.
 */

export type Account = {
  id: string;
  label: string;
  source: 'phrase' | 'key';
  evm?: EvmKey;
  solana?: SolanaKey;
};

/** Public view: what screens may render. Never contains key material. */
export type AccountInfo = {
  id: string;
  label: string;
  source: 'phrase' | 'key';
  evm?: string;
  solana?: string;
};

let accounts: Account[] | null = null;

export const isLoaded = () => accounts !== null;

export function accountInfos(): AccountInfo[] {
  return (accounts ?? []).map((a) => ({
    id: a.id,
    label: a.label,
    source: a.source,
    evm: a.evm?.address,
    solana: a.solana?.address,
  }));
}

async function build(record: secure.VaultRecord): Promise<Account[]> {
  const list: Account[] = [];
  if (record.mnemonic) {
    const d = await deriveFromPhrase(record.mnemonic);
    list.push({ id: 'hd-0', label: 'Account 1', source: 'phrase', evm: evmKey(d.evm), solana: solanaKeyFromSeed(d.solanaSeed) });
  }
  for (const k of record.imported) {
    list.push(
      k.namespace === 'evm'
        ? { id: k.id, label: 'Imported', source: 'key', evm: evmKey(k.secret as `0x${string}`) }
        : { id: k.id, label: 'Imported', source: 'key', solana: solanaKeyFromSecret(k.secret) },
    );
  }
  return list;
}

/**
 * Call only after the passcode/Face ID check passed. `isCurrent` is asked
 * after every await: if the unlock was cancelled meanwhile (lock, background),
 * the derived keys are never installed and null is returned.
 */
export async function load(isCurrent: () => boolean = () => true): Promise<AccountInfo[] | null> {
  const record = await secure.readVault();
  if (!isCurrent()) return null;
  if (!record) throw new secure.StorageError('This phone’s wallet couldn’t be read. Restore it from your recovery phrase.');
  const built = await build(record);
  if (!isCurrent()) return null;
  accounts = built;
  return accountInfos();
}

export function unload(): void {
  accounts = null;
}

/** First wallet on this phone, from a phrase or a single key. */
export async function create(input: { mnemonic: string } | { namespace: KeyNamespace; secret: string }): Promise<AccountInfo[]> {
  const record: secure.VaultRecord =
    'mnemonic' in input
      ? { v: 1, mnemonic: input.mnemonic, imported: [] }
      : { v: 1, mnemonic: null, imported: [{ id: newId(), namespace: input.namespace, secret: input.secret }] };
  const built = await build(record); // derive before saving so a bad input never persists
  await secure.saveVault(record);
  accounts = built;
  return accountInfos();
}

/** Adds an account to an existing, unlocked vault. */
export async function addImported(namespace: KeyNamespace, secret: string): Promise<AccountInfo[]> {
  const record = await secure.readVault();
  if (!record) throw new secure.StorageError('No wallet on this phone.');
  if (record.imported.some((k) => k.secret === secret)) throw new Error('That key is already on this phone.');
  const next: secure.VaultRecord = { ...record, imported: [...record.imported, { id: newId(), namespace, secret }] };
  accounts = await build(next);
  await secure.saveVault(next);
  return accountInfos();
}

/** Removes an imported account. Phrase accounts can only go with the whole wallet. */
export async function removeImported(id: string): Promise<AccountInfo[]> {
  const record = await secure.readVault();
  if (!record) throw new secure.StorageError('No wallet on this phone.');
  const next = { ...record, imported: record.imported.filter((k) => k.id !== id) };
  if (!next.mnemonic && next.imported.length === 0) throw new Error('This is the only account. Remove the wallet instead.');
  await secure.saveVault(next);
  accounts = await build(next);
  return accountInfos();
}

/** Recovery phrase for "Reveal". Callers must have stepped up first. */
export async function revealPhrase(): Promise<string | null> {
  return (await secure.readVault())?.mnemonic ?? null;
}

// ---- key access for signing ----

function requireLoaded(): Account[] {
  if (!accounts) throw new Error('Wallet is locked.');
  return accounts;
}

/** The key that signs for `address` on `namespace`, if it lives on this phone. */
export function keyFor(namespace: KeyNamespace, address: string): ChainKey | null {
  for (const a of requireLoaded()) {
    const k = namespace === 'evm' ? a.evm : a.solana;
    if (k && (namespace === 'evm' ? k.address.toLowerCase() === address.toLowerCase() : k.address === address)) return k;
  }
  return null;
}

/** Default sending/sign-in key per namespace: the first account that has one. */
export function primaryKey(namespace: KeyNamespace): ChainKey | null {
  for (const a of requireLoaded()) {
    const k = namespace === 'evm' ? a.evm : a.solana;
    if (k) return k;
  }
  return null;
}

export function allKeys(): ChainKey[] {
  return requireLoaded().flatMap((a) => [a.evm, a.solana].filter((k): k is EvmKey | SolanaKey => !!k));
}

function newId(): string {
  return `key-${Crypto.randomUUID()}`;
}
