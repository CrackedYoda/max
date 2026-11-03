import { Suspense } from "react";

import { Header } from "@/components/Header";
import { RequireAuth } from "@/components/RequireAuth";

import { AgentDetailView } from "./AgentDetailView";

export default function AgentPage() {
  return (
    // Dynamic segment: everything reading the URL (header nav, auth gate) streams in.
    <Suspense>
      <div className="min-h-screen">
        <Header />
        <RequireAuth>
          <AgentDetailView />
        </RequireAuth>
      </div>
    </Suspense>
  );
}
