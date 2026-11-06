"use client";

import { useEffect, useRef, type ReactNode } from "react";

import { Icon } from "./Icon";

/**
 * Modal dialog on the native <dialog> (same look as EditProfileDialog).
 * `locked` keeps Escape from closing it while something must not be interrupted.
 */
export function Dialog({ title, labelId, onClose, locked, children, width = 520 }: {
  title: ReactNode;
  labelId: string;
  onClose(): void;
  locked?: boolean;
  children: ReactNode;
  width?: number;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (d && !d.open) d.showModal();
  }, []);
  return (
    <dialog ref={ref} aria-labelledby={labelId}
      onCancel={(e) => { if (locked) e.preventDefault(); }}
      onClose={onClose}
      style={{ width: `min(${width}px, calc(100vw - 32px))` }}
      className="m-auto max-h-[calc(100dvh-32px)] overflow-y-auto rounded-2xl border border-line bg-surface p-0 text-ink backdrop:bg-[rgba(20,20,19,0.4)]">
      <div className="flex flex-col gap-5 p-6 sm:p-7">
        <div className="flex items-start justify-between gap-3">
          <h2 id={labelId} className="m-0 text-xl font-semibold">{title}</h2>
          <button type="button" aria-label="Close" disabled={locked} onClick={() => ref.current?.close()}
            className="-m-2 inline-flex size-10 shrink-0 items-center justify-center rounded-[10px] text-muted hover:bg-sunken disabled:opacity-40">
            <Icon name="x" size={18} />
          </button>
        </div>
        {children}
      </div>
    </dialog>
  );
}

/** Close the nearest open dialog from inside it. */
export function closeDialog(el: HTMLElement | null) {
  el?.closest("dialog")?.close();
}
