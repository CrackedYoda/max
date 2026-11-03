import { Suspense } from "react";

import { Header } from "@/components/Header";
import { RequireAuth } from "@/components/RequireAuth";

import { VerifyView } from "./VerifyView";

export default function VerifyPage() {
  return (
    <div className="min-h-screen">
      <Header />
      <RequireAuth>
        <Suspense>
          <VerifyView />
        </Suspense>
      </RequireAuth>
    </div>
  );
}
