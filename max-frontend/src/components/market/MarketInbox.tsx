"use client";

import { isMaxApiError, isPendingApproval, type Listing, type MaxId, type Offer, type ReceivedOffer, type Sale, type Wallet } from "@max/sdk";
import Link from "next/link";
import { useState, type ReactNode } from "react";

import { max } from "@/api/client";
import { marketErrorMessage } from "@/error/market";
import { useInvalidateMarket, useMarketInbox } from "@/hooks/queries";
import { toast } from "@/store/flows";
import { formatDate, relativeTime, timeUntil } from "@/utils/format";
import { displayDecimal } from "@/utils/money";

import { Handle } from "../identity/Handle";
import { Button } from "../ui/Button";
import { Banner, Card, SectionHeader, Skeleton } from "../ui/Blocks";
import { Dialog } from "../ui/Dialog";

import { BuyModal, type PayTarget } from "./BuyModal";
import { SellForm } from "./SellForm";
import { ListingStatusPill, OfferStatusPill, SaleStatusPill, walletsFor, WalletSelect } from "./shared";

const OPEN_LISTING = ["active", "locked"];
const actionBtn = "!min-h-10 !px-3 !text-sm";

/**
 * Dashboard → Market: your listings (sell, edit price, delist), offers you
 * received (accept/decline), offers you made (withdraw, pay) and sales.
 * Renders nothing while the marketplace is disabled.
 */
export function MarketInbox({ ids, wallets, adminLink }: { ids: MaxId[]; wallets: Wallet[]; adminLink?: ReactNode }) {
  const inbox = useMarketInbox();
  const [pay, setPay] = useState<PayTarget | null>(null);

  if (isMaxApiError(inbox.error, "MARKET_DISABLED") || (isMaxApiError(inbox.error) && inbox.error.status === 404)) return null;

  const data = inbox.data;
  const handleOf = (maxId: string, fallback?: string) => fallback ?? ids.find((i) => i.id === maxId)?.handle ?? "";
  const mine = new Set(wallets.map((w) => w.address));

  return (
    <section aria-labelledby="market" className="flex flex-col gap-5">
      <SectionHeader id="market" title="Market" lead="Sell your MAX IDs and track offers. Sales settle on Solana; MAX’s fee comes out of the seller’s proceeds."
        action={<div className="flex flex-wrap items-center gap-4">{adminLink}<Link href="/market" className="text-sm font-medium">Browse the market</Link></div>} />
      {inbox.error ? <Banner tone="danger">{marketErrorMessage(inbox.error)}</Banner> : null}
      {inbox.isLoading ? <Skeleton className="h-40 w-full rounded-2xl" /> : data ? (
        <div className="grid gap-5 [grid-template-columns:repeat(auto-fit,minmax(min(100%,440px),1fr))]">
          <Panel title="My listings">
            {ids.length ? ids.map((id) => (
              <ListingRow key={id.id} id={id} listing={data.listings.find((l) => l.maxId === id.id && OPEN_LISTING.includes(l.status))} />
            )) : <Empty>Claim a MAX ID first; then you can sell it here.</Empty>}
          </Panel>

          <Panel title="Offers received">
            {data.offersReceived.length ? data.offersReceived.map((o) => (
              <ReceivedRow key={o.id} offer={o} handle={handleOf(o.maxId, o.handle)}
                listed={data.listings.some((l) => l.maxId === o.maxId && l.status === "active")} wallets={wallets} />
            )) : <Empty>No open offers on your names.</Empty>}
          </Panel>

          <Panel title="Offers made">
            {data.offersMade.length ? data.offersMade.map((o) => (
              <MadeRow key={o.id} offer={o} onPay={() => setPay({
                kind: "offer", offerId: o.id, handle: o.handle ?? "", chain: o.chain, asset: o.asset, symbol: o.symbol,
                price: o.amount, priceBaseUnits: o.amountBaseUnits, fromAddress: o.fromAddress,
              })} />
            )) : <Empty>You haven’t made any offers. <Link href="/market">Browse the market</Link>.</Empty>}
          </Panel>

          <Panel title="Sales">
            {data.sales.length ? data.sales.map((s) => (
              <SaleRow key={s.id} sale={s} bought={mine.has(s.buyerAddress)}
                onPay={() => {
                  // Pin to what the buyer agreed to before the sale existed (their offer)
                  // when we have it; otherwise to the sale's displayed price, converted with
                  // trusted decimals and checked against the sale's own base units.
                  const o = s.offerId ? data.offersMade.find((x) => x.id === s.offerId) : undefined;
                  setPay({
                    kind: "sale", saleId: s.id, handle: s.handle ?? "", chain: o?.chain ?? s.chain, asset: o?.asset ?? s.asset, symbol: o?.symbol ?? s.symbol,
                    price: o?.amount ?? s.price, priceBaseUnits: o?.amountBaseUnits ?? s.priceBaseUnits,
                  });
                }} />
            )) : <Empty>No sales yet.</Empty>}
          </Panel>
        </div>
      ) : null}
      {pay ? <BuyModal target={pay} onClose={() => setPay(null)} /> : null}
    </section>
  );
}

