import type { Ed25519Verify } from "./authorization.js";
import type { MaxClient, WaitOptions } from "./client.js";
import { AuthorizationError, SigningOutcomeUnknownError } from "./errors.js";
import type { AgentTransfer, AuthorizationClaims, Payment, ProposeTransferInput } from "./types.js";

/**
 * Signs and broadcasts exactly the transaction described by the verified
 * claims, returning its hash. Build the transaction from the claims (e.g.
 * `evmTransactionFromClaims`) and check it with `assertEvmTransactionMatches`
 * / `assertSolanaTransferMatches` before signing.
 *
 * Because signing and broadcasting happen together, PolicySigner cannot tell
 * whether a failed call sent anything, so it never calls it twice for one
 * payment. Prefer `SignThenBroadcast`, which lets a failed broadcast be
 * retried with the same signed bytes. Throw `AuthorizationError` only before
 * anything is signed: it releases the payment for another attempt.
 */
export type SignFromClaims = (claims: AuthorizationClaims) => Promise<string>;

/** A signed, not yet broadcast transaction: its serialized bytes (any encoding your broadcast accepts) and its hash. */
export interface SignedTransaction {
  raw: string;
  txHash: string;
}

/**
 * Signing and broadcasting as separate steps. PolicySigner saves the signed
 * bytes before broadcasting them, and every retry broadcasts those same
 * bytes, so one payment can never become two transactions. `sign` must not
 * broadcast. `broadcast` may be called again with bytes that already landed
 * and should treat "already known" as success.
 *
 * Pin the transaction to one slot where the chain allows it (an explicit EVM
 * nonce, a Solana blockhash) and to the network in the claims (EVM chainId).
 */
export interface SignThenBroadcast {
  sign(claims: AuthorizationClaims): Promise<SignedTransaction>;
  broadcast(tx: SignedTransaction): Promise<void>;
}

/**
 * One payment's signing attempt. `reserved`: signing may have started, outcome
 * unknown. `signed`: bytes saved, maybe not broadcast. `broadcast`: the hash
 * is on its way to MAX.
 */
export type SigningAttempt =
  | { paymentId: string; state: "reserved" }
  | { paymentId: string; state: "signed"; raw: string; txHash: string }
  | { paymentId: string; state: "broadcast"; txHash: string };

/**
 * Where PolicySigner records signing attempts. `reserve` must be atomic for
 * every signer that shares the store: two processes that can sign for the
 * same wallet need one durable store (e.g. a database row with a unique key on
 * paymentId), or each can sign the same payment once.
 */
export interface AttemptStore {
  /** Create a `reserved` attempt; false if this payment already has one. */
  reserve(paymentId: string): Promise<boolean>;
  get(paymentId: string): Promise<SigningAttempt | undefined>;
  put(attempt: SigningAttempt): Promise<void>;
  /** Forget an attempt. PolicySigner calls it only when nothing was signed. */
  release(paymentId: string): Promise<void>;
}

/** Process memory only: protects one PolicySigner process, not a restart or a second process. */
export class MemoryAttemptStore implements AttemptStore {
  private readonly attempts = new Map<string, SigningAttempt>();

  async reserve(paymentId: string): Promise<boolean> {
    if (this.attempts.has(paymentId)) return false;
    this.attempts.set(paymentId, { paymentId, state: "reserved" });
    return true;
  }
  async get(paymentId: string): Promise<SigningAttempt | undefined> {
    return this.attempts.get(paymentId);
  }
  async put(attempt: SigningAttempt): Promise<void> {
    this.attempts.set(attempt.paymentId, attempt);
  }
  async release(paymentId: string): Promise<void> {
    this.attempts.delete(paymentId);
  }
}

export interface PolicySignerOptions {
  /** A client authenticated with an agent key holding authorizations:read and payments:submit. */
  client: MaxClient;
  /** The agent wallet this signer controls. Authorizations for any other sender are refused. */
  sender: string;
  verify?: Ed25519Verify;
  /** Defaults to a MemoryAttemptStore. Pass a durable, shared store to survive restarts and multiple processes. */
  attempts?: AttemptStore;
}

/**
 * The trusted signing step of an agent. It is deliberately separate from the
 * LLM: it acts only on authorizations MAX signed after deterministic policy
 * checks, so a prompt-injected agent cannot get a transfer signed.
 */
export class PolicySigner {
  private readonly attempts: AttemptStore;
  private readonly running = new Map<string, Promise<unknown>>();

