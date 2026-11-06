"use client";

import { isPendingApproval } from "@max/sdk";
import { usePathname } from "next/navigation";
import { useState } from "react";

import { max } from "@/api/client";
import { marketErrorMessage } from "@/error/market";
import { useInvalidateMarket, useMintDecimals, useWallets } from "@/hooks/queries";
import { toast } from "@/store/flows";
import { useSession } from "@/store/session";
import { displayDecimal, toBaseUnits } from "@/utils/money";

import { Handle } from "../identity/Handle";
import { Button, ButtonLink } from "../ui/Button";
import { Banner } from "../ui/Blocks";
import { Dialog } from "../ui/Dialog";

import { useNative, walletsFor, WalletSelect } from "./shared";

export type OfferAsset = { chain: string; asset: string; symbol: string };

const EXPIRY_DAYS = [1, 3, 7, 14, 30] as const;

/** Make an offer on a name, paid later from one linked wallet if the owner accepts. */
export function OfferModal({ handle, asset, listPrice, onClose }: { handle: string; asset: OfferAsset; listPrice?: string; onClose(): void }) {
  const status = useSession((s) => s.status);
  const path = usePathname();
  const wallets = useWallets();
  const invalidate = useInvalidateMarket();
  const native = useNative(asset.chain);
  const decimals = useMintDecimals(asset.chain, asset.asset);
  const payers = walletsFor(wallets.data, asset.chain);
  const [amount, setAmount] = useState("");
  const [from, setFrom] = useState("");
  const [days, setDays] = useState<number>(7);
  const [failure, setFailure] = useState<unknown>(null);
  const [pending, setPending] = useState(false);
  const fromAddress = from || payers[0]?.address || "";
  const amountOk = /^\d+(\.\d+)?$/.test(amount.trim()) && (decimals.data == null || (toBaseUnits(amount, decimals.data) ?? BigInt(0)) > BigInt(0));
  const units = { decimals: decimals.data, symbol: asset.symbol, nativeSymbol: native.symbol, nativeDecimals: native.decimals, amount: amount.trim() };
  // Formatted at render so late-arriving token decimals still apply.
  const error = failure == null ? null : marketErrorMessage(failure, units);
  const setError = setFailure;

  async function submit() {
    setError(null);
    setPending(true);
    try {
      const expiresAt = new Date(Date.now() + days * 86_400_000).toISOString();
      const res = await max().market.offer({ name: handle.replace(/^max\./, ""), amount: amount.trim(), chain: asset.chain, asset: asset.asset, fromAddress, expiresAt });
      invalidate();
      toast(isPendingApproval(res) ? "Offer held for approval" : `Offer sent for max.${handle.replace(/^max\./, "")}`);
      onClose();
    } catch (err) {
      setError(err);
    } finally {
      setPending(false);
    }
  }

  return (
    <Dialog title={<>Make an offer on <Handle name={handle} /></>} labelId="offer-title" onClose={onClose}>
      {status !== "authenticated" ? (
        <>
          <p className="m-0 text-[15px] text-muted">Sign in with a wallet to make an offer.</p>
          <ButtonLink href={`/connect?next=${encodeURIComponent(path)}`}>Sign in to make an offer</ButtonLink>
        </>
      ) : !payers.length && !wallets.isLoading ? (
        <>
          <Banner tone="info">Offers are paid from a Solana wallet linked to your MAX account. Link one first.</Banner>
          <ButtonLink href={`/connect?mode=link&next=${encodeURIComponent(path)}`}>Link a Solana wallet</ButtonLink>
        </>
      ) : (
        <form className="flex flex-col gap-5" onSubmit={(e) => { e.preventDefault(); if (amountOk && fromAddress) submit(); }}>
          {listPrice ? <p className="m-0 text-sm text-muted">Listed at {displayDecimal(listPrice)} {asset.symbol}.</p> : null}
          <div className="flex flex-col gap-2.5">
            <label htmlFor="offer-amount" className="text-sm font-semibold">Your offer</label>
            <div className="field flex items-center gap-2 rounded-xl border border-line px-4 focus-within:border-ink">
              <input id="offer-amount" value={amount} inputMode="decimal" placeholder="0.00" autoComplete="off"
                onChange={(e) => { setError(null); setAmount(e.target.value.replace(",", ".")); }}
                className="tabular min-h-12 min-w-0 flex-1 border-0 bg-transparent text-xl font-semibold outline-none" />
              <span className="text-[15px] font-semibold text-muted">{asset.symbol}</span>
            </div>
            {amount && !amountOk ? <p role="alert" className="m-0 text-[13px] text-danger">Enter an amount like 25 or 25.50</p> : null}
          </div>
          <WalletSelect id="offer-from" label="Pay from" wallets={payers} value={fromAddress} onChange={setFrom} />
          <div className="flex flex-col gap-2.5">
            <label htmlFor="offer-expiry" className="text-sm font-semibold">Offer open for</label>
            <select id="offer-expiry" value={days} onChange={(e) => setDays(Number(e.target.value))}
              className="min-h-12 w-full rounded-xl border border-line bg-surface px-3.5 text-[15px] outline-none focus:border-ink">
              {EXPIRY_DAYS.map((d) => <option key={d} value={d}>{d} day{d === 1 ? "" : "s"}</option>)}
            </select>
          </div>
          <p className="m-0 text-[13px] leading-relaxed text-muted">
            Nothing is charged now. The wallet must keep holding the amount plus a little {native.symbol} for fees, or the owner won’t see the offer.
            If it’s accepted you’ll have 24 hours to pay; unpaid accepted offers count against you.
          </p>
          {error ? <Banner tone="danger">{error}</Banner> : null}
          <Button type="submit" disabled={!amountOk || !fromAddress} loading={pending}>
            {amountOk ? `Offer ${displayDecimal(amount.trim())} ${asset.symbol}` : "Make offer"}
          </Button>
        </form>
      )}
    </Dialog>
  );
}