function Panel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Card className="flex min-w-0 flex-col gap-1 p-5">
      <h3 className="m-0 mb-1 text-[15px] font-semibold">{title}</h3>
      <ul className="m-0 flex list-none flex-col p-0">{children}</ul>
    </Card>
  );
}

function Empty({ children }: { children: ReactNode }) {
  return <li className="py-3 text-sm text-muted">{children}</li>;
}

function Row({ children }: { children: ReactNode }) {
  return <li className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-sunken py-3 last:border-b-0">{children}</li>;
}

function ListingRow({ id, listing }: { id: MaxId; listing?: Listing }) {
  const [mode, setMode] = useState<"sell" | "edit" | null>(null);
  const [confirm, setConfirm] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const invalidate = useInvalidateMarket();

  async function delist() {
    setPending(true);
    setError(null);
    try {
      await max().market.delist(listing!.id);
      invalidate();
      toast(`${id.handle} delisted`);
      setConfirm(false);
    } catch (err) {
      setError(marketErrorMessage(err));
    } finally {
      setPending(false);
    }
  }

  return (
    <Row>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <Handle name={id.name} className="text-[15px]" />
        <span className="text-[13px] text-muted">
          {listing ? <>{displayDecimal(listing.price)} {listing.symbol} · listed {relativeTime(listing.createdAt)}</> : "Not listed"}
        </span>
      </div>
      {listing ? <ListingStatusPill status={listing.status} /> : null}
      <div className="flex flex-wrap gap-1">
        {!listing ? (
          <Button size="sm" variant="secondary" className={actionBtn} onClick={() => setMode("sell")}>Sell</Button>
        ) : listing.status === "active" ? (
          confirm ? (
            <>
              <Button size="sm" variant="danger" className={actionBtn} loading={pending} onClick={delist}>Confirm delist</Button>
              <Button size="sm" variant="ghost" className={actionBtn} onClick={() => setConfirm(false)}>Keep</Button>
            </>
          ) : (
            <>
              <Button size="sm" variant="secondary" className={actionBtn} onClick={() => setMode("edit")}>Edit price</Button>
              <Button size="sm" variant="danger" className={actionBtn} onClick={() => setConfirm(true)}>Delist</Button>
            </>
          )
        ) : null}
      </div>
      {error ? <p role="alert" className="m-0 w-full text-sm text-danger">{error}</p> : null}
      {mode ? (
        <Dialog title={mode === "edit" ? <>Edit price · <Handle name={id.name} /></> : <>Sell <Handle name={id.name} /></>} labelId={`sell-${id.id}`} onClose={() => setMode(null)}>
          <SellForm id={id} listing={mode === "edit" ? listing : undefined} onDone={() => setMode(null)} />
        </Dialog>
      ) : null}
    </Row>
  );
}

function ReceivedRow({ offer, handle, listed, wallets }: { offer: ReceivedOffer; handle: string; listed: boolean; wallets: Wallet[] }) {
  const [mode, setMode] = useState<"accept" | "decline" | null>(null);
  return (
    <Row>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="text-[15px] font-semibold">{displayDecimal(offer.amount)} {offer.symbol}</span>
        <span className="text-[13px] text-muted">for <Handle name={handle} className="text-[13px]" /> · expires {timeUntil(offer.expiresAt)}</span>
      </div>
      <div className="flex flex-wrap gap-1">
        <Button size="sm" className={actionBtn} onClick={() => setMode("accept")}>Accept</Button>
        <Button size="sm" variant="ghost" className={actionBtn} onClick={() => setMode("decline")}>Decline</Button>
      </div>
      {mode ? <OfferDecision offer={offer} handle={handle} mode={mode} listed={listed} wallets={wallets} onClose={() => setMode(null)} /> : null}
    </Row>
  );
}

