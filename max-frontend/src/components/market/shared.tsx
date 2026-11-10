"use client";

import type { ListingStatus, OfferStatus, SaleStatus, Wallet } from "@max/sdk";
import { useEffect, useRef, useState } from "react";

import { useChains } from "@/hooks/queries";
import { connectPhantom, WalletError, type ConnectedWallet } from "@/services/wallets";
import { useWallet } from "@/store/wallet";
import { shortAddress } from "@/utils/format";

import { Pill } from "../ui/Blocks";

const SALE: Record<SaleStatus, { label: string; tone: "neutral" | "clay" | "success" | "danger" }> = {
  pending_review: { label: "In review", tone: "clay" },
  locked: { label: "Awaiting payment", tone: "neutral" },
  submitted: { label: "Submitted", tone: "neutral" },
  confirming: { label: "Confirming", tone: "neutral" },
  completed: { label: "Completed", tone: "success" },
  expired: { label: "Expired", tone: "danger" },
  cancelled: { label: "Cancelled", tone: "danger" },
  manual_review: { label: "Manual review", tone: "clay" },
  reversed: { label: "Reversed", tone: "danger" },
};

const LISTING: Record<ListingStatus, { label: string; tone: "neutral" | "clay" | "success" | "danger" }> = {
  active: { label: "Listed", tone: "success" },
  locked: { label: "Sale in progress", tone: "clay" },
  sold: { label: "Sold", tone: "neutral" },
  cancelled: { label: "Delisted", tone: "neutral" },
  admin_delisted: { label: "Removed by MAX", tone: "danger" },
};

const OFFER: Record<OfferStatus, { label: string; tone: "neutral" | "clay" | "success" | "danger" }> = {
  live: { label: "Open", tone: "success" },
  unfunded: { label: "Unfunded", tone: "danger" },
  accepted: { label: "Accepted: pay now", tone: "clay" },
  declined: { label: "Declined", tone: "neutral" },
  superseded: { label: "Closed", tone: "neutral" },
  expired: { label: "Expired", tone: "neutral" },
  withdrawn: { label: "Withdrawn", tone: "neutral" },
  lapsed: { label: "Lapsed (unpaid)", tone: "danger" },
  completed: { label: "Completed", tone: "success" },
};

const small = "!px-2.5 !py-1 !text-xs";

export function SaleStatusPill({ status }: { status: SaleStatus }) {
  const s = SALE[status] ?? { label: status, tone: "neutral" as const };
  return <Pill tone={s.tone} className={small}>{s.label}</Pill>;
}

export function ListingStatusPill({ status }: { status: ListingStatus }) {
  const s = LISTING[status] ?? { label: status, tone: "neutral" as const };
  return <Pill tone={s.tone} className={small}>{s.label}</Pill>;
}

export function OfferStatusPill({ status }: { status: OfferStatus }) {
  const s = OFFER[status] ?? { label: status, tone: "neutral" as const };
  return <Pill tone={s.tone} className={small}>{s.label}</Pill>;
}

/** The native coin and its decimals for a chain key ("SOL", 9 on Solana). */
export function useNative(chainKey: string | undefined): { symbol: string; decimals: number; name: string; testnet: boolean } {
  const chains = useChains();
  const c = chains.data?.find((x) => x.key === chainKey);
  return { symbol: c?.nativeCoin ?? "SOL", decimals: c?.nativeDecimals ?? 9, name: c?.name ?? chainKey ?? "", testnet: c?.testnet ?? !!chainKey?.includes("devnet") };
}

/**
 * Connect Phantom and make sure it is a Solana wallet linked to the signed-in
 * account (`required` pins one address, e.g. an offer's paying wallet).
 */
export async function connectLinkedSolana(linked: Wallet[], required?: string): Promise<ConnectedWallet> {
  const current = useWallet.getState().wallet;
  const w = current?.namespace === "solana" && (!required || current.address === required) ? current : await connectPhantom();
  useWallet.getState().set(w);
  if (required && w.address !== required) {
    throw new WalletError(`Phantom is on ${shortAddress(w.address)}. Switch Phantom to ${shortAddress(required)}, the wallet this payment comes from.`);
  }
  if (!linked.some((x) => x.namespace === "solana" && x.address === w.address)) {
    throw new WalletError(`${shortAddress(w.address)} isn’t linked to your MAX account. Link it on your dashboard, or switch wallets in Phantom.`);
  }
  return w;
}

/** mm:ss until `iso`, ticking every second; null once passed. */
export function useCountdown(iso: string | undefined): { label: string | null; secondsLeft: number } {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!iso) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [iso]);
  if (!iso) return { label: null, secondsLeft: 0 };
  const left = Math.floor((new Date(iso).getTime() - now) / 1000);
  if (left <= 0) return { label: null, secondsLeft: 0 };
  const h = Math.floor(left / 3600), m = Math.floor((left % 3600) / 60), s = left % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return { label: h ? `${h}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`, secondsLeft: left };
}

/** A stable idempotency key per exact set of terms, so a retry never makes a second sale. */
export function useIdempotencyKey() {
  const ref = useRef<{ fp: string; key: string } | null>(null);
  return (fingerprint: string) => {
    if (ref.current?.fp !== fingerprint) ref.current = { fp: fingerprint, key: crypto.randomUUID() };
    return ref.current.key;
  };
}

/** Linked wallets in the namespace a market chain settles on. */
export function walletsFor(wallets: Wallet[] | undefined, chainKey: string): Wallet[] {
  const ns = chainKey.startsWith("solana") ? "solana" : "evm";
  return (wallets ?? []).filter((w) => w.namespace === ns);
}

export function WalletSelect({ id, label, wallets, value, onChange, disabled }: { id: string; label: string; wallets: Wallet[]; value: string; onChange(v: string): void; disabled?: boolean }) {
  return (
    <div className="flex flex-col gap-2.5">
      <label htmlFor={id} className="text-sm font-semibold">{label}</label>
      <select id={id} value={value} disabled={disabled} onChange={(e) => onChange(e.target.value)}
        className="min-h-12 w-full rounded-xl border border-line bg-surface px-3.5 font-mono text-sm outline-none focus:border-ink">
        {wallets.map((w) => <option key={w.id} value={w.address}>{shortAddress(w.address, 6, 6)}</option>)}
      </select>
    </div>
  );
}
