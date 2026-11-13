"use client";

import type { Wallet } from "@max/sdk";
import Link from "next/link";
import { useState } from "react";

import { Header } from "@/components/Header";
import { IdCard, IdCardSkeleton } from "@/components/identity/IdCard";
import { ActivityFeed, type ActivityItem } from "@/components/payments/Activity";
import { MarketInbox } from "@/components/market/MarketInbox";
import { RequireAuth } from "@/components/RequireAuth";
import { Button, ButtonLink } from "@/components/ui/Button";
import { Banner, Card, SectionHeader } from "@/components/ui/Blocks";
import { Segmented } from "@/components/ui/Fields";
import { errorCode, errorMessage } from "@/error/messages";
import { useIsMarketAdmin, useMyIds, useRemoveWallet, useTransfers, useWallets } from "@/hooks/queries";
import { toast } from "@/store/flows";
import { formatDate, namespaceLabel, shortAddress } from "@/utils/format";
import { ChainLogo } from "@/components/ui/ChainLogo";

export default function DashboardPage() {
  return (
    <div className="min-h-screen">
      <Header />
      <RequireAuth>
        <Dashboard />
      </RequireAuth>
    </div>
  );
}

function Dashboard() {
  const ids = useMyIds();
  const wallets = useWallets();
  const admin = useIsMarketAdmin();

  return (
    <main className="mx-auto flex max-w-[1200px] flex-col gap-14 px-6 pb-24 pt-14">
      <section aria-labelledby="ids" className="flex flex-col gap-5">
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div className="flex flex-col gap-1.5">
            <h1 id="ids" className="m-0 font-display text-[40px] font-medium tracking-[-0.01em]">My MAX IDs</h1>
            <p className="m-0 text-[15px] text-muted">Choose which of your wallets receives on each network.</p>
          </div>
          <div className="flex flex-wrap items-center gap-4">
            <Link href="/verify" className="text-sm font-medium">Own a reserved name?</Link>
            <ButtonLink href="/claim" size="sm" className="!rounded-xl">{ids.data?.some((i) => !i.isDefault) ? "Claim another" : "Claim a name"}</ButtonLink>
          </div>
        </div>

        {ids.error ? <Banner tone="danger">{errorMessage(ids.error)}</Banner> : null}
        <div className="grid items-start gap-5 [grid-template-columns:repeat(auto-fit,minmax(340px,1fr))]">
          {ids.isLoading || wallets.isLoading ? (
            <>
              <IdCardSkeleton />
              <IdCardSkeleton />
            </>
          ) : (
            <>
              {ids.data?.map((id) => <IdCard key={id.id} id={id} wallets={wallets.data ?? []} />)}
              {ids.data && !ids.data.some((i) => !i.isDefault) ? (
                <Card className="flex flex-col items-start gap-3 p-6">
                  <h2 className="m-0 text-lg font-semibold">No claimed MAX ID yet</h2>
                  <p className="m-0 text-[15px] text-muted">Claim a handle so people can pay you by name.</p>
                  <ButtonLink href="/claim" size="sm">Claim your MAX ID</ButtonLink>
                </Card>
              ) : null}
            </>
          )}
        </div>
      </section>

      {ids.data && wallets.data ? (
        <MarketInbox ids={ids.data} wallets={wallets.data}
          adminLink={admin.isAdmin ? <Link href="/dashboard/admin/market" className="text-sm font-medium">Market admin</Link> : null} />
      ) : null}
      <WalletsSection wallets={wallets.data ?? []} loading={wallets.isLoading} />
      <ActivitySection />
    </main>
  );
}

