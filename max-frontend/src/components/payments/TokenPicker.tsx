"use client";

import type { Holding } from "@max/sdk";

import { cx } from "../ui/cx";
import { TokenLogo } from "../ui/TokenLogo";

/** USD with cents, or "<$0.01" for dust. */
export function usd(v: string | number | null | undefined): string | null {
  const n = typeof v === "number" ? v : v == null ? NaN : Number(v);
  if (!Number.isFinite(n)) return null;
  if (n > 0 && n < 0.01) return "<$0.01";
  return n.toLocaleString(undefined, { style: "currency", currency: "USD", minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

export function holdingKey(h: Pick<Holding, "kind" | "address">): string {
  return h.kind === "native" ? "native" : h.address.toLowerCase();
}

/** The wallet's tokens on one network, richest first. */
export function TokenPicker({ holdings, selected, disabled, onSelect }: {
  holdings: Holding[]; selected: string; disabled?: boolean; onSelect(h: Holding): void;
}) {
  return (
    <ul role="radiogroup" aria-label="Your tokens" className="m-0 flex max-h-[296px] list-none flex-col gap-1 overflow-y-auto rounded-xl border border-line p-1.5">
      {holdings.map((h) => {
        const on = holdingKey(h) === selected;
        return (
          <li key={holdingKey(h)}>
            <button type="button" role="radio" aria-checked={on} disabled={disabled} onClick={() => onSelect(h)}
              className={cx("flex min-h-12 w-full items-center gap-3 rounded-lg px-2.5 text-left",
                on ? "bg-clay-soft" : "hover:bg-sunken")}>
              {h.logo ? (
                // eslint-disable-next-line @next/next/no-img-element -- token logos come from many hosts
                <img src={h.logo} alt="" width={28} height={28} className="size-7 shrink-0 rounded-full bg-sunken object-cover" />
              ) : h.kind === "native" || h.verified ? (
                // Only trusted coins get a logo by symbol; an unverified "USDC" must not look like the real one.
                <TokenLogo symbol={h.symbol} size={28} />
              ) : (
                <span aria-hidden="true" className="inline-flex size-7 shrink-0 items-center justify-center rounded-full bg-sunken text-[11px] font-semibold text-muted">{h.symbol.slice(0, 3)}</span>
              )}
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="flex items-center gap-1.5 text-[15px] font-semibold text-ink">
                  <span className="truncate">{h.symbol || "Unknown"}</span>
                  {h.verified ? null : <span className="shrink-0 rounded-full bg-sunken px-1.5 text-[11px] font-semibold text-muted">Unverified</span>}
                </span>
                <span className="truncate text-[13px] text-muted">{h.name}</span>
              </span>
              <span className="flex shrink-0 flex-col items-end">
                <span className="tabular text-[15px] font-medium text-ink">{h.balance.display}</span>
                <span className="text-[13px] text-muted">{usd(h.valueUsd) ?? "No price"}</span>
              </span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}

/** Wallet balance of the chosen token, its USD worth, and a Max shortcut. */
export function BalanceBar({ holding, onMax, disabled }: { holding: Holding; onMax(): void; disabled?: boolean }) {
  const worth = usd(holding.valueUsd);
  return (
    <div className="flex items-center justify-between gap-3 rounded-xl bg-sunken px-4 py-2.5 text-sm">
      <span className="min-w-0 truncate text-body">
        Balance <span className="tabular font-semibold text-ink">{holding.balance.display} {holding.symbol}</span>
        {worth ? <span className="text-muted"> · ≈ {worth}</span> : <span className="text-muted"> · No price</span>}
      </span>
      <button type="button" onClick={onMax} disabled={disabled || holding.balance.raw === "0"}
        className="shrink-0 rounded-lg px-2.5 py-1 text-[13px] font-semibold text-clay-text hover:bg-clay-soft disabled:opacity-50">Max</button>
    </div>
  );
}
