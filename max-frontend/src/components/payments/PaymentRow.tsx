import type { PaymentStatus } from "@max/sdk";

import { Pill } from "../ui/Blocks";

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
