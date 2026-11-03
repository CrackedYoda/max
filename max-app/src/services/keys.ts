import type { WalletSigner } from '@max/sdk';
import { ed25519 } from '@noble/curves/ed25519.js';
import { hmac } from '@noble/hashes/hmac.js';
import { sha512 } from '@noble/hashes/sha2.js';
import { HDKey } from '@scure/bip32';
import { entropyToMnemonic, mnemonicToSeed, validateMnemonic } from '@scure/bip39';
import { wordlist } from '@scure/bip39/wordlists/english.js';
import { Keypair } from '@solana/web3.js';
import bs58 from 'bs58';
import * as Crypto from 'expo-crypto';
import { bytesToHex, type Hex } from 'viem';
import { privateKeyToAccount, type PrivateKeyAccount } from 'viem/accounts';

/**
 * Key material helpers. Pure functions over secrets the vault hands in; nothing
 * here persists anything.
 *
 * Paths match Phantom/MetaMask so a phrase restores the same addresses there:
 *   EVM     m/44'/60'/0'/0/0   (BIP-32 secp256k1)
 *   Solana  m/44'/501'/0'/0'   (SLIP-0010 ed25519)
 */

/** Namespaces this wallet holds keys for. */
export type KeyNamespace = 'evm' | 'solana';

export const EVM_PATH = "m/44'/60'/0'/0/0";
export const SOLANA_PATH = "m/44'/501'/0'/0'";

// ---- recovery phrase ----

/** 12 words from 128 bits of CSPRNG entropy. */
export function generatePhrase(): string {
  return entropyToMnemonic(Crypto.getRandomBytes(16), wordlist);
}

export function normalizePhrase(input: string): string {
  return input.trim().toLowerCase().split(/\s+/).filter(Boolean).join(' ');
}

export type PhraseCheck = { ok: true; phrase: string } | { ok: false; message: string };

/** Human-readable validation: says which word is wrong rather than "invalid". */
export function checkPhrase(input: string): PhraseCheck {
  const phrase = normalizePhrase(input);
  const words = phrase ? phrase.split(' ') : [];
  if (words.length !== 12 && words.length !== 24) {
    return { ok: false, message: `Recovery phrases have 12 or 24 words. This has ${words.length}.` };
  }
  const bad = words.findIndex((w) => !wordlist.includes(w));
  if (bad >= 0) return { ok: false, message: `Word ${bad + 1} isn’t a recovery word. Check the spelling.` };
  if (!validateMnemonic(phrase, wordlist)) {
    return { ok: false, message: 'These words don’t form a valid phrase. Check the order and spelling.' };
  }
  return { ok: true, phrase };
}

export const isRecoveryWord = (w: string) => wordlist.includes(w);
export const recoveryWords = wordlist;

// ---- derivation ----

function slip10Ed25519(seed: Uint8Array, path: string): Uint8Array {
  let I = hmac(sha512, new TextEncoder().encode('ed25519 seed'), seed);
  let key = I.slice(0, 32);
  let chain = I.slice(32);
  for (const seg of path.split('/').slice(1)) {
    if (!seg.endsWith("'")) throw new Error('ed25519 derivation is hardened-only');
    const index = (parseInt(seg.slice(0, -1), 10) + 0x80000000) >>> 0;
    const data = new Uint8Array(37);
    data.set(key, 1);
    new DataView(data.buffer).setUint32(33, index);
    I = hmac(sha512, chain, data);
    key = I.slice(0, 32);
    chain = I.slice(32);
  }
  return key;
}

export type DerivedKeys = { evm: Hex; solanaSeed: Uint8Array };

/** PBKDF2 is ~2048 rounds of SHA-512 in JS; callers should show progress. */
export async function deriveFromPhrase(phrase: string): Promise<DerivedKeys> {
  const seed = await mnemonicToSeed(phrase);
  const evmKey = HDKey.fromMasterSeed(seed).derive(EVM_PATH).privateKey;
  if (!evmKey) throw new Error('EVM derivation failed');
  return { evm: bytesToHex(evmKey), solanaSeed: slip10Ed25519(seed, SOLANA_PATH) };
}

// ---- imported private keys ----

export type KeyCheck = { ok: true; secret: string; address: string } | { ok: false; message: string };

export function checkPrivateKey(namespace: KeyNamespace, input: string): KeyCheck {
  const v = input.trim();
  if (namespace === 'evm') {
    const hex = v.startsWith('0x') ? v : `0x${v}`;
    if (!/^0x[0-9a-fA-F]{64}$/.test(hex)) return { ok: false, message: 'EVM keys are 0x followed by 64 hex characters.' };
    try {
      return { ok: true, secret: hex.toLowerCase(), address: privateKeyToAccount(hex as Hex).address };
    } catch {
      return { ok: false, message: 'That isn’t a valid EVM private key.' };
    }
  }
  try {
    const bytes = v.startsWith('[') ? Uint8Array.from(JSON.parse(v) as number[]) : bs58.decode(v);
    if (bytes.length !== 64 && bytes.length !== 32) throw new Error('length');
    const kp = bytes.length === 64 ? Keypair.fromSecretKey(bytes) : Keypair.fromSeed(bytes);
    return { ok: true, secret: bs58.encode(kp.secretKey), address: kp.publicKey.toBase58() };
  } catch {
    return { ok: false, message: 'That isn’t a valid Solana private key (base58 or a 64-number array).' };
  }
}

// ---- signers ----

export type EvmKey = { namespace: 'evm'; address: string; account: PrivateKeyAccount; privateKey: Hex };
export type SolanaKey = { namespace: 'solana'; address: string; keypair: Keypair };
export type ChainKey = EvmKey | SolanaKey;

export function evmKey(privateKey: Hex): EvmKey {
  const account = privateKeyToAccount(privateKey);
  return { namespace: 'evm', address: account.address, account, privateKey };
}

export function solanaKeyFromSeed(seed: Uint8Array): SolanaKey {
  const keypair = Keypair.fromSeed(seed);
  return { namespace: 'solana', address: keypair.publicKey.toBase58(), keypair };
}

export function solanaKeyFromSecret(secretB58: string): SolanaKey {
  const keypair = Keypair.fromSecretKey(bs58.decode(secretB58));
  return { namespace: 'solana', address: keypair.publicKey.toBase58(), keypair };
}

/** The SDK's sign-in interface: signs the exact challenge text, never a transaction. */
export function walletSigner(key: ChainKey): WalletSigner {
  if (key.namespace === 'evm') {
    return {
      namespace: 'evm',
      address: key.address,
      signMessage: (message) => key.account.signMessage({ message }),
    };
  }
  return {
    namespace: 'solana',
    address: key.address,
    signMessage: async (message) => {
      const sig = ed25519.sign(new TextEncoder().encode(message), key.keypair.secretKey.slice(0, 32));
      return bs58.encode(sig);
    },
  };
}

/** For "Export private key": the format other wallets import. */
export function exportSecret(key: ChainKey): string {
  return key.namespace === 'evm' ? key.privateKey : bs58.encode(key.keypair.secretKey);
}
