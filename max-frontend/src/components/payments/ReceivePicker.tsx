"use client";

import type { Chain, ReceivableAsset } from "@max/sdk";
import { useMemo, useState } from "react";

import { ChainLogo } from "@/components/ui/ChainLogo";
import { cx } from "@/components/ui/cx";
import { closeDialog, Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import { TokenLogo } from "@/components/ui/TokenLogo";
import { shortAddress } from "@/utils/format";

/** Stable identity for a receivable asset: chain plus "native" or the token address. */
export const receiveId = (a: ReceivableAsset) => `${a.chain}:${a.kind === "native" ? "native" : (a.address ?? "").toLowerCase()}`;

/**
 * "Recipient receives": the same asset on the sending network, or any asset a
 * cross-chain route can deliver, picked from a searchable list grouped by chain.
 */
export function ReceivePicker({ value, onChange, sendChain, chains, assets, loading, disabled }: {
  value: ReceivableAsset | null;
  onChange(a: ReceivableAsset | null): void;
  sendChain?: Chain;
  chains: Chain[];
  assets: ReceivableAsset[];
  loading?: boolean;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const chainName = (key: string) => chains.find((c) => c.key === key)?.name ?? key;

  return (
    <>
      <button type="button" id="receive" disabled={disabled} onClick={() => setOpen(true)} aria-haspopup="dialog"
        className="flex min-h-14 items-center gap-3 rounded-xl border border-line bg-surface px-4 py-2.5 text-left hover:bg-sunken disabled:opacity-60">
        {value ? <TokenLogo symbol={value.symbol} chain={value.chain} size={30} />
          : sendChain ? <ChainLogo chain={sendChain.key} size={26} /> : null}
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="text-[15px] font-semibold">{value ? `${value.symbol} on ${chainName(value.chain)}` : "The same asset"}</span>
          <span className="truncate text-[13px] text-muted">
            {value ? "Converted through NEAR Intents" : `On ${sendChain?.name ?? "this network"}, no conversion`}
          </span>
        </span>
        <Icon name="chevronDown" size={18} className="text-muted" />
      </button>
      {open ? (
        <Dialog title="Recipient receives" labelId="receive-title" onClose={() => setOpen(false)} width={560}>
          <ReceiveList value={value} sendChain={sendChain} chains={chains} assets={assets} loading={loading}
            onPick={(a, el) => { onChange(a); closeDialog(el); }} />
        </Dialog>
      ) : null}
    </>
  );
}

function ReceiveList({ value, sendChain, chains, assets, loading, onPick }: {
  value: ReceivableAsset | null;
  sendChain?: Chain;
  chains: Chain[];
  assets: ReceivableAsset[];
  loading?: boolean;
  onPick(a: ReceivableAsset | null, el: HTMLElement): void;
}) {
  const [q, setQ] = useState("");
  const groups = useMemo(() => {
    const needle = q.trim().toLowerCase();
    const byChain = new Map<string, ReceivableAsset[]>();
    for (const a of assets) {
      const name = chains.find((c) => c.key === a.chain)?.name ?? a.chain;
      if (needle && !`${a.symbol} ${name} ${a.address ?? ""}`.toLowerCase().includes(needle)) continue;
      byChain.set(a.chain, [...(byChain.get(a.chain) ?? []), a]);
    }
    return [...byChain.entries()];
  }, [assets, chains, q]);
  const current = value ? receiveId(value) : null;

  return (
    <div className="flex flex-col gap-4">
      <div className="field flex items-center gap-2 rounded-xl border border-line px-3.5 focus-within:border-ink">
        <span className="text-muted"><SearchIcon /></span>
        <input autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search a token or chain"
          aria-label="Search a token or chain" className="min-h-11 min-w-0 flex-1 border-0 bg-transparent text-[15px] outline-none" />
      </div>

      {!q.trim() ? (
        <button type="button" onClick={(e) => onPick(null, e.currentTarget)} aria-pressed={!current}
          className={cx("flex items-center gap-3 rounded-xl border px-3.5 py-3 text-left",
            !current ? "border-accent bg-clay-soft" : "border-line hover:bg-sunken")}>
          {sendChain ? <ChainLogo chain={sendChain.key} size={28} /> : null}
          <span className="flex flex-1 flex-col">
            <span className="text-[15px] font-semibold">The same asset</span>
            <span className="text-[13px] text-muted">Delivered on {sendChain?.name ?? "this network"}, no conversion</span>
          </span>
          {!current ? <Icon name="check" size={18} className="text-clay-text" /> : null}
        </button>
      ) : null}

      {loading ? <p className="m-0 text-sm text-muted">Loading what NEAR Intents can deliver…</p> : null}
      {!loading && !groups.length ? <p className="m-0 text-sm text-muted">{q.trim() ? `Nothing matches “${q.trim()}”.` : "Cross-chain delivery isn’t available right now."}</p> : null}

      {groups.map(([chainKey, list]) => {
        const c = chains.find((x) => x.key === chainKey);
        return (
          <section key={chainKey} aria-label={c?.name ?? chainKey} className="flex flex-col gap-1">
            <h3 className="m-0 flex items-center gap-2 px-1 pb-1 text-[13px] font-semibold uppercase tracking-[0.06em] text-muted">
              <ChainLogo chain={chainKey} size={16} />{c?.name ?? chainKey}
              {c?.receiveOnly ? <span className="rounded-full bg-sunken px-1.5 text-[11px] normal-case tracking-normal">receive only</span> : null}
            </h3>
            {list.map((a) => {
              const on = receiveId(a) === current;
              return (
                <button key={receiveId(a)} type="button" onClick={(e) => onPick(a, e.currentTarget)} aria-pressed={on}
                  className={cx("flex items-center gap-3 rounded-xl px-3 py-2.5 text-left",
                    on ? "bg-clay-soft" : "hover:bg-sunken")}>
                  <TokenLogo symbol={a.symbol} size={30} />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="flex items-center gap-1.5 text-[15px] font-semibold">
                      {a.symbol}
                      {!a.verified ? <span title="Not on MAX’s verified token list. Confirm it with the recipient."
                        className="rounded-full border border-line-strong bg-sunken px-1.5 text-[11px] font-semibold text-body">Unverified</span> : null}
                    </span>
                    <span className="truncate font-mono text-[12px] text-muted">
                      {a.kind === "native" ? `${c?.name ?? chainKey} native coin` : shortAddress(a.address ?? "")}
                    </span>
                  </span>
                  {on ? <Icon name="check" size={18} className="text-clay-text" /> : null}
                </button>
              );
            })}
          </section>
        );
      })}
    </div>
  );
}

function SearchIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
      <circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" />
    </svg>
  );
}
