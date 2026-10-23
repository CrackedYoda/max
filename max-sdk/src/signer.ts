import type { Ed25519Verify } from "./authorization.js";
import type { MaxClient, WaitOptions } from "./client.js";
import { AuthorizationError } from "./errors.js";
import type { AgentTransfer, AuthorizationClaims, Payment, ProposeTransferInput } from "./types.js";

/**
 * Signs and broadcasts exactly the transaction described by the verified
 * claims, returning its hash. Build the transaction from the claims (e.g.
 * `evmTransactionFromClaims`) and check it with `assertEvmTransactionMatches`
 * / `assertSolanaTransferMatches` before signing.
 */
export type SignFromClaims = (claims: AuthorizationClaims) => Promise<string>;

export interface PolicySignerOptions {
  /** A client authenticated with an agent key holding authorizations:read and payments:submit. */
  client: MaxClient;
  /** The agent wallet this signer controls. Authorizations for any other sender are refused. */
  sender: string;
  verify?: Ed25519Verify;
}

/**
 * The trusted signing step of an agent. It is deliberately separate from the
 * LLM: it acts only on authorizations MAX signed after deterministic policy
 * checks, so a prompt-injected agent cannot get a transfer signed.
 */
export class PolicySigner {
  constructor(private readonly opts: PolicySignerOptions) {}

  /** Verify MAX's authorization for paymentId, sign via `sign`, and submit the hash. */
  async execute(paymentId: string, sign: SignFromClaims): Promise<Payment> {
    const claims = await this.opts.client.agent.verifiedClaims(paymentId, { sender: this.opts.sender, verify: this.opts.verify });
    const txHash = await sign(claims);
    if (!txHash) throw new AuthorizationError("signer returned no transaction hash");
    return this.opts.client.agent.submit(paymentId, txHash);
  }

  /**
   * Propose a transfer and, once MAX approves it (immediately, or after the
   * administrator approves when `waitForApproval` is set), execute it.
   */
  async proposeAndExecute(
    input: Omit<ProposeTransferInput, "senderAddress">,
    sign: SignFromClaims,
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
