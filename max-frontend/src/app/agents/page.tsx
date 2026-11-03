import { Header } from "@/components/Header";
import { RequireAuth } from "@/components/RequireAuth";

import { AgentsView } from "./AgentsView";

export default function AgentsPage() {
  return (
    <div className="min-h-screen">
      <Header />
      <RequireAuth>
        <AgentsView />
      </RequireAuth>
    </div>
  );
}