function WalletsSection({ wallets, loading }: { wallets: Wallet[]; loading: boolean }) {
  const remove = useRemoveWallet();
  const [confirming, setConfirming] = useState<string | null>(null);
  const code = errorCode(remove.error);

  return (
    <section aria-labelledby="wallets" className="flex flex-col gap-5">
      <SectionHeader
        id="wallets"
        title="Wallets"
        lead="Wallets you’ve proven you own. Any of them can sign you in."
        action={<ButtonLink href="/connect?mode=link&next=/dashboard" variant="secondary" size="sm" className="!rounded-xl">Link another wallet</ButtonLink>}
      />
      <div className="overflow-x-auto rounded-2xl border border-line bg-surface">
        <table className="w-full min-w-[560px] border-collapse text-sm">
          <thead>
            <tr className="text-left text-muted">
              {["Network", "Address", "Linked"].map((h) => (
                <th key={h} scope="col" className="border-b border-line px-5 py-3.5 text-xs font-semibold uppercase tracking-[0.06em]">{h}</th>
              ))}
              <th scope="col" className="border-b border-line px-5 py-3.5"><span className="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={4} className="px-5 py-6 text-muted">Loading…</td></tr>
            ) : wallets.map((w, i) => (
              <tr key={w.id}>
                <td className={`px-5 py-3.5 font-semibold ${i < wallets.length - 1 ? "border-b border-line" : ""}`}><span className="inline-flex items-center gap-2"><ChainLogo chain={w.namespace} />{namespaceLabel(w.namespace)}</span></td>
                <td className={`px-5 py-3.5 font-mono ${i < wallets.length - 1 ? "border-b border-line" : ""}`} title={w.address}>{shortAddress(w.address)}</td>
                <td className={`px-5 py-3.5 text-muted ${i < wallets.length - 1 ? "border-b border-line" : ""}`}>{formatDate(w.verifiedAt)}</td>
                <td className={`px-5 py-2 text-right ${i < wallets.length - 1 ? "border-b border-line" : ""}`}>
                  {confirming === w.id ? (
                    <span className="inline-flex gap-1">
                      <Button variant="danger" size="sm" className="!min-h-10 !px-3 !text-sm" loading={remove.isPending}
                        onClick={() => remove.mutate(w.id, {
                          onSuccess: () => {
                            toast(`${namespaceLabel(w.namespace)} wallet removed`);
                            setConfirming(null);
                          },
                          onSettled: () => setConfirming(null),
                        })}>
                        Confirm remove
                      </Button>
                      <Button variant="ghost" size="sm" className="!min-h-10 !px-3 !text-sm !font-medium" onClick={() => setConfirming(null)}>Cancel</Button>
                    </span>
                  ) : (
                    <Button variant="danger" size="sm" className="!min-h-10 !px-3 !text-sm !font-normal" onClick={() => { remove.reset(); setConfirming(w.id); }}>Remove</Button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {code === "LAST_WALLET" ? (
        <Banner tone="danger" title="Can’t remove this wallet.">You can’t remove your only wallet. Link another one first so you can still sign in.</Banner>
      ) : remove.error ? (
        <Banner tone="danger">{errorMessage(remove.error)}</Banner>
      ) : null}
      <p className="m-0 text-[13px] text-muted">Removing a wallet stops payments to it immediately. Payments already in flight keep their original destination.</p>
    </section>
  );
}

function ActivitySection() {
  const [view, setView] = useState<"all" | "received" | "sent">("all");
  const received = useTransfers("received");
  const sent = useTransfers("sent");
  const items: ActivityItem[] = [
    ...(view !== "sent" ? (received.data ?? []).map((payment) => ({ payment, direction: "received" as const })) : []),
    ...(view !== "received" ? (sent.data ?? []).map((payment) => ({ payment, direction: "sent" as const })) : []),
  ];
  const loading = (view !== "sent" && received.isLoading) || (view !== "received" && sent.isLoading);
  const error = (view !== "sent" ? received.error : null) ?? (view !== "received" ? sent.error : null);
  return (
    <section aria-labelledby="activity" className="flex flex-col gap-5">
      <SectionHeader id="activity" title="Activity" lead="Every payment to and from your MAX IDs. Only you can see this." />
      <div className="max-w-[380px]">
        <Segmented label="Show" value={view} onChange={setView}
          options={[{ value: "all", label: "All" }, { value: "received", label: "Received" }, { value: "sent", label: "Sent" }]} />
      </div>
      <Card className="px-3 py-4 sm:px-4">
        {loading ? (
          <p className="m-0 px-1 py-2 text-sm text-muted">Loading…</p>
        ) : error ? (
          <p role="alert" className="m-0 px-1 py-2 text-sm text-danger">{errorMessage(error)}</p>
        ) : items.length ? (
          <ActivityFeed items={items} />
        ) : (
          <p className="m-0 px-1 py-2 text-sm text-muted">
            {view === "sent" ? "You haven’t sent anything yet." : "No payments yet. Share your handle to get paid."}
          </p>
        )}
      </Card>
    </section>
  );
}
