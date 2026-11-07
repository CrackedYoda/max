"use client";

import { isPendingApproval, type Listing, type MarketAsset, type MaxId } from "@max/sdk";
import { useState } from "react";

import { max } from "@/api/client";
import { listableReason, marketErrorMessage } from "@/error/market";
import { useInvalidateMarket, useMarketEligibility, useWallets } from "@/hooks/queries";
import { toast } from "@/store/flows";
import { displayDecimal, displayUnits, sellerPercent, sellerReceives, toBaseUnits } from "@/utils/money";

import { Button } from "../ui/Button";
import { Banner, Skeleton } from "../ui/Blocks";

import { walletsFor, WalletSelect } from "./shared";

/**
 * List a MAX ID (or change a listing's price). Shows exactly what the seller
 * receives: price − floor(price × feeBps / 10000), in base units like max-core.
 */
export function SellForm({ id, listing, onDone }: { id: MaxId; listing?: Listing; onDone(): void }) {
  const elig = useMarketEligibility(id.id);
  const wallets = useWallets();
  const invalidate = useInvalidateMarket();
  const [assetKey, setAssetKey] = useState("");
  const [price, setPrice] = useState(listing?.price ?? "");
  const [payout, setPayout] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  if (elig.isLoading) return <div className="flex flex-col gap-3"><Skeleton className="h-12 w-full" /><Skeleton className="h-12 w-full" /></div>;
  if (elig.error || !elig.data) return <Banner tone="danger">{marketErrorMessage(elig.error)}</Banner>;
  const e = elig.data;
  // An existing listing is already past the eligibility checks; only its price changes.
  if (!listing && !e.listable) {
    return (
      <Banner tone="info" title="This name can’t be listed yet">
        {listableReason(e.reason, { listableAt: e.listableAt, renameAvailableAt: e.renameAvailableAt })}
      </Banner>
    );
  }
  if (!e.assets.length) return <Banner tone="info">No payment asset is enabled on the marketplace yet.</Banner>;

  const asset: MarketAsset =
    (listing && e.assets.find((a) => a.chain === listing.chain && a.asset === listing.asset)) ||
    e.assets.find((a) => `${a.chain}:${a.asset}` === assetKey) ||
    e.assets[0]!;
  const payouts = walletsFor(wallets.data, asset.chain);
  const payoutAddress = payout || payouts[0]?.address || "";
  const base = price.trim() ? toBaseUnits(price, asset.decimals) : null;
  const min = toBaseUnits(e.minPrice, asset.decimals) ?? BigInt(0);
  const priceErr = !price.trim() ? null : base === null ? `Enter a price like 250 or 250.50 (up to ${asset.decimals} decimals).` : base < min ? `The minimum price is ${displayDecimal(e.minPrice)} ${asset.symbol}.` : null;
  const ok = base !== null && !priceErr && (listing || payoutAddress);
  const receive = base !== null && !priceErr ? sellerReceives(base, e.feeBps) : null;

  async function submit() {
    setError(null);
    setPending(true);
    try {
      const res = listing
        ? await max().market.updateListing(listing.id, price.trim())
        : await max().market.list({ maxId: id.id, price: price.trim(), chain: asset.chain, asset: asset.asset, payoutAddress });
      invalidate();
      toast(isPendingApproval(res) ? "Held for approval by the agent’s administrator" : listing ? "Price updated" : `${id.handle} is listed`);
      onDone();
    } catch (err) {
      setError(marketErrorMessage(err));
    } finally {
      setPending(false);
    }
  }

  return (
    <form className="flex flex-col gap-5" onSubmit={(ev) => { ev.preventDefault(); if (ok) submit(); }}>
      {!listing && e.assets.length > 1 ? (
        <div className="flex flex-col gap-2.5">
          <label htmlFor="sell-asset" className="text-sm font-semibold">Get paid in</label>
          <select id="sell-asset" value={`${asset.chain}:${asset.asset}`} onChange={(ev) => setAssetKey(ev.target.value)}
            className="min-h-12 w-full rounded-xl border border-line bg-surface px-3.5 text-[15px] outline-none focus:border-ink">
            {e.assets.map((a) => <option key={`${a.chain}:${a.asset}`} value={`${a.chain}:${a.asset}`}>{a.symbol} on {a.chain}</option>)}
          </select>
        </div>
      ) : null}
      <div className="flex flex-col gap-2.5">
        <label htmlFor="sell-price" className="text-sm font-semibold">Price</label>
        <div className={`field flex items-center gap-2 rounded-xl border px-4 ${priceErr ? "border-danger" : "border-line focus-within:border-ink"}`}>
          <input id="sell-price" value={price} inputMode="decimal" placeholder="0.00" autoComplete="off" aria-invalid={!!priceErr || undefined}
            onChange={(ev) => { setError(null); setPrice(ev.target.value.replace(",", ".")); }}
            className="tabular min-h-12 min-w-0 flex-1 border-0 bg-transparent text-xl font-semibold outline-none" />
          <span className="text-[15px] font-semibold text-muted">{asset.symbol}</span>
        </div>
        {priceErr ? <p role="alert" className="m-0 text-[13px] text-danger">{priceErr}</p> : <p className="m-0 text-[13px] text-muted">Minimum {displayDecimal(e.minPrice)} {asset.symbol}. Buyers pay this price.</p>}
      </div>
      <p className="m-0 rounded-xl bg-success-soft px-4 py-3 text-[15px] font-semibold text-[#1E5440]" aria-live="polite">
        You receive {sellerPercent(e.feeBps)}%: {receive !== null ? displayUnits(receive, asset.decimals) : "0"} {asset.symbol}
      </p>
      {listing ? (
        <p className="m-0 text-[13px] text-muted">Paid to the wallet you chose when listing. To change it, delist and list again.</p>
      ) : payouts.length ? (
        <WalletSelect id="sell-payout" label="Get paid to" wallets={payouts} value={payoutAddress} onChange={setPayout} />
      ) : (
        <Banner tone="info">Link a Solana wallet to your account to get paid. Sales settle on Solana in {asset.symbol}.</Banner>
      )}
      <p className="m-0 text-[13px] leading-relaxed text-muted">
        When it sells, the buyer pays you and MAX’s {e.feeBps / 100}% fee in one transaction. The name moves to the buyer, and its wallets and profile are reset.
      </p>
      {error ? <Banner tone="danger">{error}</Banner> : null}
      <Button type="submit" disabled={!ok} loading={pending}>{listing ? "Update price" : "List for sale"}</Button>
    </form>
  );
}
