import { assertSaleInstructionsMatch, type Chain, type Instructions, type Payment, type SaleExpectations, type SaleView } from '@max/sdk';
import {
  createAssociatedTokenAccountIdempotentInstruction,
  createTransferCheckedInstruction,
  getAssociatedTokenAddressSync,
  TOKEN_PROGRAM_ID,
} from '@solana/spl-token';
import { Connection, LAMPORTS_PER_SOL, PublicKey, SystemProgram, Transaction } from '@solana/web3.js';
import bs58 from 'bs58';
import { Buffer } from 'buffer';
import {
  createPublicClient,
  defineChain,
  erc20Abi,
  formatUnits,
  http,
  keccak256,
  type PublicClient,
} from 'viem';

import { getEnv, USDC } from '@/config/env';
import type { EvmKey, SolanaKey } from '@/services/keys';
import { assertSaleChain, matchSaleSignature, saleInstructions } from '@/services/saleTx';
import * as vault from '@/services/vault';
import type { SignedTx } from '@/store/flows';

/**
 * Direct chain access: balances, fee estimates, and signing + broadcasting the
 * exact transfer MAX's instructions describe. Keys never leave this module's callers.
 */

export class ChainError extends Error {
  /** True when the transaction may already be on its way: never sign a second one. */
  constructor(message: string, readonly ambiguous = false) {
    super(message);
  }
}

export const hasRpc = (chainKey: string) => !!getEnv().rpc[chainKey];

const evmClients = new Map<string, PublicClient>();
function evmClient(chain: Chain): PublicClient {
  const rpc = getEnv().rpc[chain.key];
  if (!rpc || !chain.evmChainId) throw new ChainError(`${chain.name} isn’t set up in this app.`);
  let c = evmClients.get(chain.key);
  if (!c) {
    c = createPublicClient({
      chain: defineChain({
        id: chain.evmChainId,
        name: chain.name,
        nativeCurrency: { name: chain.nativeCoin, symbol: chain.nativeCoin, decimals: chain.nativeDecimals },
        rpcUrls: { default: { http: [rpc] } },
      }),
      transport: http(rpc, { timeout: 15_000, retryCount: 1 }),
    });
    evmClients.set(chain.key, c);
  }
  return c;
}

const solConns = new Map<string, Connection>();
function solConn(chain: Pick<Chain, 'key' | 'name'>): Connection {
  const rpc = getEnv().rpc[chain.key];
  if (!rpc) throw new ChainError(`${chain.name} isn’t set up in this app.`);
  let c = solConns.get(chain.key);
  if (!c) {
    c = new Connection(rpc, 'confirmed');
    solConns.set(chain.key, c);
  }
  return c;
}

/** A Solana connection for a chain known only by key (market sales carry the key). */
const solConnForKey = (key: string) => {
  if (!key.startsWith('solana')) throw new ChainError('Market sales settle on Solana only.');
  return solConn({ key, name: key });
};

// ---- balances ----

export type Balance = {
  chain: Chain;
  asset: string; // "native" | contract/mint
  symbol: string;
  name: string;
  address: string; // holder
  amount: string; // display units
  raw: bigint;
  decimals: number;
};

export async function nativeBalance(chain: Chain, holder: string): Promise<Balance> {
  let raw: bigint;
  if (chain.namespace === 'evm') {
    raw = await evmClient(chain).getBalance({ address: holder as `0x${string}` });
  } else {
    raw = BigInt(await solConn(chain).getBalance(new PublicKey(holder)));
  }
  return {
    chain,
    asset: 'native',
    symbol: chain.nativeCoin,
    name: chain.namespace === 'solana' ? 'Solana' : chain.nativeCoin === 'ETH' ? 'Ether' : chain.nativeCoin,
    address: holder,
    raw,
    decimals: chain.nativeDecimals,
    amount: formatUnits(raw, chain.nativeDecimals),
  };
}

export async function usdcBalance(chain: Chain, holder: string): Promise<Balance | null> {
  const token = USDC[chain.key];
  if (!token) return null;
  let raw = 0n;
  if (chain.namespace === 'evm') {
    raw = await evmClient(chain).readContract({ address: token as `0x${string}`, abi: erc20Abi, functionName: 'balanceOf', args: [holder as `0x${string}`] });
  } else {
    const ata = getAssociatedTokenAddressSync(new PublicKey(token), new PublicKey(holder), false, TOKEN_PROGRAM_ID);
    try {
      const bal = await solConn(chain).getTokenAccountBalance(ata);
      raw = BigInt(bal.value.amount);
    } catch {
      raw = 0n; // no token account yet
    }
  }
  return { chain, asset: token, symbol: 'USDC', name: 'USD Coin', address: holder, raw, decimals: 6, amount: formatUnits(raw, 6) };
}

