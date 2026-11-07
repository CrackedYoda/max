"use client";

import type { ListingCard as Card } from "@max/sdk";
import Link from "next/link";

import { relativeTime } from "@/utils/format";
import { displayDecimal } from "@/utils/money";

import { Handle } from "../identity/Handle";
import { Button } from "../ui/Button";

import { useNative } from "./shared";

export function ListingCard({ listing, onBuy }: { listing: Card; onBuy(): void }) {
  const chain = useNative(listing.chain);
  const handle = listing.handle.replace(/^max\./, "");
  return (
    <article className="flex flex-col gap-4 rounded-2xl border border-line bg-surface p-5">
      <div className="flex flex-col gap-1">
        <Link href={`/max.${handle}`} className="min-w-0 truncate text-xl no-underline !text-ink hover:underline">
          <Handle name={handle} />
        </Link>
        <span className="text-[13px] text-muted">Listed {relativeTime(listing.createdAt)}{chain.testnet ? " · Testnet" : ""}</span>
      </div>
      <div className="flex items-baseline gap-1.5">
        <span className="tabular text-2xl font-semibold">{displayDecimal(listing.price)}</span>
        <span className="text-[15px] font-semibold text-muted">{listing.symbol}</span>
      </div>
      <div className="mt-auto flex flex-wrap gap-2">
        <Button size="sm" onClick={onBuy} className="flex-1">Buy now</Button>
        <Link href={`/max.${handle}`} className="inline-flex min-h-11 items-center px-2 text-sm font-medium">Details</Link>
      </div>
    </article>
  );
}
