"use client";

import { isPendingApproval, type SaleView } from "@max/sdk";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";

import { max } from "@/api/client";
import { marketErrorMessage } from "@/error/market";
import { SALE_TERMINAL, useInvalidateMarket, useSale, useTrustedDecimals, useWallets } from "@/hooks/queries";
import { findSaleSignature, MaybeSentError, signSale } from "@/services/market";
import { useSession } from "@/store/session";
import { shortAddress } from "@/utils/format";
import { displayDecimal, pinnedPriceBase } from "@/utils/money";

import { Handle } from "../identity/Handle";
import { Button, ButtonLink, Spinner } from "../ui/Button";
import { Banner, ReviewRow } from "../ui/Blocks";
import { CopyButton } from "../ui/Copy";
import { Dialog } from "../ui/Dialog";
import { Icon } from "../ui/Icon";

import { connectLinkedSolana, SaleStatusPill, useCountdown, useIdempotencyKey, useNative } from "./shared";

/**
 * What the user clicked. These are the values the transaction is checked
 * against before signing, so they come from the listing/offer shown, never
 * from the sale MAX returns. `price` is the decimal amount shown to the user;
 * it is converted with trusted decimals (env pin / chain), and
 * `priceBaseUnits`, when the API sent one, must agree with that conversion.
 */
type Terms = { handle: string; chain: string; asset: string; symbol: string; price: string; priceBaseUnits?: string };
export type PayTarget =
  | (Terms & { kind: "listing"; listingId: string })
  | (Terms & { kind: "offer"; offerId: string; fromAddress: string; priceBaseUnits: string })
  | (Terms & { kind: "sale"; saleId: string });

type Phase =
  | { k: "intro" }
  | { k: "working"; step: "connect" | "lock" | "sign" | "record"; view?: SaleView }
  | { k: "review"; view: SaleView }
  | { k: "held"; saleId: string }
  | { k: "record_failed"; view: SaleView; txHash: string; error: string }
  /** The wallet errored after signing began: the payment may be on chain. Never offer a plain re-sign. */
  | { k: "uncertain"; view: SaleView; lookup: "checking" | "not_found" | "failed" }
  | { k: "watching"; saleId: string };

const STEP: Record<"connect" | "lock" | "sign" | "record", string> = {
  connect: "Connecting Phantom…",
  lock: "Reserving the name for you…",
  sign: "Confirm in Phantom…",
  record: "Recording the payment with MAX…",
};

