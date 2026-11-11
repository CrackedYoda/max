"use client";

import { normalizeName, type FeeOption, type MaxId, type RenameView } from "@max/sdk";
import { useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";

import { max } from "@/api/client";
import { keys } from "@/api/keys";
import { Handle } from "@/components/identity/Handle";
import { StatusPill } from "@/components/payments/PaymentRow";
import { Button, ButtonLink } from "@/components/ui/Button";
import { Banner, Card, Skeleton } from "@/components/ui/Blocks";
import { cx } from "@/components/ui/cx";
import { HandleInput } from "@/components/ui/Fields";
import { Icon } from "@/components/ui/Icon";
import { errorMessage } from "@/error/messages";
import { useAccountId, useAvailability, useMyIds, usePayment, useRenameEligibility, useRenameRequest } from "@/hooks/queries";
import { useDebounced } from "@/hooks/useDebounced";
import { useEvmProviders } from "@/hooks/useEvmProviders";
import { AttemptRecovery, useUnsettledAttempt } from "@/components/payments/AttemptRecovery";
import { clearAttempt } from "@/services/sendAttempts";
import { paymentSummary, SendOutcomeUnknown, signAndSend } from "@/services/transfer";
import { connectFor } from "@/services/wallets";
import { useWallet } from "@/store/wallet";
import { formatDate, shortAddress } from "@/utils/format";
import { stripPrefix } from "@/validation/identity";
import { ChainLogo } from "@/components/ui/ChainLogo";

type Phase =
  | { kind: "form" }
  | { kind: "working"; step: "connect" | "reserve" | "sign" | "record" }
  | { kind: "record_failed"; view: RenameView; txHash: string; error: string }
  | { kind: "paid"; requestId: string; paymentId: string };

/**
 * Paid rename: reserve the new name → sign the fee in your wallet → MAX applies
 * the rename once the fee confirms. The old name is quarantined so nobody can
 * grab it and receive payments meant for you.
 */
export function RenameFlow() {
  const { id } = useParams<{ id: string }>();
  const ids = useMyIds();
  const maxId = ids.data?.find((x) => x.id === id);
  const elig = useRenameEligibility(id);

  if (ids.isLoading || elig.isLoading) {
    return (
      <main className="mx-auto flex max-w-[760px] flex-col gap-6 px-6 pb-24 pt-14">
        <Skeleton className="h-12 w-64" />
        <Skeleton className="h-64 rounded-2xl" />
      </main>
    );
  }
  if (!maxId || elig.error || !elig.data) {
    return (
      <main className="mx-auto flex max-w-[760px] flex-col gap-4 px-6 pb-24 pt-14">
        <Banner tone="danger">{elig.error ? errorMessage(elig.error) : "That MAX ID isn’t yours."}</Banner>
        <Link href="/dashboard">Back to dashboard</Link>
      </main>
    );
  }
  return <RenameForm maxId={maxId} eligibility={elig.data} />;
}

function RenameForm({ maxId, eligibility: e }: { maxId: MaxId; eligibility: NonNullable<ReturnType<typeof useRenameEligibility>["data"]> }) {
  const qc = useQueryClient();
  const accountId = useAccountId();
  const evm = useEvmProviders();
  const wallet = useWallet((s) => s.wallet);
  const [q, setQ] = useState("");
  const [fee, setFee] = useState<FeeOption | null>(e.fees[0] ?? null);
  const [phase, setPhase] = useState<Phase>(e.pending ? { kind: "paid", requestId: e.pending.id, paymentId: "" } : { kind: "form" });
  const [error, setError] = useState<string | null>(null);
  // One idempotency key per exact terms, so a retry never reserves twice.
  const [idem, setIdem] = useState<{ fp: string; key: string } | null>(null);

  const debounced = useDebounced(q);
  const name = normalizeName(debounced);
  const avail = useAvailability(debounced);
  const settled = q === debounced;
  const same = name === maxId.name;
  const nameOk = settled && !!name && !same && !!avail.data?.available;
  // A fee payment whose outcome is unknown replaces the form until it's settled.
  const unsettled = useUnsettledAttempt((p) => p.kind === "rename" && p.maxId === maxId.id);
  const recovering = phase.kind === "form" ? unsettled : null;
  const busy = phase.kind === "working" || !!recovering;

  useEffect(() => {
    if (!busy) return;
    const h = (ev: BeforeUnloadEvent) => ev.preventDefault();
    window.addEventListener("beforeunload", h);
    return () => window.removeEventListener("beforeunload", h);
  }, [busy]);

  async function pay() {
    if (!fee || !name) return;
    setError(null);
    let view: RenameView | null = null;
    let txHash: string | null = null;
    try {
      setPhase({ kind: "working", step: "connect" });
      const w = await connectFor(fee.chain.namespace, evm, wallet);
      useWallet.getState().set(w);

      const fp = JSON.stringify([maxId.id, name, fee.chain.key, fee.asset, w.address]);
      const key = idem?.fp === fp ? idem.key : crypto.randomUUID();
      setIdem({ fp, key });

      setPhase({ kind: "working", step: "reserve" });
      view = await max().renames.request(maxId.id, { newName: name, feeChain: fee.chain.key, feeAsset: fee.asset, senderAddress: w.address, idempotencyKey: key });
      if (view.feePayment.txHash) {
        setPhase({ kind: "paid", requestId: view.request.id, paymentId: view.feePayment.id });
        return;
      }

      setPhase({ kind: "working", step: "sign" });
      txHash = await signAndSend(view.feePayment, w, { kind: "rename", maxId: maxId.id, requestId: view.request.id }, paymentSummary(view.feePayment, "MAX"));

      setPhase({ kind: "working", step: "record" });
      await max().transfers.submit(view.feePayment.id, txHash);
      clearAttempt(view.feePayment.id);
      qc.invalidateQueries({ queryKey: keys.rename(accountId, maxId.id) });
      setPhase({ kind: "paid", requestId: view.request.id, paymentId: view.feePayment.id });
    } catch (err) {
      if (err instanceof SendOutcomeUnknown) {
        setPhase({ kind: "form" }); // shows recovery
        return;
      }
      if (view && txHash) {
        // Broadcast already happened; only recording failed. Retry without re-signing.
        setPhase({ kind: "record_failed", view, txHash, error: errorMessage(err) });
      } else {
        setPhase({ kind: "form" });
        setError(errorMessage(err));
      }
    }
  }

  async function retryRecord(view: RenameView, txHash: string) {
    setPhase({ kind: "working", step: "record" });
    try {
      await max().transfers.submit(view.feePayment.id, txHash);
      clearAttempt(view.feePayment.id);
      setPhase({ kind: "paid", requestId: view.request.id, paymentId: view.feePayment.id });
    } catch (err) {
      setPhase({ kind: "record_failed", view, txHash, error: errorMessage(err) });
    }
  }

  if (phase.kind === "paid") return <RenameProgress maxId={maxId} requestId={phase.requestId} />;

  const cooldownUntil = new Date(e.renameAvailableAt);
  const label =
    phase.kind === "working"
      ? { connect: "Connecting wallet…", reserve: "Reserving name…", sign: "Confirm in your wallet…", record: "Recording payment…" }[phase.step]
      : fee
        ? `Pay ${fee.amount} ${fee.symbol} and rename`
        : "Rename";

  return (
    <main className="mx-auto flex max-w-[760px] flex-col gap-8 px-6 pb-24 pt-14">
      <div className="flex flex-col gap-2">
        <Link href="/dashboard" className="inline-flex w-fit items-center gap-1 text-sm font-medium"><span aria-hidden="true">←</span> Dashboard</Link>
        <h1 className="m-0 font-display text-[40px] font-medium tracking-[-0.01em]">Rename <Handle name={maxId.name} /></h1>
        <p className="m-0 text-[15px] leading-relaxed text-muted">
          Your wallets, destinations and profile move to the new name. The old name is held for 30 days so nobody else can catch payments meant for you.
        </p>
      </div>

      {recovering ? (
        <AttemptRecovery attempt={recovering}
          onRecorded={(paymentId) => {
            qc.invalidateQueries({ queryKey: keys.rename(accountId, maxId.id) });
            const purpose = recovering.purpose;
            setPhase({ kind: "paid", requestId: purpose.kind === "rename" ? purpose.requestId : "", paymentId });
          }}
          onNotSent={() => {
            setPhase({ kind: "form" });
            setError("Your fee was never sent. You can pay it again.");
          }} />
      ) : !e.eligible ? (
        <Banner tone="warn" title="Can’t rename right now">
          {e.reason?.startsWith("cooldown")
            ? `Handles can be renamed once every ${e.cooldownHours} hours. You can rename again ${formatDate(e.renameAvailableAt)} at ${cooldownUntil.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" })}.`
            : e.reason ?? "This handle can’t be renamed."}
        </Banner>
      ) : (
        <Card as="form" className="flex flex-col gap-6 p-7">
          <div className="flex flex-col gap-2.5">
            <label htmlFor="new-name" className="text-sm font-semibold">New name</label>
            <HandleInput id="new-name" scale="lg" value={q} disabled={busy} onChange={(ev) => setQ(stripPrefix(ev.target.value))} placeholder="newname" tone={!q || !settled ? "neutral" : nameOk ? "ok" : "bad"} />
            <p aria-live="polite" className={cx("m-0 min-h-5 text-sm", !q || !settled ? "text-muted" : nameOk ? "text-success" : "text-danger")}>
              {!q.trim()
                ? "3–32 lowercase letters, numbers or hyphens."
                : !settled || (name && !avail.data && !same)
                  ? "Checking…"
                  : !name
                    ? "Use 3–32 lowercase letters, numbers or hyphens."
                    : same
                      ? "That’s already this handle’s name."
                      : avail.data?.available
                        ? `${avail.data.handle} is available.`
                        : `${avail.data?.handle} isn’t available.`}
            </p>
          </div>

          <fieldset className="m-0 flex flex-col gap-2 border-0 p-0">
            <legend className="mb-2 text-sm font-semibold">Pay the fee with</legend>
            {e.fees.length ? (
              <div className="grid gap-2 [grid-template-columns:repeat(auto-fit,minmax(220px,1fr))]">
                {e.fees.map((f) => {
                  const on = fee?.chain.key === f.chain.key && fee.asset === f.asset;
                  return (
                    <label key={`${f.chain.key}:${f.asset}`} className={cx("flex cursor-pointer items-center justify-between gap-3 rounded-xl border p-4", on ? "border-2 border-accent bg-clay-soft" : "border-line bg-surface hover:bg-ivory")}>
                      <span className="flex items-center gap-3">
                        <input type="radio" name="fee" checked={on} disabled={busy} onChange={() => setFee(f)} className="size-4 accent-clay" />
                        <span className="flex flex-col">
                          <span className="font-semibold">{f.amount} {f.symbol}</span>
                          <span className="inline-flex items-center gap-1.5 text-[13px] text-muted"><ChainLogo chain={f.chain.key} size={14} />{f.chain.name}</span>
                        </span>
                      </span>
                      <span className="text-xs text-muted">{f.chain.namespace === "solana" ? "Phantom" : "EVM wallet"}</span>
                    </label>
                  );
                })}
              </div>
            ) : (
              <Banner tone="warn">No fee options are configured.</Banner>
            )}
          </fieldset>

          <ul className="m-0 flex list-none flex-col gap-2 rounded-xl bg-ivory p-4 text-sm text-body">
            <li className="flex gap-2"><Icon name="check" className="mt-0.5 shrink-0 text-success" />The rename applies after the fee confirms on-chain.</li>
            <li className="flex gap-2"><Icon name="check" className="mt-0.5 shrink-0 text-success" />Payments already in flight still reach you.</li>
            <li className="flex gap-2"><Icon name="lock" className="mt-0.5 shrink-0 text-muted" />Next rename possible after {e.cooldownHours} hours.</li>
          </ul>

          {phase.kind === "record_failed" ? (
            <Banner tone="warn" title="Paid, but not recorded yet">
              <span className="block">Your fee was sent ({shortAddress(phase.txHash, 6, 6)}). Retrying only tells MAX about it, so you won’t pay twice.</span>
              <span className="block text-[13px]">{phase.error}</span>
            </Banner>
          ) : null}
          {error ? <Banner tone="danger">{error}</Banner> : null}

          {phase.kind === "record_failed" ? (
            <Button className="w-fit" onClick={() => retryRecord(phase.view, phase.txHash)}>Retry recording</Button>
          ) : (
            <Button type="submit" className="w-fit" disabled={!nameOk || !fee} loading={busy} onClick={(ev) => { ev.preventDefault(); void pay(); }}>
              {label}
            </Button>
          )}
        </Card>
      )}
    </main>
  );
}

function RenameProgress({ maxId, requestId }: { maxId: MaxId; requestId: string }) {
  const qc = useQueryClient();
  const accountId = useAccountId();
  const view = useRenameRequest(maxId.id, requestId);
  const payment = usePayment(view.data?.feePayment.id ?? null);
  const status = view.data?.request.status;

  useEffect(() => {
    if (status === "completed") qc.invalidateQueries({ queryKey: keys.account(accountId) });
  }, [status, qc, accountId]);

  if (!view.data) {
    return (
      <main className="mx-auto max-w-[640px] px-6 pb-24 pt-14">
        {view.error ? <Banner tone="danger">{errorMessage(view.error)}</Banner> : <Skeleton className="h-48 rounded-2xl" />}
      </main>
    );
  }
  const r = view.data.request;
  const p = payment.data ?? view.data.feePayment;
  const done = r.status === "completed";
  const failed = r.status === "failed" || r.status === "expired";

  return (
    <main className="mx-auto flex max-w-[640px] flex-col gap-6 px-6 pb-24 pt-14">
      <Card className="flex flex-col items-center gap-5 p-8 text-center">
        <span aria-hidden="true" className={cx("inline-flex size-16 items-center justify-center rounded-full", failed ? "bg-danger-soft text-danger" : done ? "bg-success-soft text-success" : "bg-sunken text-muted")}>
          <Icon name={failed ? "alert" : done ? "check" : "arrowRight"} size={28} strokeWidth={2.5} />
        </span>
        <h1 className="m-0 font-display text-[34px] font-medium">{done ? "Renamed" : failed ? "Rename didn’t go through" : "Waiting for the fee to confirm"}</h1>
        <p className="m-0 text-[16px] text-muted" aria-live="polite">
          <Handle name={r.oldHandle} /> → <Handle name={r.newHandle} />
        </p>
        {!done && !failed ? (
          <>
            <StatusPill status={p.status} />
            <p className="m-0 text-sm text-muted">This page updates on its own. You can leave. The rename finishes without you.</p>
          </>
        ) : null}
        {failed ? <Banner tone="danger">{r.failureReason ?? "The fee wasn’t confirmed in time. Your handle is unchanged."}</Banner> : null}
        {p.explorerUrl ? <a href={p.explorerUrl} target="_blank" rel="noreferrer" className="text-sm font-semibold">View fee transaction ↗</a> : null}
        <div className="flex flex-wrap justify-center gap-2">
          <ButtonLink href="/dashboard" variant={done ? "primary" : "secondary"} size="sm">Back to dashboard</ButtonLink>
          {done ? <ButtonLink href={`/${r.newHandle}`} variant="secondary" size="sm">View profile</ButtonLink> : null}
        </div>
      </Card>
    </main>
  );
}