// ---- fees ----

/** Rough fee for the review screen, in native display units. Null when it can't be estimated. */
export async function estimateFee(payment: Payment): Promise<string | null> {
  const ins = payment.instructions;
  try {
    if (ins?.evm) {
      const c = evmClient(payment.chain);
      const [gas, fees] = await Promise.all([
        c.estimateGas({ account: payment.senderAddress as `0x${string}`, to: ins.evm.to as `0x${string}`, value: BigInt(ins.evm.value), data: ins.evm.data as `0x${string}` }),
        c.estimateFeesPerGas(),
      ]);
      const perGas = fees.maxFeePerGas ?? fees.gasPrice ?? 0n;
      return formatUnits(gas * perGas, payment.chain.nativeDecimals);
    }
    if (ins?.solana) return (5000 / LAMPORTS_PER_SOL).toString();
  } catch {
    // fall through: show "—" rather than a made-up number
  }
  return null;
}

// ---- sign ----

/**
 * Signs the payment's instructions with the on-device key for its sender. The
 * hash is known before broadcasting, so a dropped connection never leads to a
 * second signature.
 */
export async function signPayment(payment: Payment): Promise<SignedTx> {
  const ins = payment.instructions;
  if (!ins) throw new ChainError('This payment has no signing instructions.');
  const ns = payment.chain.namespace;
  if (ns !== 'evm' && ns !== 'solana') throw new ChainError(`${payment.chain.name} isn’t supported in this app.`);
  const key = vault.keyFor(ns, payment.senderAddress);
  if (!key) throw new ChainError('The sending account isn’t on this phone.');
  if (ins.evm && key.namespace === 'evm') return signEvm(payment.chain, ins.evm, key);
  if (ins.solana && key.namespace === 'solana') return signSolana(payment.chain, ins.solana, key);
  throw new ChainError('Unsupported payment instructions.');
}

async function signEvm(chain: Chain, e: NonNullable<Instructions['evm']>, key: EvmKey): Promise<SignedTx> {
  if (e.chainId !== chain.evmChainId) throw new ChainError('Chain mismatch in payment instructions.');
  const c = evmClient(chain);
  const request = await c.prepareTransactionRequest({
    account: key.account,
    to: e.to as `0x${string}`,
    value: BigInt(e.value),
    data: e.data as `0x${string}`,
    chain: c.chain,
  });
  const raw = await key.account.signTransaction(request as Parameters<typeof key.account.signTransaction>[0]);
  return { namespace: 'evm', raw, hash: keccak256(raw) };
}

async function signSolana(chain: Chain, s: NonNullable<Instructions['solana']>, key: SolanaKey): Promise<SignedTx> {
  const from = new PublicKey(s.from);
  if (!from.equals(key.keypair.publicKey)) throw new ChainError('Sender mismatch in payment instructions.');
  const tx = new Transaction();
  if (s.kind === 'system_transfer') {
    tx.add(SystemProgram.transfer({ fromPubkey: from, toPubkey: new PublicKey(s.to), lamports: BigInt(s.lamports!) }));
  } else {
    const mint = new PublicKey(s.mint!);
    const program = new PublicKey(s.tokenProgram!);
    const dest = new PublicKey(s.destinationTokenAccount!);
    // Idempotent create: harmless if the recipient's token account already exists.
    tx.add(createAssociatedTokenAccountIdempotentInstruction(from, dest, new PublicKey(s.to), mint, program));
    tx.add(createTransferCheckedInstruction(new PublicKey(s.sourceTokenAccount!), mint, dest, from, BigInt(s.amount!), s.decimals!, [], program));
  }
  tx.recentBlockhash = (await solConn(chain).getLatestBlockhash('confirmed')).blockhash;
  tx.feePayer = from;
  tx.sign(key.keypair);
  const sig = tx.signature;
  if (!sig) throw new ChainError('Signing failed.');
  return { namespace: 'solana', raw: tx.serialize().toString('base64'), hash: bs58.encode(sig) };
}

// ---- broadcast ----

/** Sends an already-signed transaction. Safe to repeat: same bytes, same hash. */
export async function broadcast(chain: Chain, signed: SignedTx): Promise<void> {
  try {
    if (signed.namespace === 'evm') {
      await evmClient(chain).sendRawTransaction({ serializedTransaction: signed.raw as `0x${string}` });
    } else {
      await solConn(chain).sendRawTransaction(Buffer.from(signed.raw, 'base64'), { skipPreflight: false, maxRetries: 3 });
    }
  } catch (err) {
    classifyBroadcastError(err, `Not enough ${chain.nativeCoin} to cover the amount and network fee.`);
  }
}

