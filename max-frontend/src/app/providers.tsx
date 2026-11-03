"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useState, type ReactNode } from "react";

import { Toasts } from "@/components/ui/Toasts";
import { ErrorBoundary } from "@/error/ErrorBoundary";
import { boot } from "@/services/session";

export function Providers({ children }: { children: ReactNode }) {
  const [qc] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { staleTime: 30_000, refetchOnWindowFocus: true, retry: 1 },
          mutations: { retry: 0 },
        },
      }),
  );
  useEffect(() => {
    boot();
  }, []);
  return (
    <ErrorBoundary>
      <QueryClientProvider client={qc}>
        {children}
        <Toasts />
      </QueryClientProvider>
    </ErrorBoundary>
  );
}
