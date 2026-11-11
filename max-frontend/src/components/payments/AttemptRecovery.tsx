"use client";

import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";

import { max } from "@/api/client";
import { Button } from "@/components/ui/Button";
import { Banner, Card } from "@/components/ui/Blocks";
import { errorMessage } from "@/error/messages";
import { clearAttempt, listAttempts, loadAttempt, saveAttempt, subscribeAttempts, type AttemptPurpose, type SendAttempt } from "@/services/sendAttempts";
import { checkAttempt } from "@/services/transfer";

const POLL_MS = 10_000;

/**
 * The oldest send whose outcome is still unknown for this purpose, from this
 * tab, an earlier visit or another tab. While there is one, nothing new may be
 * signed for that purpose.
 */
export function useUnsettledAttempt(match: (p: AttemptPurpose) => boolean): SendAttempt | null {
  const id = useSyncExternalStore(subscribeAttempts, () => listAttempts(match)[0]?.paymentId ?? null, () => null);
  return useMemo(() => (id ? loadAttempt(id) : null), [id]);
}

/**
 * Shown instead of a send button while an earlier attempt's outcome is
 * unknown. It asks MAX and the chain what happened, records the transaction
 * if it landed, and only reports "nothing was sent" when the chain proves it.
 */
export function AttemptRecovery({ attempt, onRecorded, onNotSent }: {
  attempt: SendAttempt;
  onRecorded(paymentId: string): void;
  onNotSent(): void;
}) {
  const [txHash, setTxHash] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pasted, setPasted] = useState("");
  const [recording, setRecording] = useState(false);
  const done = useRef(false);
  const cb = useRef({ onRecorded, onNotSent });
  useEffect(() => {
    cb.current = { onRecorded, onNotSent };
  });

  async function record(hash: string): Promise<void> {
    setTxHash(hash);
    if (attempt.namespace === "evm" && !attempt.txHash) saveAttempt({ ...attempt, txHash: hash });
    await max().transfers.submit(attempt.paymentId, hash);
    done.current = true;
    clearAttempt(attempt.paymentId);
    cb.current.onRecorded(attempt.paymentId);
  }

  useEffect(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    async function tick() {
      if (stopped || done.current) return;
      try {
        // Another tab, or an earlier visit, may already have recorded it.
        const p = await max().transfers.get(attempt.paymentId);
        if (p.txHash) {
          done.current = true;
          clearAttempt(attempt.paymentId);
          cb.current.onRecorded(attempt.paymentId);
          return;
        }
        const res = await checkAttempt(attempt);
        if (stopped) return;
        if (res.kind === "landed") await record(res.txHash);
        else if (res.kind === "not_sent") {
          done.current = true;
          clearAttempt(attempt.paymentId);
          cb.current.onNotSent();
          return;
        } else setError(null);
      } catch (err) {
        if (!stopped) setError(errorMessage(err));
      }
      if (!stopped && !done.current) timer = setTimeout(tick, POLL_MS);
    }
    void tick();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- one poll loop per attempt
  }, [attempt.paymentId]);

  async function recordPasted() {
    setRecording(true);
    setError(null);
    try {
      await record(pasted.trim());
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setRecording(false);
    }
  }

  return (
    <Card className="flex flex-col gap-4 p-7">
      <h2 id="confirm" className="m-0 text-xl font-semibold">Checking your last payment</h2>
      <Banner tone="warn" title="Don’t send it again yet.">
        Your wallet may already have sent {attempt.summary}. We’re checking the network, so you won’t pay twice. This usually takes a minute or two
        {attempt.namespace === "evm" ? ", and up to 10 minutes if your wallet never sent it" : ""}.
      </Banner>
      {txHash ? <code className="break-all rounded-xl bg-ivory p-3 font-mono text-[13px]">{txHash}</code> : null}
      {error ? <p role="alert" className="m-0 text-sm text-danger">{error}</p> : null}
      <div className="flex flex-col gap-2">
        <label htmlFor="pasted-hash" className="text-[13px] text-muted">Found it in your wallet’s activity? Paste its transaction hash to record it now.</label>
        <div className="flex gap-2">
          <input id="pasted-hash" value={pasted} onChange={(e) => setPasted(e.target.value)} autoComplete="off" spellCheck={false}
            placeholder={attempt.namespace === "evm" ? "0x…" : "Transaction signature"}
            className="min-h-11 min-w-0 flex-1 rounded-xl border border-line bg-surface px-3 font-mono text-[13px] outline-none focus:border-ink" />
          <Button variant="secondary" loading={recording} disabled={!pasted.trim() || recording} onClick={recordPasted}>Record</Button>
        </div>
      </div>
    </Card>
  );
}
