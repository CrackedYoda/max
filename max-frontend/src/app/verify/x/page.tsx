import { Suspense } from "react";

import { Header } from "@/components/Header";
import { RequireAuth } from "@/components/RequireAuth";

import { VerifyView } from "../VerifyView";

/** MAX's X OAuth callback redirects here with ?x=verified|error. */
export default function VerifyResultPage() {
  return (
    <div className="min-h-screen">
      <Header />
      <RequireAuth>
        <Suspense>
          <VerifyView fromCallback />
        </Suspense>
      </RequireAuth>
    </div>
  );
}
