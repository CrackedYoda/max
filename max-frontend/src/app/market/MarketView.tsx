"use client";

import { isMaxApiError, type ListingCard as Card } from "@max/sdk";
import { useState } from "react";

import { BuyModal } from "@/components/market/BuyModal";
import { ListingCard } from "@/components/market/ListingCard";
import { Button } from "@/components/ui/Button";
import { Banner, Heading, Skeleton } from "@/components/ui/Blocks";
import { Segmented } from "@/components/ui/Fields";
import { marketErrorMessage } from "@/error/market";
import { useMarketSearch } from "@/hooks/queries";
import { useDebounced } from "@/hooks/useDebounced";
import { stripPrefix } from "@/validation/identity";

type Sort = "newest" | "price_asc" | "price_desc";
const PRICE = /^\d*\.?\d*$/;

export function MarketView() {
  const [q, setQ] = useState("");
  const [minPrice, setMin] = useState("");
  const [maxPrice, setMax] = useState("");
  const [sort, setSort] = useState<Sort>("newest");
  const [buying, setBuying] = useState<Card | null>(null);

  const params = {
    q: useDebounced(stripPrefix(q).trim()) || undefined,
    minPrice: useDebounced(minPrice.trim()) || undefined,
    maxPrice: useDebounced(maxPrice.trim()) || undefined,
    sort,
    limit: 24,
  };
  const search = useMarketSearch(params);
  const listings = search.data?.pages.flatMap((p) => p.listings) ?? [];

  if (isMaxApiError(search.error, "MARKET_DISABLED")) {
    return (
      <main className="mx-auto flex max-w-[640px] flex-col gap-3 px-6 py-24">
        <h1 className="m-0 font-display text-[36px] font-medium">The market isn’t open yet</h1>
        <p className="m-0 text-muted">Buying and selling MAX IDs is coming soon.</p>
      </main>
    );
  }

  return (
    <main className="mx-auto flex max-w-[1200px] flex-col gap-8 px-4 pb-24 pt-14 sm:px-6">
      <Heading title="Market" lead="Buy a MAX ID from its owner. Payment and the name change hands in one step; MAX never holds the money." />

      <form role="search" aria-label="Search listings" onSubmit={(e) => e.preventDefault()}
        className="grid gap-4 rounded-2xl border border-line bg-surface p-5 md:grid-cols-[minmax(0,2fr)_minmax(0,1fr)_minmax(0,1fr)]">
        <Field id="mq" label="Search">
          <input id="mq" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Handle, e.g. yoda" autoComplete="off" spellCheck={false}
            className="min-h-12 w-full rounded-xl border border-line bg-surface px-4 font-mono text-base outline-none focus:border-ink" />
        </Field>
        <Field id="mmin" label="Min price">
          <input id="mmin" value={minPrice} inputMode="decimal" placeholder="0" onChange={(e) => PRICE.test(e.target.value) && setMin(e.target.value)}
            className="tabular min-h-12 w-full rounded-xl border border-line bg-surface px-4 text-base outline-none focus:border-ink" />
        </Field>
        <Field id="mmax" label="Max price">
          <input id="mmax" value={maxPrice} inputMode="decimal" placeholder="Any" onChange={(e) => PRICE.test(e.target.value) && setMax(e.target.value)}
            className="tabular min-h-12 w-full rounded-xl border border-line bg-surface px-4 text-base outline-none focus:border-ink" />
        </Field>
        <div className="md:col-span-3">
          <Segmented label="Sort" value={sort} onChange={setSort}
            options={[{ value: "newest", label: "Newest" }, { value: "price_asc", label: "Price ↑" }, { value: "price_desc", label: "Price ↓" }]} />
        </div>
      </form>

      {search.error ? <Banner tone="danger">{marketErrorMessage(search.error)}</Banner> : null}

      <section aria-label="Listings" aria-busy={search.isLoading} className="grid gap-4 [grid-template-columns:repeat(auto-fill,minmax(min(100%,260px),1fr))]">
        {search.isLoading
          ? Array.from({ length: 6 }, (_, i) => <Skeleton key={i} className="h-[176px] rounded-2xl" />)
          : listings.map((l) => <ListingCard key={l.listingId} listing={l} onBuy={() => setBuying(l)} />)}
      </section>
      {!search.isLoading && !search.error && !listings.length ? (
        <p className="m-0 text-center text-[15px] text-muted">{params.q || params.minPrice || params.maxPrice ? "No listings match. Try a wider price range." : "Nothing is listed yet."}</p>
      ) : null}
      {search.hasNextPage ? (
        <Button variant="secondary" className="self-center" loading={search.isFetchingNextPage} onClick={() => search.fetchNextPage()}>Load more</Button>
      ) : null}

      {buying ? (
        <BuyModal onClose={() => setBuying(null)} target={{
          kind: "listing", listingId: buying.listingId, handle: buying.handle, chain: buying.chain, asset: buying.asset,
          symbol: buying.symbol, price: buying.price, priceBaseUnits: buying.priceBaseUnits,
        }} />
      ) : null}
    </main>
  );
}

function Field({ id, label, children }: { id: string; label: string; children: React.ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <label htmlFor={id} className="text-sm font-semibold">{label}</label>
      {children}
    </div>
  );
}