/**
 * Returns when the error means the transaction already landed. Otherwise
 * throws a ChainError: definite ("nothing was sent") or ambiguous (it may
 * have gone out: never sign a second one).
 */
function classifyBroadcastError(err: unknown, insufficient: string): void {
  const msg = err instanceof Error ? err.message : String(err);
  // Already known/processed means the first attempt landed.
  if (/already known|already been processed|nonce too low|AlreadyProcessed/i.test(msg)) return;
  if (/insufficient funds|insufficient lamports|0x1\b|no record of a prior credit/i.test(msg)) throw new ChainError(insufficient);
  // Preflight rejected it: nothing was sent.
  if (/simulation failed/i.test(msg)) throw new ChainError('The network rejected this transaction. Nothing was sent.');
  if (/blockhash not found/i.test(msg)) throw new ChainError('The network moved on before this was sent. Review it again.');
  // Timeouts and dropped connections: it may have gone out.
  throw new ChainError('We couldn’t confirm the network received it.', true);
}

// ---- marketplace sales (Solana, multi-instruction) ----

/** A mint's decimals read through this app's own RPC, never from MAX. */
export async function mintDecimals(chainKey: string, mint: string): Promise<number> {
  const supply = await solConnForKey(chainKey).getTokenSupply(new PublicKey(mint));
  return supply.value.decimals;
}

/**
 * Verifies a sale's instructions against what the user agreed to, then signs
 * the multi-instruction transaction (create token accounts as needed, fee to
 * MAX's treasury, the rest to the seller, the sale memo) with the on-device
 * key for `expect.buyer`. `expect` must come from what the user tapped (the
 * listing or offer) and trusted decimals, never from the sale response:
 * `assertSaleInstructionsMatch` throws AuthorizationError on any mismatch, and
 * nothing is signed. Not broadcast here: the caller keeps the bytes so a retry
 * re-sends the same transaction.
 */
export async function signSale(view: SaleView, chainKey: string, expect: Required<Pick<SaleExpectations, 'buyer' | 'asset' | 'priceBaseUnits'>>): Promise<SignedTx> {
  // `chainKey` is the tapped listing/offer's chain: the RPC is chosen by it, never by the sale.
  assertSaleChain(view.sale.chain, chainKey);
  assertSaleInstructionsMatch(view.sale, view.instructions, expect);
  const key = vault.keyFor('solana', expect.buyer);
  if (!key || key.namespace !== 'solana') throw new ChainError('The paying account isn’t on this phone.');
  const payer = key.keypair.publicKey;
  const tx = new Transaction();
  tx.add(...saleInstructions(view.instructions?.solana, payer));
  tx.recentBlockhash = (await solConnForKey(chainKey).getLatestBlockhash('confirmed')).blockhash;
  tx.feePayer = payer;
  tx.sign(key.keypair);
  const sig = tx.signature;
  if (!sig) throw new ChainError('Signing failed.');
  return { namespace: 'solana', raw: tx.serialize().toString('base64'), hash: bs58.encode(sig) };
}

/** Sends a signed sale payment; same definite/ambiguous classification as `broadcast`. */
export async function broadcastSale(chainKey: string, signed: SignedTx): Promise<void> {
  try {
    await solConnForKey(chainKey).sendRawTransaction(Buffer.from(signed.raw, 'base64'), { skipPreflight: false, maxRetries: 3 });
  } catch (err) {
    classifyBroadcastError(err, 'The paying wallet doesn’t hold enough to cover the price and the network fee. Nothing was sent.');
  }
}

/**
 * After an uncertain send: is this sale paid on chain? Checks the signature
 * this phone signed (if any), then the buyer's last 20 transactions for the
 * `max:sale:<id>` memo. Returns the landed signature or null. Throws when the
 * network can't be asked.
 */
export async function findSalePayment(chainKey: string, buyer: string, saleId: string, knownHash?: string): Promise<string | null> {
  const conn = solConnForKey(chainKey);
  if (knownHash) {
    const st = (await conn.getSignatureStatuses([knownHash], { searchTransactionHistory: true })).value[0];
    if (st && !st.err) return knownHash;
  }
  const sigs = await conn.getSignaturesForAddress(new PublicKey(buyer), { limit: 20 }, 'confirmed');
  return matchSaleSignature(sigs, saleId);
}
