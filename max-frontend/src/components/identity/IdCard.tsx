"use client";

import { looksLikeAddress, type MaxId, type Namespace, type Wallet } from "@max/sdk";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";

import { errorMessage } from "@/error/messages";
import { useSetDestination } from "@/hooks/queries";
import { toast } from "@/store/flows";
import { namespaceLabel, shortAddress } from "@/utils/format";

import { Button } from "../ui/Button";
import { Card, KindBadge } from "../ui/Blocks";
import { CopyButton } from "../ui/Copy";
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
  const current = id.destinations.find((d) => d.namespace === namespace);
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
        <span className="w-[72px] text-[13px] font-semibold text-muted">{label}</span>
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
    </li>
  );
}

const EVM_WARNING = "This address will receive on every EVM chain MAX supports (Ethereum, Base, Arbitrum, OP, Polygon). Exchange deposit addresses often accept only one chain.";

/** Paste an address, review it in full, and confirm control before saving. */
function PastePanel({ id, namespace, onDone, onBack }: { id: MaxId; namespace: Namespace; onDone(): void; onBack(): void }) {
  const [address, setAddress] = useState("");
  const [reviewing, setReviewing] = useState(false);
  const [confirmed, setConfirmed] = useState(false);
  const set = useSetDestination();
  const label = namespaceLabel(namespace);
  const trimmed = address.trim();
  const shapeOk = looksLikeAddress(namespace, trimmed);
  const fieldId = `paste-${id.id}-${namespace}`;

  const save = () =>
    set.mutate({ id: id.id, namespace, target: { address: trimmed, confirmed: true } }, {
      onSuccess: () => {
        toast(`${label} destination updated`);
        onDone();
      },
      onError: (e) => toast(errorMessage(e), "danger"),
    });

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
          {namespace === "evm" ? <p className="m-0 text-[13px] text-muted">{EVM_WARNING}</p> : null}
          <label className="flex items-start gap-2.5 text-sm">
            <input type="checkbox" checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} className="mt-0.5 size-4" />
            I control this address. Payments sent to it can’t be reversed.
          </label>
          <div className="flex gap-2">
            <Button size="sm" disabled={!confirmed} loading={set.isPending} onClick={save}>Save</Button>
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
