"use client";

import { forwardRef, type InputHTMLAttributes, type ReactNode } from "react";

import { cx } from "./cx";

type FieldProps = InputHTMLAttributes<HTMLInputElement> & {
  label: ReactNode;
  hint?: ReactNode;
  error?: string;
  trailing?: ReactNode;
};

export const TextField = forwardRef<HTMLInputElement, FieldProps>(function TextField({ label, hint, error, trailing, id, className, ...input }, ref) {
  const fid = id ?? (typeof label === "string" ? label.toLowerCase().replace(/\W+/g, "-") : undefined);
  return (
    <div className="flex flex-col gap-2.5">
      <label htmlFor={fid} className="text-sm font-semibold">{label}</label>
      <div className={cx("field flex items-center gap-2 rounded-xl border bg-surface px-4", error ? "border-danger" : "border-line focus-within:border-ink")}>
        <input
          ref={ref}
          id={fid}
          aria-invalid={!!error || undefined}
          aria-describedby={error || hint ? `${fid}-msg` : undefined}
          autoComplete="off"
          spellCheck={false}
          {...input}
          className={cx("min-h-12 min-w-0 flex-1 border-0 bg-transparent text-base text-ink outline-none", className)}
        />
        {trailing}
      </div>
      {error ? (
        <p id={`${fid}-msg`} role="alert" className="m-0 text-sm text-danger">{error}</p>
      ) : hint ? (
        <p id={`${fid}-msg`} className="m-0 text-[13px] text-muted">{hint}</p>
      ) : null}
    </div>
  );
});

/** The `max.` handle input used on Landing, Claim and Send. */
export const HandleInput = forwardRef<HTMLInputElement, Omit<InputHTMLAttributes<HTMLInputElement>, "size"> & { scale?: "xl" | "lg" | "md"; tone?: "neutral" | "ok" | "bad"; trailing?: ReactNode }>(
  function HandleInput({ scale = "lg", tone = "neutral", trailing, className, ...input }, ref) {
    const text = { xl: "text-2xl", lg: "text-xl", md: "text-lg" }[scale];
    const border = tone === "bad" ? "border-danger" : tone === "ok" ? "border-success-strong" : "border-line focus-within:border-ink";
    return (
      <div className={cx("field flex flex-wrap items-center gap-1 rounded-xl border bg-surface px-4", border, className)}>
        <span aria-hidden="true" className={cx("font-mono font-semibold text-accent", text)}>max.</span>
        <input
          ref={ref}
          autoComplete="off"
          autoCapitalize="off"
          spellCheck={false}
          {...input}
          className={cx("min-w-0 flex-[1_1_160px] border-0 bg-transparent py-3 font-mono text-ink outline-none", text)}
        />
        {trailing}
      </div>
    );
  },
);

/** Two-option segmented control (Noma's SegmentedTabs). */
export function Segmented<T extends string>({ options, value, onChange, label }: { options: readonly { value: NoInfer<T>; label: string }[]; value: T; onChange(v: NoInfer<T>): void; label: string }) {
  return (
    <div role="radiogroup" aria-label={label} className="grid gap-2 rounded-[14px] bg-sunken p-1" style={{ gridTemplateColumns: `repeat(${options.length}, minmax(0, 1fr))` }}>
      {options.map((o) => {
        const on = o.value === value;
        return (
          <button key={o.value} type="button" role="radio" aria-checked={on} onClick={() => onChange(o.value)}
            className={cx("min-h-12 rounded-[10px] text-[15px]", on ? "border border-line bg-surface font-semibold text-ink shadow-[0_1px_2px_rgba(20,20,19,0.06)]" : "font-medium text-muted")}>
            {o.label}
          </button>
        );
      })}
    </div>
  );
}
