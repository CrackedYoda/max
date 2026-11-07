import { Suspense } from "react";

import { Header } from "@/components/Header";

import { MarketView } from "./MarketView";

export default function MarketPage() {
  return (
    <div className="min-h-screen">
      <Header />
      <Suspense>
        <MarketView />
      </Suspense>
    </div>
  );
}
