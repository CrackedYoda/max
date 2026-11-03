"use client";

import { usePathname, useRouter } from "next/navigation";
import { useEffect, type ReactNode } from "react";

import { useSession } from "@/store/session";

import { Spinner } from "./ui/Button";

/** UX gate only: the API enforces auth itself. */
export function RequireAuth({ children }: { children: ReactNode }) {
  const status = useSession((s) => s.status);
  const router = useRouter();
  const path = usePathname();
  useEffect(() => {
    if (status === "anonymous") router.replace(`/connect?next=${encodeURIComponent(path)}`);
  }, [status, router, path]);

  if (status === "authenticated") return <>{children}</>;
  if (status === "unavailable") {
    return (
      <main className="mx-auto flex max-w-[640px] flex-col gap-3 px-6 py-24">
        <h1 className="m-0 font-display text-[32px] font-medium">Can’t reach MAX right now</h1>
        <p className="m-0 text-muted">Nothing has been sent or changed. Check your connection and reload.</p>
      </main>
    );
  }
  return (
    <div className="flex min-h-[40vh] items-center justify-center text-muted" aria-label="Loading"><Spinner className="size-6" /></div>
  );
}
