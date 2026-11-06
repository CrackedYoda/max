"use client";

import Link from "next/link";
import { useState } from "react";

import { getEnv } from "@/config/env";
import { useMarketName, useMyIds } from "@/hooks/queries";
import { useSession } from "@/store/session";
import { formatDate, relativeTime } from "@/utils/format";
import { displayDecimal } from "@/utils/money";

import { Button } from "../ui/Button";
import { Skeleton } from "../ui/Blocks";

import { BuyModal } from "./BuyModal";
import { OfferModal, type OfferAsset } from "./OfferModal";

/**
 * Profile → market panel. Listed: price, Buy now, Make offer. Not listed but
 * listable: Make offer. Renders nothing when the market is off or the name
 * isn't tradable and has no history.
 */
export function MarketPanel({ name }: { name: string }) {
  const view = useMarketName(name);
  const signedIn = useSession((s) => s.status === "authenticated");
  const myIds = useMyIds();
  const [modal, setModal] = useState<"buy" | "offer" | null>(null);

  if (view.isLoading) return <Skeleton className="h-28 w-full rounded-2xl" />;
  const v = view.data;
  if (!v) return null;
  const owned = signedIn && !!myIds.data?.some((i) => i.id === v.maxId || i.name === name);
  const listing = v.listing;
  const active = listing?.status === "active";
  const offerAsset: OfferAsset | null = listing
    ? { chain: listing.chain, asset: listing.asset, symbol: listing.symbol }
    : v.lastSale
      ? { chain: v.lastSale.chain, asset: v.lastSale.asset, symbol: v.lastSale.symbol }
      : getEnv().marketAssets[0] // v1 settles in USDC only (spec §2)
        ? { ...getEnv().marketAssets[0]!, symbol: "USDC" }
        : null;
  const canOffer = !owned && (active || v.listable) && !!offerAsset;
  if (!listing && !v.lastSale && !canOffer) return null;

  return (
    <section aria-labelledby="market-panel" className="flex w-full flex-col gap-4 rounded-2xl border border-line bg-surface p-6 text-left">
      <h2 id="market-panel" className="m-0 text-[15px] font-semibold">Market</h2>
      {listing ? (
        <div className="flex flex-col gap-1">
          <div className="flex items-baseline gap-2">
            <span className="tabular text-[32px] font-semibold">{displayDecimal(listing.price)}</span>
            <span className="text-lg font-semibold text-muted">{listing.symbol}</span>
          </div>
          <span className="text-[13px] text-muted">
            {active ? `Listed for sale ${relativeTime(listing.createdAt)}` : "A sale is in progress for this name."}
            {v.liveOfferCount ? ` · ${v.liveOfferCount} open offer${v.liveOfferCount === 1 ? "" : "s"}` : ""}
          </span>
        </div>
      ) : (
        <p className="m-0 text-[15px] text-muted">
          Not listed for sale.{v.liveOfferCount ? ` ${v.liveOfferCount} open offer${v.liveOfferCount === 1 ? "" : "s"}.` : ""}
        </p>
      )}
      {v.lastSale ? (
        <p className="m-0 text-[13px] text-muted">Last sold for {displayDecimal(v.lastSale.price)} {v.lastSale.symbol} on {formatDate(v.lastSale.completedAt)}.</p>
      ) : null}
      {owned ? (
        <p className="m-0 text-sm">This is yours. <Link href="/dashboard#market">Manage it on your dashboard</Link>.</p>
      ) : (
        <div className="flex flex-wrap gap-3">
          {active ? <Button onClick={() => setModal("buy")}>Buy now</Button> : null}
          {canOffer ? <Button variant={active ? "secondary" : "primary"} onClick={() => setModal("offer")}>Make offer</Button> : null}
        </div>
      )}
      {modal === "buy" && listing ? (
        <BuyModal onClose={() => setModal(null)} target={{
          kind: "listing", listingId: listing.id, handle: v.handle, chain: listing.chain, asset: listing.asset, symbol: listing.symbol, price: listing.price,
        }} />
      ) : null}
      {modal === "offer" && offerAsset ? (
        <OfferModal handle={v.handle} asset={offerAsset} listPrice={active ? listing?.price : undefined} onClose={() => setModal(null)} />
      ) : null}
    </section>
  );
}
