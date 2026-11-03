import { Suspense } from "react";

import { ConnectFlow } from "./ConnectFlow";

export default function ConnectPage() {
  return (
    <Suspense>
      <ConnectFlow />
    </Suspense>
  );
}
