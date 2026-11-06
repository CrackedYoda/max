"use client";

import type { AdminListing, AdminMarketFilter, AdminOffer, AdminSale, MarketRestriction, RestrictionKind } from "@max/sdk";
import { useQueryClient } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";

import { max } from "@/api/client";
import { keys } from "@/api/keys";
import { marketErrorMessage } from "@/error/market";
import { useAccountId, useAdminMarket } from "@/hooks/queries";
import { useDebounced } from "@/hooks/useDebounced";
import { toast } from "@/store/flows";
import { formatDate, relativeTime, shortAddress } from "@/utils/format";
import { displayDecimal } from "@/utils/money";

import { Handle } from "../identity/Handle";
import { Button } from "../ui/Button";
import { Banner, Card, Pill, Skeleton } from "../ui/Blocks";
import { CopyButton } from "../ui/Copy";
import { cx } from "../ui/cx";

import { ListingStatusPill, OfferStatusPill, SaleStatusPill } from "./shared";

type Tab = "review" | "sales" | "listings" | "offers" | "restrictions";
const TABS: { value: Tab; label: string }[] = [
  { value: "review", label: "Review queue" },
  { value: "sales", label: "Sales" },
  { value: "listings", label: "Listings" },
  { value: "offers", label: "Offers" },
  { value: "restrictions", label: "Restrictions" },
];

const SALE_STATUSES = ["pending_review", "locked", "submitted", "confirming", "completed", "expired", "cancelled", "manual_review", "reversed"];
const LISTING_STATUSES = ["active", "locked", "sold", "cancelled", "admin_delisted"];
const OFFER_STATUSES = ["live", "unfunded", "accepted", "declined", "superseded", "expired", "withdrawn", "lapsed", "completed"];
const btn = "!min-h-10 !px-3 !text-sm";
const input = "min-h-11 w-full rounded-xl border border-line bg-surface px-3.5 text-[15px] outline-none focus:border-ink";

