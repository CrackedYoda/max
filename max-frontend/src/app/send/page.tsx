import { Suspense } from "react";

import { Header } from "@/components/Header";

import { SendFlow } from "./SendFlow";

export default function SendPage() {
  return (
    <div className="min-h-screen">
      <Header />
      <Suspense>
        <SendFlow />
      </Suspense>
    </div>
  );
}
