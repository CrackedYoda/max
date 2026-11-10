"use client";

import { looksLikeAddress, type Chain, type MaxId, type Namespace, type Wallet } from "@max/sdk";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";

import { errorMessage } from "@/error/messages";
import { useChains, useRemoveChainDestination, useSetChainDestination, useSetDestination } from "@/hooks/queries";
import { toast } from "@/store/flows";
import { namespaceLabel, shortAddress } from "@/utils/format";

import { Button } from "../ui/Button";
import { Card, KindBadge } from "../ui/Blocks";
import { ChainLogo } from "../ui/ChainLogo";
import { CopyButton } from "../ui/Copy";
import { Dialog } from "../ui/Dialog";
import { Icon } from "../ui/Icon";

import { EditProfileDialog } from "./EditProfileDialog";
import { Handle } from "./Handle";

const NAMESPACES: Namespace[] = ["evm", "solana", "bitcoin", "litecoin", "tron"];
const LINKABLE: Namespace[] = ["evm", "solana"];

export function IdCard({ id, wallets }: { id: MaxId; wallets: Wallet[] }) {
  const [editing, setEditing] = useState(false);
  return (
    <Card as="article" className="flex flex-col gap-5 p-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-1.5">
          <div className="flex flex-wrap items-center gap-2.5">
            <h2 className="m-0 text-2xl"><Handle name={id.name} /></h2>
            <KindBadge type={id.type} />
            {id.isDefault ? <span className="rounded-full bg-clay-soft px-2.5 py-[3px] text-xs font-semibold text-clay-text">Default ID</span> : null}
            {id.status !== "active" ? <span className="rounded-full bg-danger-soft px-2.5 py-[3px] text-xs font-semibold text-[#9C2F2F]">Paused</span> : null}
          </div>
          {id.displayName && id.displayName !== id.name ? <span className="text-sm text-muted">{id.displayName}</span> : null}
        </div>
        <Button variant="secondary" size="sm" className="!text-sm !font-medium" onClick={() => setEditing(true)}>Edit profile</Button>
      </div>

      <ul className="m-0 flex list-none flex-col border-t border-line p-0">
        {NAMESPACES.map((ns) => (
          <DestinationRow key={ns} id={id} namespace={ns} wallets={wallets.filter((w) => w.namespace === ns)} />
        ))}
      </ul>
      <div className="flex flex-wrap gap-x-5 gap-y-2">
        <Link href={`/${id.handle}`} className="text-sm font-medium">View public profile</Link>
        {id.isDefault ? null : <Link href={`/rename/${id.id}`} className="text-sm font-medium">Rename</Link>}
      </div>
      {editing ? <EditProfileDialog id={id} onClose={() => setEditing(false)} /> : null}
    </Card>
  );
}