  constructor(private readonly opts: PolicySignerOptions) {
    this.attempts = opts.attempts ?? new MemoryAttemptStore();
  }

  /**
   * Verify MAX's authorization for paymentId, sign it once, broadcast and
   * submit the hash. Calls for the same payment run one at a time; a repeat
   * call, concurrent or a retry, reuses the first attempt and never signs a
   * second transaction. Throws SigningOutcomeUnknownError when an earlier
   * attempt's outcome can't be known.
   */
  execute(paymentId: string, signer: SignFromClaims | SignThenBroadcast): Promise<Payment> {
    const prev = this.running.get(paymentId) ?? Promise.resolve();
    const run = prev.then(() => this.run(paymentId, signer));
    const tail = run.catch(() => undefined);
    this.running.set(paymentId, tail);
    void tail.then(() => {
      if (this.running.get(paymentId) === tail) this.running.delete(paymentId);
    });
    return run;
  }

  private async run(paymentId: string, signer: SignFromClaims | SignThenBroadcast): Promise<Payment> {
    let attempt = await this.attempts.get(paymentId);
    if (!attempt) {
      if (!(await this.attempts.reserve(paymentId))) throw new SigningOutcomeUnknownError(paymentId);
      attempt = await this.sign(paymentId, signer);
    }
    if (attempt.state === "reserved") throw new SigningOutcomeUnknownError(paymentId);
    if (attempt.state === "signed") {
      if (typeof signer === "function") throw new Error(`payment ${paymentId} has signed bytes waiting; pass a SignThenBroadcast to broadcast them`);
      await signer.broadcast({ raw: attempt.raw, txHash: attempt.txHash });
      attempt = { paymentId, state: "broadcast", txHash: attempt.txHash };
      await this.attempts.put(attempt);
    }
    return this.opts.client.agent.submit(paymentId, attempt.txHash);
  }

  /** Runs with the attempt reserved; returns it signed (or broadcast), or releases it if nothing was signed. */
  private async sign(paymentId: string, signer: SignFromClaims | SignThenBroadcast): Promise<SigningAttempt> {
    const unsigned = async (err: unknown): Promise<never> => {
      await this.attempts.release(paymentId);
      throw err;
    };
    let claims: AuthorizationClaims;
    try {
      claims = await this.opts.client.agent.verifiedClaims(paymentId, { sender: this.opts.sender, verify: this.opts.verify });
    } catch (err) {
      return unsigned(err);
    }

    if (typeof signer === "function") {
      let txHash: string;
      try {
        txHash = await signer(claims);
      } catch (err) {
        if (err instanceof AuthorizationError) return unsigned(err);
        throw err; // may have broadcast: the attempt stays reserved
      }
      if (!txHash) throw new AuthorizationError("signer returned no transaction hash");
      const attempt: SigningAttempt = { paymentId, state: "broadcast", txHash };
      await this.attempts.put(attempt);
      return attempt;
    }

    let tx: SignedTransaction;
    try {
      tx = await signer.sign(claims);
    } catch (err) {
      return unsigned(err);
    }
    if (!tx?.raw || !tx.txHash) return unsigned(new AuthorizationError("signer returned no signed transaction"));
    const attempt: SigningAttempt = { paymentId, state: "signed", raw: tx.raw, txHash: tx.txHash };
    await this.attempts.put(attempt);
    return attempt;
  }

  /**
   * Propose a transfer and, once MAX approves it (immediately, or after the
   * administrator approves when `waitForApproval` is set), execute it.
   */
  async proposeAndExecute(
    input: Omit<ProposeTransferInput, "senderAddress">,
    sign: SignFromClaims | SignThenBroadcast,
    opts: { waitForApproval?: boolean | WaitOptions<AgentTransfer> } = {},
  ): Promise<{ transfer: AgentTransfer; payment?: Payment }> {
    let transfer = await this.opts.client.agent.propose({ ...input, senderAddress: this.opts.sender });
    if (transfer.authorization.decision === "pending_approval" && opts.waitForApproval) {
      const waitOpts = typeof opts.waitForApproval === "object" ? opts.waitForApproval : {};
      transfer = await this.opts.client.agent.waitForDecision(transfer.payment.id, waitOpts);
    }
    const d = transfer.authorization.decision;
    if (d !== "auto_approved" && d !== "approved") return { transfer };
    return { transfer, payment: await this.execute(transfer.payment.id, sign) };
  }
}
