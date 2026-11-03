"use client";

import { useState } from "react";

import { cx } from "./cx";
import { Icon } from "./Icon";

export function CopyButton({ value, label, className }: { value: string; label: string; className?: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      aria-label={done ? "Copied" : label}
      onClick={async () => {
        await navigator.clipboard.writeText(value);
        setDone(true);
        setTimeout(() => setDone(false), 1500);
      }}
      className={cx("inline-flex size-11 shrink-0 items-center justify-center rounded-[10px] text-muted hover:bg-sunken", className)}>
      <Icon name={done ? "check" : "copy"} />
    </button>
  );
}