function DestinationRow({ id, namespace, wallets }: { id: MaxId; namespace: Namespace; wallets: Wallet[] }) {
  const current = id.destinations.find((d) => d.namespace === namespace && !d.chain);
  const overrides = id.destinations.filter((d) => d.namespace === namespace && d.chain).length;
  const [customizing, setCustomizing] = useState(false);
  const [open, setOpen] = useState(false);
  const set = useSetDestination();
  const ref = useRef<HTMLLIElement>(null);
  const label = namespaceLabel(namespace);
  const linkable = LINKABLE.includes(namespace);
  const [pasting, setPasting] = useState(false);
  const close = () => {
    setOpen(false);
    setPasting(false);
  };

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => ref.current && !ref.current.contains(e.target as Node) && close();
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && close();
    document.addEventListener("mousedown", onDoc);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <li ref={ref} className="flex flex-col border-b border-line">
      <div className="flex flex-wrap items-center gap-3 py-3.5">
        <span className="flex w-[96px] items-center gap-2 text-[13px] font-semibold text-muted"><ChainLogo chain={namespace} />{label}</span>
        {current ? (
          <>
            <span className="flex flex-1 flex-wrap items-center gap-2">
              <span className="font-mono text-sm" title={current.address}>{shortAddress(current.address)}</span>
              {current.source === "pasted" ? (
                <span title="Pasted, not proven by a wallet signature." className="rounded-full border border-line-strong bg-sunken px-2 py-[2px] text-xs font-semibold text-body">Unverified</span>
              ) : null}
            </span>
            <CopyButton value={current.address} label={`Copy ${label} address`} />
          </>
        ) : (
          <span className="flex-1 text-sm text-muted">Not set</span>
        )}
        <button type="button" aria-expanded={open} aria-haspopup="true" onClick={() => (open ? close() : setOpen(true))}
          className="inline-flex min-h-11 items-center gap-1.5 rounded-[10px] border border-line bg-surface px-3 text-sm text-ink hover:bg-sunken">
          {current ? "Change" : "Set"}<Icon name="chevronDown" size={14} />
        </button>
      </div>
      {open && pasting ? (
        <PastePanel id={id} namespace={namespace} onDone={close} onBack={() => setPasting(false)} />
      ) : open ? (
        <div role="group" aria-label={`Choose ${label} destination`} className="mb-3 flex flex-col gap-0.5 rounded-xl border border-line bg-surface p-2 shadow-[0_8px_24px_rgba(20,20,19,0.08)]">
          {linkable ? <>
          <span className="px-2.5 py-2 text-xs font-semibold uppercase tracking-[0.06em] text-muted">Your {label} wallets</span>
          {wallets.length === 0 ? <span className="px-2.5 py-2 text-sm text-muted">No {label} wallet linked yet.</span> : null}
          {wallets.map((w) => {
            const isCurrent = current?.walletId === w.id;
            return (
              <button key={w.id} type="button" disabled={isCurrent || set.isPending}
                onClick={() =>
                  set.mutate({ id: id.id, namespace, target: { walletId: w.id } }, {
                    onSuccess: () => {
                      toast(`${label} destination updated`);
                      close();
                    },
                    onError: (e) => toast(errorMessage(e), "danger"),
                  })
                }
                className="flex min-h-11 items-center justify-between gap-2 rounded-lg px-2.5 text-left hover:bg-clay-soft disabled:cursor-default disabled:hover:bg-transparent">
                <span className="font-mono text-sm text-ink" title={w.address}>{shortAddress(w.address)}</span>
                <span className="text-[13px] font-semibold text-clay-text">{isCurrent ? "Current" : "Set as destination"}</span>
              </button>
            );
          })}
          <Link href="/connect?mode=link&next=/dashboard" className="p-2.5 text-sm font-medium">Link another wallet</Link>
          </> : <span className="px-2.5 py-2 text-sm text-muted">MAX can’t link {label} wallets. Paste a receive address instead.</span>}
          <button type="button" onClick={() => setPasting(true)}
            className="flex min-h-11 items-center rounded-lg px-2.5 text-left text-sm font-medium text-clay-text hover:bg-clay-soft">
            Paste an address
          </button>
        </div>
      ) : null}
      {namespace === "evm" ? (
        <button type="button" onClick={() => setCustomizing(true)}
          className="mb-3 -mt-1.5 self-start pl-[108px] text-[13px] font-medium text-clay-text hover:underline">
          {overrides ? `${overrides} ${overrides === 1 ? "chain" : "chains"} customized` : "Customize per chain"}
        </button>
      ) : null}
      {customizing ? <ChainOverridesDialog id={id} wallets={wallets} onClose={() => setCustomizing(false)} /> : null}
    </li>
  );
}

/** Per-chain overrides for EVM: each chain uses the EVM default unless set here. */
function ChainOverridesDialog({ id, wallets, onClose }: { id: MaxId; wallets: Wallet[]; onClose(): void }) {
  const chains = useChains();
  const evmChains = (chains.data ?? []).filter((c) => c.namespace === "evm");
  const fallback = id.destinations.find((d) => d.namespace === "evm" && !d.chain);
  const [editing, setEditing] = useState<string | null>(null);
  return (
    <Dialog title="Customize EVM chains" labelId={`chains-${id.id}`} onClose={onClose} width={560}>
      <p className="m-0 text-sm text-muted">
        Chains you don’t customize use your EVM default{fallback ? ` (${shortAddress(fallback.address)})` : ", which isn’t set yet"}.
      </p>
      <ul className="m-0 flex list-none flex-col p-0">
        {evmChains.map((c) => (
          <ChainOverrideRow key={c.key} id={id} chain={c} wallets={wallets} open={editing === c.key}
            onToggle={() => setEditing(editing === c.key ? null : c.key)} />
        ))}
      </ul>
    </Dialog>
  );
}

