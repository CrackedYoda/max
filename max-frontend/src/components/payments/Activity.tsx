import type { Payment } from "@max/sdk";

import { CopyButton } from "@/components/ui/Copy";
import { cx } from "@/components/ui/cx";
import { Icon } from "@/components/ui/Icon";
import { TokenLogo } from "@/components/ui/TokenLogo";
import { formatDate, shortAddress } from "@/utils/format";

import { StatusPill } from "./PaymentRow";

export type ActivityItem = { payment: Payment; direction: "sent" | "received" };

/** The owner's payments, newest first, grouped by day. */
export function ActivityFeed({ items }: { items: ActivityItem[] }) {
  const sorted = [...items].sort((a, b) => b.payment.createdAt.localeCompare(a.payment.createdAt));
  const days = new Map<string, ActivityItem[]>();
  for (const it of sorted) {
    const day = dayLabel(it.payment.createdAt);
    days.set(day, [...(days.get(day) ?? []), it]);
  }
  return (
    <div className="flex flex-col gap-6">
      {[...days.entries()].map(([day, list]) => (
        <section key={day} aria-label={day} className="flex flex-col gap-1">
          <h3 className="m-0 px-1 pb-1 text-[13px] font-semibold uppercase tracking-[0.06em] text-muted">{day}</h3>
          <ul className="m-0 flex list-none flex-col gap-1 p-0">
            {list.map((it) => <ActivityRow key={`${it.direction}-${it.payment.id}`} {...it} />)}
          </ul>
        </section>
      ))}
    </div>
  );
}

function ActivityRow({ payment: p, direction }: ActivityItem) {
  const out = direction === "sent";
  const cc = p.crossChain;
  // The recipient sees what arrived; the sender sees what left their wallet.
  const shown = !out && cc
    ? { amount: (cc.amountOut ?? cc.expectedAmountOut).display, asset: cc.destinationAsset, chain: cc.destinationChain }
    : { amount: p.amount.display, asset: p.asset, chain: p.chain };
  const symbol = shown.asset.symbol ?? (shown.asset.kind === "native" ? shown.chain.nativeCoin : shortAddress(shown.asset.address ?? ""));
  const trusted = shown.asset.kind === "native" || shown.asset.verified;
  const tx = !out && cc?.destinationTxs[0] ? cc.destinationTxs[0] : p.txHash ? { hash: p.txHash, explorerUrl: p.explorerUrl } : null;
  const type = p.purpose === "rename_fee" ? "Rename fee" : cc ? "Converted" : out ? "Sent" : "Received";
  const counterparty = out ? p.recipient.handle : shortAddress(p.senderAddress);

  return (
    <li className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-xl px-3 py-3 hover:bg-sunken/60">
      <span className="relative">
        <TokenLogo symbol={symbol} chain={shown.chain.key} size={38} plain={!trusted} />
        <span aria-hidden="true" className={cx("absolute -left-1 -top-1 flex size-[18px] items-center justify-center rounded-full border-2 border-surface",
          out ? "bg-ink text-white" : "bg-success text-white")}>
          <Icon name={out ? "send" : "receive"} size={10} strokeWidth={2.5} />
        </span>
      </span>

      <div className="flex min-w-[180px] flex-1 flex-col gap-1">
        <span className="flex flex-wrap items-center gap-2 text-[15px] font-semibold">
          <span className="truncate">{out ? "To" : "From"} <span className="font-mono font-medium">{counterparty}</span></span>
          <span className={cx("rounded-full px-2 py-[1px] text-[11px] font-semibold",
            type === "Converted" ? "bg-clay-soft text-clay-text" : type === "Rename fee" ? "bg-sunken text-body" : out ? "bg-sunken text-body" : "bg-success-soft text-success")}>
            {type}
          </span>
        </span>
        <span className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[13px] text-muted">
          <span>{shown.chain.name}</span>
          <span aria-hidden="true">·</span>
          <span>{timeOf(p.createdAt)}</span>
          {cc ? (
            <>
              <span aria-hidden="true">·</span>
              <span>{out
                ? `→ ${(cc.amountOut ?? cc.expectedAmountOut).display} ${cc.destinationAsset.symbol ?? cc.destinationChain.nativeCoin} on ${cc.destinationChain.name}`
                : `paid in ${p.asset.symbol ?? p.chain.nativeCoin} on ${p.chain.name}`}</span>
            </>
          ) : null}
        </span>
        {tx ? (
          <span className="flex items-center gap-1.5 text-[12px] text-muted">
            <span className="font-mono">{shortAddress(tx.hash, 6, 6)}</span>
            <CopyButton value={tx.hash} label="Copy transaction hash" className="!size-6" />
            {tx.explorerUrl ? (
              <a href={tx.explorerUrl} target="_blank" rel="noreferrer" aria-label="View on explorer"
                className="inline-flex size-6 items-center justify-center rounded-md !text-muted hover:bg-sunken"><Icon name="external" size={13} /></a>
            ) : null}
          </span>
        ) : null}
      </div>

      <div className="ml-auto flex flex-col items-end gap-1.5">
        <span className={cx("tabular text-[15px] font-semibold", out ? "text-ink" : "text-success")}>
          {out ? "−" : "+"}{shown.amount} {symbol}
        </span>
        <StatusPill status={p.status} />
      </div>
    </li>
  );
}

function dayLabel(iso: string): string {
  const d = new Date(iso);
  const today = new Date();
  const yesterday = new Date(today.getTime() - 86_400_000);
  if (d.toDateString() === today.toDateString()) return "Today";
  if (d.toDateString() === yesterday.toDateString()) return "Yesterday";
  return formatDate(iso);
}

const timeOf = (iso: string) => new Date(iso).toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