export function BuyModal({ target, onClose }: { target: PayTarget; onClose(): void }) {
  const status = useSession((s) => s.status);
  const path = usePathname();
  const wallets = useWallets();
  const invalidate = useInvalidateMarket();
  const keyFor = useIdempotencyKey();
  const native = useNative(target.chain);
  const [phase, setPhase] = useState<Phase>({ k: "intro" });
  // The raw error, formatted at render so late-arriving token decimals still apply.
  const [failure, setFailure] = useState<unknown>(null);

  const trusted = useTrustedDecimals(target.chain, target.asset);
  const decimals = trusted.decimals;
  const pin = decimals != null ? pinnedPriceBase(target.price, decimals, target.priceBaseUnits) : null;
  const [confirmedRetry, setConfirmedRetry] = useState(false);
  const units = { decimals, symbol: target.symbol, nativeSymbol: native.symbol, nativeDecimals: native.decimals, amount: target.price };
  const error = failure == null ? null : typeof failure === "string" ? failure : marketErrorMessage(failure, units);
  const setError = setFailure;

  const watchId = phase.k === "held" || phase.k === "watching" ? phase.saleId : null;
  const sale = useSale(watchId);
  // An approved review sale comes back locked with its instructions: pay it here.
  const approved = phase.k === "held" && sale.data?.sale.status === "locked" && !sale.data.sale.txHash && sale.data.instructions ? sale.data : null;
  const reviewView = phase.k === "review" || (phase.k === "working" && phase.view) ? phase.view! : approved;

  const busy = phase.k === "working" && (phase.step === "sign" || phase.step === "record");
  useEffect(() => {
    if (!busy) return;
    const h = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", h);
    return () => window.removeEventListener("beforeunload", h);
  }, [busy]);

  const terminal = sale.data && SALE_TERMINAL.includes(sale.data.sale.status);
  useEffect(() => {
    if (terminal) invalidate();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [terminal]);

  function advance(view: SaleView) {
    const s = view.sale;
    if (s.status === "pending_review") setPhase({ k: "held", saleId: s.id });
    else if (s.status === "locked" && !s.txHash && view.instructions) setPhase({ k: "review", view });
    else setPhase({ k: "watching", saleId: s.id });
  }

  async function start() {
    setError(null);
    try {
      setPhase({ k: "working", step: "connect" });
      const w = await connectLinkedSolana(wallets.data ?? [], target.kind === "offer" ? target.fromAddress : undefined);
      setPhase({ k: "working", step: "lock" });
      const res =
        target.kind === "listing"
          ? await max().market.buy(target.listingId, { fromAddress: w.address, idempotencyKey: keyFor(JSON.stringify(["buy", target.listingId, w.address])) })
          : target.kind === "offer"
            ? await max().market.payOffer(target.offerId, { idempotencyKey: keyFor(JSON.stringify(["pay", target.offerId])) })
            : await max().market.getSale(target.saleId);
      if (isPendingApproval(res)) {
        setPhase({ k: "intro" });
        setError("This is waiting for approval by the agent’s administrator.");
        return;
      }
      invalidate();
      advance(res);
    } catch (err) {
      setPhase({ k: "intro" });
      setError(err);
    }
  }

  async function pay(view: SaleView) {
    setError(null);
    setConfirmedRetry(false);
    if (trusted.error) return setError(trusted.error);
    if (!pin) return setError("Couldn’t read this token’s decimals from the network, so the price can’t be checked. Nothing was signed.");
    if ("error" in pin) return setError(pin.error);
    try {
      setPhase({ k: "working", step: "connect", view });
      const w = await connectLinkedSolana(wallets.data ?? [], view.sale.buyerAddress);
      setPhase({ k: "working", step: "sign", view });
      const sig = await signSale(view, w, { asset: target.asset, priceBaseUnits: pin.base });
      await record(view, sig);
    } catch (err) {
      if (err instanceof MaybeSentError) {
        setError(err);
        await lookForPayment(view);
        return;
      }
      setPhase({ k: "review", view });
      setError(err);
    }
  }

  /** After an uncertain wallet error: find the sale's tx on chain before anyone signs again. */
  async function lookForPayment(view: SaleView) {
    setPhase({ k: "uncertain", view, lookup: "checking" });
    try {
      const sig = await findSaleSignature(view);
      if (sig) {
        setError(null);
        await record(view, sig);
      } else setPhase({ k: "uncertain", view, lookup: "not_found" });
    } catch {
      setPhase({ k: "uncertain", view, lookup: "failed" });
    }
  }

  async function record(view: SaleView, txHash: string) {
    setPhase({ k: "working", step: "record", view });
    try {
      await max().market.submit(view.sale.id, txHash);
      setPhase({ k: "watching", saleId: view.sale.id });
    } catch (err) {
      // Already broadcast: only MAX's record of it failed. Never sign again.
      setPhase({ k: "record_failed", view, txHash, error: marketErrorMessage(err, units) });
    }
  }

  const title = target.kind === "offer" || target.kind === "sale" ? <>Pay for <Handle name={target.handle} /></> : <>Buy <Handle name={target.handle} /></>;

  return (
    <Dialog title={title} labelId="buy-title" onClose={onClose} locked={busy}>
      <div aria-live="polite" className="flex flex-col gap-5">
        {status !== "authenticated" ? (
          <>
            <PriceLine price={target.price} symbol={target.symbol} />
            <p className="m-0 text-[15px] text-muted">Sign in with a wallet to buy. Browsing the market doesn’t need one.</p>
            <ButtonLink href={`/connect?next=${encodeURIComponent(path)}`}>Sign in to buy</ButtonLink>
          </>
        ) : phase.k === "intro" || (phase.k === "working" && !phase.view) ? (
          <>
            <PriceLine price={target.price} symbol={target.symbol} />
            <p className="m-0 text-[15px] leading-relaxed text-muted">
              {target.kind === "listing"
                ? "Continuing reserves the name for you for 15 minutes and shows the full quote, including the network fee, before anything is signed."
                : "Continuing fetches a fresh quote, including the network fee, before anything is signed."}
            </p>
            <p className="m-0 text-[13px] text-muted">You pay from a Solana wallet linked to your MAX account, with Phantom. MAX never holds funds: the payment goes straight to the seller and the MAX fee.</p>
            {error ? <Banner tone="danger">{error}</Banner> : null}
            <Button onClick={start} loading={phase.k === "working"}>{phase.k === "working" ? STEP[phase.step] : "Continue"}</Button>
          </>
        ) : reviewView ? (
          <Review view={reviewView} target={target} nativeSymbol={native.symbol} error={error}
            working={phase.k === "working" ? STEP[phase.step] : null} onPay={() => pay(reviewView)} />
        ) : phase.k === "held" ? (
          <>
            <Banner tone="info" title="Held for review">
              Large sales are checked by MAX before payment. Keep this open, or come back from your dashboard: once it’s approved you’ll have 15 minutes to pay. Nothing has been charged.
            </Banner>
            <p className="m-0 flex items-center gap-2 text-sm text-muted"><Spinner />Checking every few seconds…</p>
            {sale.data && sale.data.sale.status !== "pending_review" && sale.data.sale.status !== "locked" ? <SaleOutcome view={sale.data} handle={target.handle} /> : null}
          </>
        ) : phase.k === "uncertain" ? (
          <>
            <Banner tone="warn" title="Your payment may already have been sent">
              {error ?? "Phantom reported an error after signing started."} Don’t pay again until you’ve checked.
            </Banner>
            {phase.lookup === "checking" ? (
              <p className="m-0 flex items-center gap-2 text-sm text-muted"><Spinner />Looking for this payment in {shortAddress(phase.view.sale.buyerAddress)}’s recent transactions…</p>
            ) : (
              <>
                <p className="m-0 text-[15px] leading-relaxed">
                  {phase.lookup === "failed"
                    ? "We couldn’t reach the network to check. Check your wallet before retrying: open Phantom’s activity and look for this payment."
                    : `No payment for this sale shows up yet among ${shortAddress(phase.view.sale.buyerAddress)}’s last 20 transactions. A payment can take a minute to appear: check Phantom’s activity, then check again.`}
                </p>
                <Button variant="secondary" onClick={() => lookForPayment(phase.view)}>Check again</Button>
                <label className="flex items-start gap-2.5 text-sm">
                  <input type="checkbox" className="mt-0.5 size-4 shrink-0" checked={confirmedRetry} onChange={(e) => setConfirmedRetry(e.target.checked)} />
                  I checked my wallet and this payment isn’t there. Paying again could otherwise charge me twice.
                </label>
                <Button variant="danger" disabled={!confirmedRetry} onClick={() => pay(phase.view)}>Sign again</Button>
              </>
            )}
          </>
        ) : phase.k === "record_failed" ? (
          <>
            <Banner tone="warn" title="Your payment was sent, but MAX hasn’t recorded it yet.">
              Don’t pay again. This only links the transaction to the sale so it can be matched.
            </Banner>
            <code className="break-all rounded-xl bg-ivory p-3 font-mono text-[13px]">{phase.txHash}</code>
            <p role="alert" className="m-0 text-sm text-danger">{phase.error}</p>
            <Button onClick={() => record(phase.view, phase.txHash)}>Try recording again</Button>
          </>
        ) : phase.k === "watching" ? (
          sale.data ? <SaleOutcome view={sale.data} handle={target.handle} /> : <p className="m-0 flex items-center gap-2 text-sm text-muted"><Spinner />Loading the sale…</p>
        ) : null}
      </div>
    </Dialog>
  );
}

function PriceLine({ price, symbol }: { price: string; symbol: string }) {
  return (
    <div className="flex items-baseline gap-2">
      <span className="tabular text-[32px] font-semibold">{displayDecimal(price)}</span>
      <span className="text-lg font-semibold text-muted">{symbol}</span>
    </div>
  );
}

function Review({ view, target, nativeSymbol, error, working, onPay }: {
  view: SaleView; target: PayTarget; nativeSymbol: string; error: string | null; working: string | null; onPay(): void;
}) {
  const s = view.sale;
  const q = view.quote;
  const { label, secondsLeft } = useCountdown(s.lockExpiresAt);
  const expired = !!s.lockExpiresAt && secondsLeft <= 0;
  const sym = target.symbol;
  return (
    <>
      {expired ? (
        <Banner tone="danger" title="The payment window closed.">The name was released. Nothing was charged. Close this and start again if it’s still listed.</Banner>
      ) : label ? (
        <p className="m-0 flex items-center gap-2 self-start rounded-[10px] bg-clay-soft px-3 py-2 text-sm font-semibold text-clay-text">
          <Icon name="lock" size={16} />Reserved for you · <span className="tabular">{label}</span> left to pay
        </p>
      ) : null}
      <dl className="m-0 grid grid-cols-2 gap-x-4 gap-y-4 rounded-xl bg-ivory p-4">
        <ReviewRow label="Price" value={`${displayDecimal(target.price)} ${sym}`} />
        <ReviewRow label="Network fee" value={q ? `≈ ${q.networkFee} ${nativeSymbol}` : "—"} />
        <ReviewRow label="Total you pay" value={q ? `${displayDecimal(target.price)} ${sym} + ${q.totalNative} ${nativeSymbol}` : `${displayDecimal(target.price)} ${sym}`} />
        <ReviewRow label="Seller receives" value={`${displayDecimal(q?.sellerReceives ?? s.sellerReceives)} ${sym}`} />
      </dl>
      <p className="m-0 text-[13px] leading-relaxed text-muted">
        The seller pays MAX’s {s.feeBps / 100}% fee ({displayDecimal(q?.fee ?? s.fee)} {sym}) out of the price. Paying from <span className="font-mono" title={s.buyerAddress}>{shortAddress(s.buyerAddress)}</span>.
        The transaction is checked against this price before Phantom opens.
      </p>
      {error ? <Banner tone="danger">{error}</Banner> : null}
      <Button onClick={onPay} disabled={expired} loading={!!working}>{working ?? `Sign and pay ${displayDecimal(target.price)} ${sym}`}</Button>
    </>
  );
}

function SaleOutcome({ view, handle }: { view: SaleView; handle: string }) {
  const s = view.sale;
  const copy: Record<string, { tone: "info" | "success" | "warn" | "danger"; title: string; body: string }> = {
    submitted: { tone: "info", title: "Payment sent", body: "Waiting for the network to confirm it. You can close this; the sale finishes on its own." },
    confirming: { tone: "info", title: "Confirming on the network", body: `${s.confirmations} confirmation${s.confirmations === 1 ? "" : "s"} so far. You can close this; the sale finishes on its own.` },
    locked: { tone: "info", title: "Waiting for the payment", body: "MAX hasn’t seen the transaction yet." },
    completed: { tone: "success", title: `max.${handle.replace(/^max\./, "")} is yours`, body: "It’s in your dashboard. Its payment details were reset: pick where it receives." },
    expired: { tone: "danger", title: "The payment window closed", body: "If you already sent the payment, MAX will still match it. Otherwise nothing was charged." },
    cancelled: { tone: "danger", title: "This sale was cancelled", body: s.statusReason ?? "The name was released." },
    manual_review: { tone: "warn", title: "This sale needs a manual check", body: "Your payment arrived but the sale couldn’t complete automatically. MAX will contact you about the outcome." },
    reversed: { tone: "danger", title: "This sale was reversed", body: s.statusReason ?? "The name went back to the seller." },
  };
  const c = copy[s.status] ?? { tone: "info" as const, title: "In progress", body: "" };
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2"><SaleStatusPill status={s.status} />{!SALE_TERMINAL.includes(s.status) ? <Spinner className="text-muted" /> : null}</div>
      <Banner tone={c.tone} title={c.title}>{c.body}</Banner>
      {s.txHash ? (
        <div className="flex items-center gap-2">
          <code className="min-w-0 flex-1 truncate rounded-xl bg-ivory p-3 font-mono text-[13px]" title={s.txHash}>{s.txHash}</code>
          <CopyButton value={s.txHash} label="Copy transaction signature" />
        </div>
      ) : null}
      {s.status === "completed" ? <ButtonLink href="/dashboard">Go to dashboard</ButtonLink> : null}
    </div>
  );
}
