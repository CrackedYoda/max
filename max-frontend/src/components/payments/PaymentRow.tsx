import type { Payment, PaymentStatus } from "@max/sdk";

import { relativeTime, shortAddress } from "@/utils/format";

import { Pill } from "../ui/Blocks";
import { Icon } from "../ui/Icon";

const LABELS: Record<PaymentStatus, string> = {
  CREATED: "Awaiting approval",
  QUOTED: "Quoted",
  AWAITING_SENDER_SIGNATURE: "Not signed",
  SUBMITTED: "Submitted",
  CONFIRMING: "Confirming",
  COMPLETED: "Completed",
  EXPIRED: "Expired",
  FAILED: "Failed",
  REFUND_PENDING: "Refund pending",
  REFUNDED: "Refunded",
  MANUAL_REVIEW: "Under review",
};

export function statusLabel(s: PaymentStatus): string {
  return LABELS[s] ?? s;
}

export function StatusPill({ status }: { status: PaymentStatus }) {
  const tone = status === "COMPLETED" ? "success" : status === "FAILED" || status === "EXPIRED" ? "danger" : status === "MANUAL_REVIEW" || status === "REFUND_PENDING" ? "clay" : "neutral";
  return <Pill tone={tone} className="!px-2.5 !py-1 !text-xs">{statusLabel(status)}</Pill>;
}

export function PaymentRow({ payment, direction }: { payment: Payment; direction: "sent" | "received" }) {
  const out = direction === "sent";
  const who = out ? payment.recipient.handle : shortAddress(payment.senderAddress);
  const symbol = payment.asset.symbol ?? (payment.asset.address ? shortAddress(payment.asset.address) : "");
  return (
    <li className="flex items-center gap-3 border-b border-sunken px-1 py-3 last:border-b-0">
      <span aria-hidden="true" className={`inline-flex size-9 shrink-0 items-center justify-center rounded-full ${out ? "bg-sunken text-body" : "bg-success-soft text-success"}`}>
        <Icon name={out ? "send" : "receive"} />
      </span>
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-sm font-medium">{out ? "To" : "From"} <span className="font-mono">{who}</span></span>
        <span className="text-xs text-muted">{payment.chain.name} · {relativeTime(payment.createdAt)}</span>
      </div>
      <StatusPill status={payment.status} />
      <span className={`tabular w-[150px] text-right text-sm font-semibold ${out ? "text-ink" : "text-success"}`}>
        {out ? "−" : "+"}{payment.amount.display} {symbol}
      </span>
      {payment.explorerUrl ? (
        <a href={payment.explorerUrl} target="_blank" rel="noreferrer" aria-label="View on explorer" className="inline-flex size-9 items-center justify-center rounded-lg !text-muted hover:bg-sunken"><Icon name="external" /></a>
      ) : <span className="size-9" />}
    </li>
  );
}
