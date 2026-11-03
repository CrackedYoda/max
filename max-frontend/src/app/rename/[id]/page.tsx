import { Suspense } from "react";

import { Header } from "@/components/Header";
import { RequireAuth } from "@/components/RequireAuth";

import { RenameFlow } from "./RenameFlow";

export default function RenamePage() {
  return (
    // Dynamic segment: everything reading the URL (header nav, auth gate) streams in.
    <Suspense>
      <div className="min-h-screen">
        <Header />
        <RequireAuth>
          <RenameFlow />
        </RequireAuth>
      </div>
    </Suspense>
  );
}
