"use client";

import Link from "next/link";

import { Header } from "@/components/Header";
import { AdminMarket } from "@/components/market/AdminMarket";
import { RequireAuth } from "@/components/RequireAuth";
import { Spinner } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Blocks";
import { useIsMarketAdmin } from "@/hooks/queries";

export default function AdminMarketPage() {
  return (
    <div className="min-h-screen">
      <Header />
      <RequireAuth>
        <AdminGate />
      </RequireAuth>
    </div>
  );
}

/** UX gate only: max-core's admin routes answer 404 to anyone not in ADMIN_ACCOUNT_IDS. */
function AdminGate() {
  const { isAdmin, isLoading } = useIsMarketAdmin();
  if (isLoading) {
    return <div className="flex min-h-[40vh] items-center justify-center text-muted" aria-label="Loading"><Spinner className="size-6" /></div>;
  }
  if (!isAdmin) {
    return (
      <main className="mx-auto flex max-w-[640px] flex-col items-center gap-4 px-6 py-24 text-center">
        <h1 className="m-0 font-display text-[40px] font-medium">Page not found</h1>
        <Link href="/dashboard">Back to dashboard</Link>
      </main>
    );
  }
  return (
    <main className="mx-auto flex max-w-[1200px] flex-col gap-8 px-4 pb-24 pt-14 sm:px-6">
      <Heading title="Market admin" lead="Review large sales, step in on stuck ones, and restrict accounts. Every action is written to the audit log." />
      <AdminMarket />
    </main>
  );
}
