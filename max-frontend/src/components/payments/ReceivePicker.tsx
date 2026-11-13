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
            {value ? "Converted when you pay" : `On ${sendChain?.name ?? "this network"}, no conversion`}
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
  // null shows the chain grid; a chain key shows that chain's coins.
  const [chainKey, setChainKey] = useState<string | null>(null);
  const name = (key: string) => chains.find((c) => c.key === key)?.name ?? key;
  const current = value ? receiveId(value) : null;
  const needle = q.trim().toLowerCase();

  const byChain = useMemo(() => {
    const m = new Map<string, ReceivableAsset[]>();
    for (const a of assets) m.set(a.chain, [...(m.get(a.chain) ?? []), a]);
    return m;
  }, [assets]);
  const matches = (a: ReceivableAsset) => `${a.symbol} ${name(a.chain)} ${a.address ?? ""}`.toLowerCase().includes(needle);
  const shown = chainKey ? (byChain.get(chainKey) ?? []).filter((a) => !needle || matches(a))
    : needle ? assets.filter(matches) : [];

  return (
    <div className="flex flex-col gap-4">
      <div className="field flex items-center gap-2 rounded-xl border border-line px-3.5 focus-within:border-ink">
        <span className="text-muted"><SearchIcon /></span>
        <input autoFocus value={q} onChange={(e) => setQ(e.target.value)}
          placeholder={chainKey ? `Search ${name(chainKey)}` : "Search a token or chain"}
          aria-label={chainKey ? `Search ${name(chainKey)}` : "Search a token or chain"}
          className="min-h-11 min-w-0 flex-1 border-0 bg-transparent text-[15px] outline-none" />
      </div>

      {chainKey ? (
        <div className="flex items-center gap-2">
          <button type="button" onClick={() => { setChainKey(null); setQ(""); }}
            className="inline-flex items-center gap-1.5 rounded-[10px] px-2 py-1.5 text-sm font-medium text-clay-text hover:bg-sunken">
            <Icon name="arrowRight" size={16} className="rotate-180" />All chains
          </button>
          <span className="flex items-center gap-2 text-[15px] font-semibold">
            <ChainLogo chain={chainKey} size={20} />{name(chainKey)}
          </span>
        </div>
      ) : null}

      {!chainKey && !needle ? (
        <>
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

          {loading ? <p className="m-0 text-sm text-muted">Loading coins…</p> : null}
          {!loading && !byChain.size ? <p className="m-0 text-sm text-muted">Cross-chain delivery isn’t available right now.</p> : null}
          {byChain.size ? (
            <div className="flex flex-col gap-2.5">
              <span className="text-[13px] font-semibold uppercase tracking-[0.06em] text-muted">Or convert on another chain</span>
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
                {[...byChain.entries()].map(([key, list]) => {
                  const c = chains.find((x) => x.key === key);
                  const holds = value?.chain === key;
                  return (
                    <button key={key} type="button" onClick={() => { setChainKey(key); setQ(""); }}
                      className={cx("flex items-center gap-2.5 rounded-xl border px-3 py-2.5 text-left",
                        holds ? "border-accent bg-clay-soft" : "border-line hover:bg-sunken")}>
                      <ChainLogo chain={key} size={26} />
                      <span className="flex min-w-0 flex-col">
                        <span className="truncate text-[14px] font-semibold">{c?.name ?? key}</span>
                        <span className="text-[12px] text-muted">
                          {holds && value ? value.symbol : `${list.length} ${list.length === 1 ? "coin" : "coins"}`}
                        </span>
                      </span>
                    </button>
                  );
                })}
              </div>
            </div>
          ) : null}
        </>
      ) : (
        <div className="flex flex-col gap-1">
          {!shown.length ? <p className="m-0 text-sm text-muted">Nothing matches “{q.trim()}”.</p> : null}
          {shown.map((a) => {
            const on = receiveId(a) === current;
            return (
              <button key={receiveId(a)} type="button" onClick={(e) => onPick(a, e.currentTarget)} aria-pressed={on}
                className={cx("flex items-center gap-3 rounded-xl px-3 py-2.5 text-left", on ? "bg-clay-soft" : "hover:bg-sunken")}>
                {/* Searching across chains, the badge says which chain each coin is on. */}
                <TokenLogo symbol={a.symbol} chain={chainKey ? undefined : a.chain} size={30} />
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="flex items-center gap-1.5 text-[15px] font-semibold">
                    {a.symbol}
                    {!chainKey ? <span className="text-[13px] font-normal text-muted">on {name(a.chain)}</span> : null}
                    {!a.verified ? <span title="Not on MAX’s verified token list. Confirm it with the recipient."
                      className="rounded-full border border-line-strong bg-sunken px-1.5 text-[11px] font-semibold text-body">Unverified</span> : null}
                  </span>
                  <span className="truncate font-mono text-[12px] text-muted">
                    {a.kind === "native" ? `${name(a.chain)} native coin` : shortAddress(a.address ?? "")}
                  </span>
                </span>
                {on ? <Icon name="check" size={18} className="text-clay-text" /> : null}
              </button>
            );
          })}
        </div>
      )}
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