function OfferDecision({ offer, handle, mode, listed, wallets, onClose }: {
  offer: ReceivedOffer; handle: string; mode: "accept" | "decline"; listed: boolean; wallets: Wallet[]; onClose(): void;
}) {
  const invalidate = useInvalidateMarket();
  const payouts = walletsFor(wallets, offer.chain);
  const [payout, setPayout] = useState("");
  const [note, setNote] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const payoutAddress = payout || payouts[0]?.address || "";
  const needPayout = mode === "accept" && !listed;

  async function go(close: () => void) {
    setPending(true);
    setError(null);
    try {
      if (mode === "accept") {
        const res = await max().market.acceptOffer(offer.id, needPayout ? { payoutAddress } : {});
        toast(isPendingApproval(res) ? "Held for approval" : "Offer accepted. The buyer has 24 hours to pay.");
      } else {
        await max().market.declineOffer(offer.id, note.trim() ? { note: note.trim() } : {});
        toast("Offer declined");
      }
      invalidate();
      close();
    } catch (err) {
      setError(marketErrorMessage(err));
    } finally {
      setPending(false);
    }
  }

  return (
    <Dialog title={mode === "accept" ? "Accept this offer?" : "Decline this offer?"} labelId={`decide-${offer.id}`} onClose={onClose}>
      <form className="flex flex-col gap-5" onSubmit={(e) => { e.preventDefault(); const form = e.currentTarget; go(() => form.closest("dialog")?.close()); }}>
        <p className="m-0 text-[15px]">
          <strong>{displayDecimal(offer.amount)} {offer.symbol}</strong> for <Handle name={handle} />.
        </p>
        {mode === "accept" ? (
          <>
            <p className="m-0 text-[13px] leading-relaxed text-muted">
              The name is reserved for the buyer for 24 hours while they pay. Other offers on it close, and your listing (if any) is paused. You receive the amount minus MAX’s fee.
            </p>
            {needPayout ? (
              payouts.length ? <WalletSelect id={`payout-${offer.id}`} label="Get paid to" wallets={payouts} value={payoutAddress} onChange={setPayout} />
                : <Banner tone="info">Link a Solana wallet to your account to get paid.</Banner>
            ) : null}
          </>
        ) : (
          <div className="flex flex-col gap-2.5">
            <label htmlFor={`note-${offer.id}`} className="text-sm font-semibold">Note to the buyer <span className="font-normal text-muted">(optional)</span></label>
            <input id={`note-${offer.id}`} value={note} maxLength={280} onChange={(e) => setNote(e.target.value)} autoComplete="off"
              className="min-h-12 rounded-xl border border-line bg-surface px-4 text-base outline-none focus:border-ink" />
          </div>
        )}
        {error ? <Banner tone="danger">{error}</Banner> : null}
        <Button type="submit" variant={mode === "accept" ? "primary" : "secondary"} loading={pending} disabled={needPayout && !payoutAddress}>
          {mode === "accept" ? "Accept offer" : "Decline offer"}
        </Button>
      </form>
    </Dialog>
  );
}

function MadeRow({ offer, onPay }: { offer: Offer; onPay(): void }) {
  const invalidate = useInvalidateMarket();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const open = offer.status === "live" || offer.status === "unfunded";

  async function withdraw() {
    setPending(true);
    setError(null);
    try {
      await max().market.withdrawOffer(offer.id);
      invalidate();
      toast("Offer withdrawn");
    } catch (err) {
      setError(marketErrorMessage(err));
    } finally {
      setPending(false);
    }
  }

  return (
    <Row>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="text-[15px] font-semibold">{displayDecimal(offer.amount)} {offer.symbol}</span>
        <span className="text-[13px] text-muted">
          for {offer.handle ? <Handle name={offer.handle} className="text-[13px]" /> : "a name"}{open ? <> · expires {timeUntil(offer.expiresAt)}</> : null}
        </span>
      </div>
      <OfferStatusPill status={offer.status} />
      <div className="flex flex-wrap gap-1">
        {offer.status === "accepted" ? <Button size="sm" className={actionBtn} onClick={onPay}>Pay</Button> : null}
        {open ? <Button size="sm" variant="ghost" className={actionBtn} loading={pending} onClick={withdraw}>Withdraw</Button> : null}
      </div>
      {offer.status === "unfunded" ? (
        <p className="m-0 w-full text-[13px] text-danger">
          The paying wallet no longer holds {displayDecimal(offer.amount)} {offer.symbol} plus fees, so the owner can’t see this offer. Top it up to make it visible again.
        </p>
      ) : null}
      {offer.status === "accepted" ? <p className="m-0 w-full text-[13px] text-clay-text">Accepted: pay within 24 hours of acceptance, or the offer lapses and counts against you.</p> : null}
      {offer.status === "declined" && offer.declineNote ? <p className="m-0 w-full text-[13px] text-muted">Note from the owner: {offer.declineNote}</p> : null}
      {error ? <p role="alert" className="m-0 w-full text-sm text-danger">{error}</p> : null}
    </Row>
  );
}

function SaleRow({ sale, bought, onPay }: { sale: Sale; bought: boolean; onPay(): void }) {
  const payable = bought && !sale.txHash && (sale.status === "locked" || sale.status === "pending_review");
  return (
    <Row>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="text-[15px]">{sale.handle ? <Handle name={sale.handle} /> : "MAX ID"}</span>
        <span className="text-[13px] text-muted">
          {bought ? "Bought" : "Sold"} for {displayDecimal(sale.price)} {sale.symbol}
          {!bought ? <> · you receive {displayDecimal(sale.sellerReceives)}</> : null} · {formatDate(sale.completedAt ?? sale.createdAt)}
        </span>
      </div>
      <SaleStatusPill status={sale.status} />
      {payable ? <Button size="sm" className={actionBtn} onClick={onPay}>{sale.status === "pending_review" ? "Check" : "Pay"}</Button> : null}
    </Row>
  );
}