/** /dashboard/admin/market: review queue, sales, listings, offers, restrictions. */
export function AdminMarket() {
  const [tab, setTab] = useState<Tab>("review");
  return (
    <div className="flex flex-col gap-6">
      <div role="tablist" aria-label="Market admin" className="flex flex-wrap gap-2">
        {TABS.map((t) => (
          <button key={t.value} type="button" role="tab" aria-selected={tab === t.value} aria-controls={`panel-${t.value}`} id={`tab-${t.value}`}
            onClick={() => setTab(t.value)}
            className={cx("min-h-11 rounded-[10px] px-3.5 text-sm text-ink",
              tab === t.value ? "border-2 border-accent bg-clay-soft font-semibold" : "border border-line bg-surface font-medium hover:bg-sunken")}>
            {t.label}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`}>
        {tab === "review" ? <SalesTab fixedStatus="pending_review" />
          : tab === "sales" ? <SalesTab />
            : tab === "listings" ? <ListingsTab />
              : tab === "offers" ? <OffersTab />
                : <RestrictionsTab />}
      </div>
    </div>
  );
}

// ---- shared bits ----

function useRefresh() {
  const qc = useQueryClient();
  const accountId = useAccountId();
  return () => {
    qc.invalidateQueries({ queryKey: [...keys.account(accountId), "admin", "market"] });
    qc.invalidateQueries({ queryKey: keys.market });
  };
}

function Filters({ statuses, status, setStatus, name, setName, account, setAccount }: {
  statuses?: string[]; status?: string; setStatus?(v: string): void;
  name: string; setName(v: string): void; account: string; setAccount(v: string): void;
}) {
  return (
    <div className="grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(min(100%,200px),1fr))]">
      {statuses && setStatus ? (
        <label className="flex flex-col gap-1.5 text-[13px] font-semibold">Status
          <select value={status} onChange={(e) => setStatus(e.target.value)} className={input}>
            <option value="">All</option>
            {statuses.map((s) => <option key={s} value={s}>{s.replace(/_/g, " ")}</option>)}
          </select>
        </label>
      ) : null}
      <label className="flex flex-col gap-1.5 text-[13px] font-semibold">Name
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="yoda" autoComplete="off" spellCheck={false} className={cx(input, "font-mono")} />
      </label>
      <label className="flex flex-col gap-1.5 text-[13px] font-semibold">Account ID
        <input value={account} onChange={(e) => setAccount(e.target.value.trim())} placeholder="uuid" autoComplete="off" spellCheck={false} className={cx(input, "font-mono")} />
      </label>
    </div>
  );
}

function useFilter(fixedStatus?: string) {
  const [status, setStatus] = useState(fixedStatus ?? "");
  const [name, setName] = useState("");
  const [account, setAccount] = useState("");
  const filter: AdminMarketFilter = {
    status: (fixedStatus ?? status) || undefined,
    name: useDebounced(name.trim().replace(/^max\./i, "")) || undefined,
    accountId: useDebounced(account) || undefined,
    limit: 100,
  };
  return { filter, ui: { status, setStatus, name, setName, account, setAccount } };
}

function List<T>({ q, empty, render }: { q: { isLoading: boolean; error: unknown; data?: T[] }; empty: string; render(x: T): ReactNode }) {
  if (q.isLoading) return <Skeleton className="h-32 w-full rounded-2xl" />;
  if (q.error) return <Banner tone="danger">{marketErrorMessage(q.error)}</Banner>;
  if (!q.data?.length) return <p className="m-0 py-4 text-sm text-muted">{empty}</p>;
  return <ul className="m-0 flex list-none flex-col gap-3 p-0">{q.data.map(render)}</ul>;
}

function Item({ children }: { children: ReactNode }) {
  return <li><Card className="flex flex-col gap-3 p-4">{children}</Card></li>;
}

function Account({ label, id }: { label: string; id: string }) {
  return (
    <span className="inline-flex items-center gap-1 text-[13px] text-muted">
      {label} <span className="font-mono text-ink" title={id}>{shortAddress(id, 8, 4)}</span>
      <CopyButton value={id} label={`Copy ${label.toLowerCase()} account ID`} className="!size-8" />
    </span>
  );
}

/**
 * An admin action. Clicking it opens a reason field; `required` blocks
 * confirming until a reason is typed (max-core refuses an empty one).
 */
function ReasonAction({ label, required, danger, run }: { label: string; required: boolean; danger?: boolean; run(reason: string): Promise<unknown> }) {
  const refresh = useRefresh();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  if (!open) return <Button size="sm" variant={danger ? "danger" : "secondary"} className={btn} onClick={() => setOpen(true)}>{label}</Button>;
  const missing = required && !reason.trim();
  return (
    <form className="flex w-full flex-col gap-2 rounded-xl bg-ivory p-3"
      onSubmit={async (e) => {
        e.preventDefault();
        if (missing) return;
        setPending(true);
        setError(null);
        try {
          await run(reason.trim());
          toast(`${label}: done`);
          setOpen(false);
          setReason("");
          refresh();
        } catch (err) {
          setError(marketErrorMessage(err));
        } finally {
          setPending(false);
        }
      }}>
      <label className="flex flex-col gap-1.5 text-[13px] font-semibold">
        <span>{label}: reason {required ? <span className="font-normal text-muted">(required, kept in the audit log)</span> : <span className="font-normal text-muted">(optional)</span>}</span>
        <input value={reason} onChange={(e) => setReason(e.target.value)} maxLength={500} autoFocus required={required} autoComplete="off" className={input} />
      </label>
      {error ? <p role="alert" className="m-0 text-sm text-danger">{error}</p> : null}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" size="sm" variant={danger ? "danger" : "primary"} className={btn} disabled={missing} loading={pending}>Confirm {label.toLowerCase()}</Button>
        <Button size="sm" variant="ghost" className={btn} onClick={() => { setOpen(false); setError(null); }}>Cancel</Button>
      </div>
    </form>
  );
}

// ---- tabs ----

function SalesTab({ fixedStatus }: { fixedStatus?: string }) {
  const { filter, ui } = useFilter(fixedStatus);
  const q = useAdminMarket<AdminSale[]>("sales", filter, () => max().admin.marketSales(filter));
  return (
    <div className="flex flex-col gap-4">
      {fixedStatus ? <p className="m-0 text-sm text-muted">Sales above the review threshold wait here. Approving starts the buyer’s payment window now.</p> : null}
      <Filters statuses={fixedStatus ? undefined : SALE_STATUSES} {...ui} />
      <List q={q} empty={fixedStatus ? "Nothing waiting for review." : "No sales match."} render={(s) => (
        <Item key={s.id}>
          <div className="flex flex-wrap items-center gap-2">
            {s.handle ? <Handle name={s.handle} className="text-[17px]" /> : <span className="font-mono text-sm">{s.maxId}</span>}
            <SaleStatusPill status={s.status} />
            <span className="ml-auto font-semibold">{displayDecimal(s.price)} {s.symbol}</span>
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1">
            <Account label="Seller" id={s.sellerAccountId} />
            <Account label="Buyer" id={s.buyerAccountId} />
            <span className="text-[13px] text-muted">Created {relativeTime(s.createdAt)}{s.lockExpiresAt ? ` · lock until ${new Date(s.lockExpiresAt).toLocaleString()}` : ""}</span>
          </div>
          <p className="m-0 text-[13px] text-muted">
            Fee {displayDecimal(s.fee)} · seller gets {displayDecimal(s.sellerReceives)} · {s.offerId ? "offer" : "buy now"} · {s.chain}
            {s.txHash ? <> · tx <span className="font-mono" title={s.txHash}>{shortAddress(s.txHash, 6, 6)}</span></> : null}
            {s.statusReason ? <> · {s.statusReason}</> : null}
          </p>
          <div className="flex flex-wrap gap-2">
            {s.status === "pending_review" ? (
              <>
                <ReasonAction label="Approve" required={false} run={(r) => max().admin.marketApproveSale(s.id, r || undefined)} />
                <ReasonAction label="Deny" required danger run={(r) => max().admin.marketDenySale(s.id, r)} />
              </>
            ) : null}
            {s.status === "manual_review" || (s.status === "locked" && !s.txHash) ? (
              <ReasonAction label="Cancel" required danger run={(r) => max().admin.marketCancelSale(s.id, r)} />
            ) : null}
            {s.status === "completed" ? (
              <ReasonAction label="Reverse" required danger run={(r) => max().admin.marketReverseSale(s.id, r)} />
            ) : null}
          </div>
          {s.status === "completed" ? <p className="m-0 text-[12px] text-muted">Reversing moves the name back to the seller. Money is not moved: refunds are handled manually.</p> : null}
        </Item>
      )} />
    </div>
  );
}

function ListingsTab() {
  const { filter, ui } = useFilter();
  const q = useAdminMarket<AdminListing[]>("listings", filter, () => max().admin.marketListings(filter));
  return (
    <div className="flex flex-col gap-4">
      <Filters statuses={LISTING_STATUSES} {...ui} />
      <List q={q} empty="No listings match." render={(l) => (
        <Item key={l.id}>
          <div className="flex flex-wrap items-center gap-2">
            {l.handle ? <Handle name={l.handle} className="text-[17px]" /> : <span className="font-mono text-sm">{l.maxId}</span>}
            <ListingStatusPill status={l.status} />
            <span className="ml-auto font-semibold">{displayDecimal(l.price)} {l.symbol}</span>
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1">
            <Account label="Seller" id={l.sellerAccountId} />
            <span className="text-[13px] text-muted">Listed {formatDate(l.createdAt)} · payout <span className="font-mono" title={l.payoutAddress}>{shortAddress(l.payoutAddress)}</span>{l.statusReason ? ` · ${l.statusReason}` : ""}</span>
          </div>
          {l.status === "active" ? <div className="flex flex-wrap gap-2"><ReasonAction label="Delist" required danger run={(r) => max().admin.marketDelist(l.id, r)} /></div> : null}
        </Item>
      )} />
    </div>
  );
}

function OffersTab() {
  const { filter, ui } = useFilter();
  const q = useAdminMarket<AdminOffer[]>("offers", filter, () => max().admin.marketOffers(filter));
  return (
    <div className="flex flex-col gap-4">
      <Filters statuses={OFFER_STATUSES} {...ui} />
      <List q={q} empty="No offers match." render={(o) => (
        <Item key={o.id}>
          <div className="flex flex-wrap items-center gap-2">
            {o.handle ? <Handle name={o.handle} className="text-[17px]" /> : <span className="font-mono text-sm">{o.maxId}</span>}
            <OfferStatusPill status={o.status} />
            <span className="ml-auto font-semibold">{displayDecimal(o.amount)} {o.symbol}</span>
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1">
            <Account label="Buyer" id={o.buyerAccountId} />
            <span className="text-[13px] text-muted">From <span className="font-mono" title={o.fromAddress}>{shortAddress(o.fromAddress)}</span> · made {formatDate(o.createdAt)} · expires {formatDate(o.expiresAt)}</span>
          </div>
        </Item>
      )} />
    </div>
  );
}

function RestrictionsTab() {
  const [all, setAll] = useState(false);
  const [account, setAccount] = useState("");
  const accountId = useDebounced(account) || undefined;
  const params = { accountId, all, limit: 100 };
  const q = useAdminMarket<MarketRestriction[]>("restrictions", params, () => max().admin.marketRestrictions(params));
  return (
    <div className="flex flex-col gap-6">
      <NewRestriction />
      <div className="flex flex-wrap items-end gap-4">
        <label className="flex min-w-[min(100%,280px)] flex-1 flex-col gap-1.5 text-[13px] font-semibold">Account ID
          <input value={account} onChange={(e) => setAccount(e.target.value.trim())} placeholder="uuid" autoComplete="off" spellCheck={false} className={cx(input, "font-mono")} />
        </label>
        <label className="flex min-h-11 items-center gap-2 text-sm font-medium">
          <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} className="size-4" />Include lifted and expired
        </label>
      </div>
      <List q={q} empty="No restrictions." render={(r) => (
        <Item key={r.id}>
          <div className="flex flex-wrap items-center gap-2">
            <Pill tone={r.kind === "ban" ? "danger" : "clay"} className="!px-2.5 !py-1 !text-xs">{r.kind === "ban" ? "Market ban" : "Offer suspension"}</Pill>
            {r.active ? <Pill className="!px-2.5 !py-1 !text-xs">Active</Pill> : <Pill className="!px-2.5 !py-1 !text-xs">{r.liftedAt ? "Lifted" : "Expired"}</Pill>}
            <Account label="Account" id={r.accountId} />
          </div>
          <p className="m-0 text-[13px] text-muted">
            {r.until ? `Until ${formatDate(r.until)}` : "No end date"} · since {formatDate(r.createdAt)}{r.createdBy ? "" : " · automatic"}
          </p>
          <p className="m-0 text-sm">{r.reason}</p>
          {r.active ? <div className="flex flex-wrap gap-2"><ReasonAction label="Lift" required={false} run={(reason) => max().admin.marketLiftRestriction(r.id, reason || undefined)} /></div> : null}
        </Item>
      )} />
    </div>
  );
}

function NewRestriction() {
  const refresh = useRefresh();
  const [accountId, setAccountId] = useState("");
  const [kind, setKind] = useState<RestrictionKind>("ban");
  const [until, setUntil] = useState("");
  const [reason, setReason] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const ok = /^[0-9a-f-]{36}$/i.test(accountId) && reason.trim().length > 0;
  return (
    <Card as="form" className="flex flex-col gap-4 p-5">
      <h3 className="m-0 text-[15px] font-semibold">Restrict an account</h3>
      <div className="grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(min(100%,200px),1fr))]">
        <label className="flex flex-col gap-1.5 text-[13px] font-semibold">Account ID
          <input value={accountId} onChange={(e) => setAccountId(e.target.value.trim())} placeholder="uuid" autoComplete="off" spellCheck={false} className={cx(input, "font-mono")} />
        </label>
        <label className="flex flex-col gap-1.5 text-[13px] font-semibold">Kind
          <select value={kind} onChange={(e) => setKind(e.target.value as RestrictionKind)} className={input}>
            <option value="ban">Market ban</option>
            <option value="offer_suspension">Offer suspension</option>
          </select>
        </label>
        <label className="flex flex-col gap-1.5 text-[13px] font-semibold"><span>Until <span className="font-normal text-muted">(optional)</span></span>
          <input type="date" value={until} onChange={(e) => setUntil(e.target.value)} className={input} />
        </label>
      </div>
      <label className="flex flex-col gap-1.5 text-[13px] font-semibold"><span>Reason <span className="font-normal text-muted">(required, kept in the audit log)</span></span>
        <input value={reason} onChange={(e) => setReason(e.target.value)} maxLength={500} autoComplete="off" className={input} />
      </label>
      {error ? <p role="alert" className="m-0 text-sm text-danger">{error}</p> : null}
      <Button size="sm" variant="danger" className={cx(btn, "self-start")} disabled={!ok} loading={pending}
        onClick={async () => {
          setPending(true);
          setError(null);
          try {
            await max().admin.marketRestrict({ accountId, kind, reason: reason.trim(), until: until ? new Date(`${until}T23:59:59`).toISOString() : undefined });
            toast("Restriction added");
            setAccountId(""); setReason(""); setUntil("");
            refresh();
          } catch (err) {
            setError(marketErrorMessage(err));
          } finally {
            setPending(false);
          }
        }}>
        Restrict
      </Button>
    </Card>
  );
}