function ChainOverrideRow({ id, chain, wallets, open, onToggle }: { id: MaxId; chain: Chain; wallets: Wallet[]; open: boolean; onToggle(): void }) {
  const override = id.destinations.find((d) => d.chain === chain.key);
  const set = useSetChainDestination();
  const remove = useRemoveChainDestination();
  const [pasting, setPasting] = useState(false);
  const done = (msg: string) => () => {
    toast(msg);
    setPasting(false);
    onToggle();
  };
  const fail = (e: unknown) => toast(errorMessage(e), "danger");
  return (
    <li className="flex flex-col border-b border-line last:border-b-0">
      <div className="flex flex-wrap items-center gap-3 py-3">
        <span className="flex min-w-[150px] flex-1 items-center gap-2.5 text-sm font-semibold">
          <ChainLogo chain={chain.key} size={20} />{chain.name}
          {chain.testnet ? <span className="rounded-full bg-sunken px-1.5 text-[11px] font-medium text-muted">Testnet</span> : null}
        </span>
        {override ? (
          <span className="flex items-center gap-2">
            <span className="font-mono text-sm" title={override.address}>{shortAddress(override.address)}</span>
            {override.source === "pasted" ? <span title="Pasted, not proven by a wallet signature." className="rounded-full border border-line-strong bg-sunken px-2 py-[2px] text-xs font-semibold text-body">Unverified</span> : null}
          </span>
        ) : <span className="text-sm text-muted">Uses default</span>}
        <button type="button" aria-expanded={open} onClick={onToggle}
          className="inline-flex min-h-10 items-center gap-1.5 rounded-[10px] border border-line bg-surface px-3 text-sm text-ink hover:bg-sunken">
          Change<Icon name="chevronDown" size={14} />
        </button>
      </div>
      {open && pasting ? (
        <PastePanel id={id} namespace="evm" chain={chain} onDone={() => { setPasting(false); onToggle(); }} onBack={() => setPasting(false)} />
      ) : open ? (
        <div role="group" aria-label={`Choose ${chain.name} destination`} className="mb-3 flex flex-col gap-0.5 rounded-xl border border-line bg-surface p-2">
          {override ? (
            <button type="button" disabled={remove.isPending}
              onClick={() => remove.mutate({ id: id.id, chain: chain.key }, { onSuccess: done(`${chain.name} uses your EVM default`), onError: fail })}
              className="flex min-h-11 items-center rounded-lg px-2.5 text-left text-sm font-medium text-ink hover:bg-clay-soft">
              Use EVM default
            </button>
          ) : null}
          {wallets.map((w) => {
            const isCurrent = override?.walletId === w.id;
            return (
              <button key={w.id} type="button" disabled={isCurrent || set.isPending}
                onClick={() => set.mutate({ id: id.id, chain: chain.key, target: { walletId: w.id } }, { onSuccess: done(`${chain.name} destination updated`), onError: fail })}
                className="flex min-h-11 items-center justify-between gap-2 rounded-lg px-2.5 text-left hover:bg-clay-soft disabled:cursor-default disabled:hover:bg-transparent">
                <span className="font-mono text-sm text-ink" title={w.address}>{shortAddress(w.address)}</span>
                <span className="text-[13px] font-semibold text-clay-text">{isCurrent ? "Current" : "Use this wallet"}</span>
              </button>
            );
          })}
          <button type="button" onClick={() => setPasting(true)}
            className="flex min-h-11 items-center rounded-lg px-2.5 text-left text-sm font-medium text-clay-text hover:bg-clay-soft">
            Paste an address
          </button>
        </div>
      ) : null}
    </li>
  );
}

