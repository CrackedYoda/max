import { Suspense } from "react";

import { Header } from "@/components/Header";
import { RequireAuth } from "@/components/RequireAuth";

import { ClaimForm } from "./ClaimForm";

export default function ClaimPage() {
  return (
    <div className="min-h-screen">
      <Header />
      <RequireAuth>
        <Suspense>
          <ClaimForm />
        </Suspense>
      </RequireAuth>
    </div>
  );
}
