"use client";

import { useToasts } from "@/store/flows";

import { Icon } from "./Icon";

export function Toasts() {
  const toasts = useToasts((s) => s.toasts);
  return (
    <div aria-live="polite" className="pointer-events-none fixed right-6 top-[84px] z-50 flex flex-col gap-2">
      {toasts.map((t) => (
        <div key={t.id} role="status" className="pointer-events-auto flex items-center gap-2.5 rounded-xl bg-night px-4 py-3 text-sm font-medium text-night-text shadow-[0_8px_24px_rgba(20,20,19,0.16)]">
          <span className={t.tone === "success" ? "text-[#7FC4A3]" : "text-[#F0A3A3]"}>
            <Icon name={t.tone === "success" ? "check" : "alert"} strokeWidth={2.5} />
          </span>
          {t.message}
        </div>
      ))}
    </div>
  );
}