const EVM_WARNING = "This address will receive on every EVM chain you haven’t customized. Exchange deposit addresses often accept only one chain.";

/** Paste an address, review it in full, and confirm control before saving. */
function PastePanel({ id, namespace, chain, onDone, onBack }: { id: MaxId; namespace: Namespace; chain?: Chain; onDone(): void; onBack(): void }) {
  const setChain = useSetChainDestination();
  const [address, setAddress] = useState("");
  const [reviewing, setReviewing] = useState(false);
  const [confirmed, setConfirmed] = useState(false);
  const set = useSetDestination();
  const label = chain?.name ?? namespaceLabel(namespace);
  const trimmed = address.trim();
  const shapeOk = looksLikeAddress(namespace, trimmed);
  const fieldId = `paste-${id.id}-${chain?.key ?? namespace}`;
  const pending = set.isPending || setChain.isPending;

  const save = () => {
    const target = { address: trimmed, confirmed: true as const };
    const opts = {
      onSuccess: () => {
        toast(`${label} destination updated`);
        onDone();
      },
      onError: (e: unknown) => toast(errorMessage(e), "danger"),
    };
    if (chain) setChain.mutate({ id: id.id, chain: chain.key, target }, opts);
    else set.mutate({ id: id.id, namespace, target }, opts);
  };

  return (
    <div role="group" aria-label={`Paste a ${label} address`} className="mb-3 flex flex-col gap-3 rounded-xl border border-line bg-surface p-4 shadow-[0_8px_24px_rgba(20,20,19,0.08)]">
      {!reviewing ? (
        <>
          <label htmlFor={fieldId} className="text-sm font-semibold">{label} receive address</label>
          <input id={fieldId} value={address} onChange={(e) => setAddress(e.target.value)} autoComplete="off" spellCheck={false}
            aria-invalid={trimmed !== "" && !shapeOk} aria-describedby={`${fieldId}-msg`}
            className="min-h-11 rounded-lg border border-line bg-surface px-3 font-mono text-sm text-ink outline-none focus:border-ink" />
          <p id={`${fieldId}-msg`} className={trimmed !== "" && !shapeOk ? "m-0 text-sm text-danger" : "m-0 text-[13px] text-muted"}>
            {trimmed !== "" && !shapeOk ? `That doesn’t look like a ${label} address.` : "Paste the address exactly as your wallet or exchange shows it."}
          </p>
          <div className="flex gap-2">
            <Button size="sm" disabled={!shapeOk} onClick={() => setReviewing(true)}>Review</Button>
            <Button size="sm" variant="secondary" onClick={onBack}>Back</Button>
          </div>
        </>
      ) : (
        <>
          <span className="text-sm font-semibold">Check this {label} address</span>
          <span className="break-all rounded-lg bg-sunken p-3 font-mono text-sm">{trimmed}</span>
          {namespace === "evm" && !chain ? <p className="m-0 text-[13px] text-muted">{EVM_WARNING}</p> : null}
          <label className="flex items-start gap-2.5 text-sm">
            <input type="checkbox" checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} className="mt-0.5 size-4" />
            I control this address. Payments sent to it can’t be reversed.
          </label>
          <div className="flex gap-2">
            <Button size="sm" disabled={!confirmed} loading={pending} onClick={save}>Save</Button>
            <Button size="sm" variant="secondary" onClick={() => setReviewing(false)}>Edit</Button>
          </div>
        </>
      )}
    </div>
  );
}

export function IdCardSkeleton() {
  return (
    <div aria-busy="true" aria-label="Loading MAX ID" className="flex flex-col gap-4 rounded-2xl border border-line bg-surface p-6">
      <div className="h-6 w-[55%] rounded-md bg-sunken" />
      <div className="h-3.5 w-[30%] rounded-md bg-sunken" />
      <div className="h-px bg-line" />
      <div className="h-3.5 w-[80%] rounded-md bg-sunken" />
      <div className="h-3.5 w-[70%] rounded-md bg-sunken" />
    </div>
  );
}
