import type { ReactNode } from "react";

import { cx } from "./cx";
import { Icon, type IconName } from "./Icon";

export function Card({ children, className, as: Tag = "div" }: { children: ReactNode; className?: string; as?: "div" | "section" | "article" | "form" }) {
  return <Tag className={cx("rounded-2xl border border-line bg-surface", className)}>{children}</Tag>;
}

/** Page heading: Fraunces display title + lead (Noma's Heading). */
export function Heading({ title, lead, size = "lg", id }: { title: ReactNode; lead?: ReactNode; size?: "xl" | "lg" | "md"; id?: string }) {
  const sizes = { xl: "text-[clamp(40px,6vw,68px)] leading-[1.05] tracking-[-0.02em]", lg: "text-[44px] tracking-[-0.01em]", md: "text-[32px]" };
  return (
    <div className="flex flex-col gap-3">
      <h1 id={id} className={cx("m-0 font-display font-medium", sizes[size])}>{title}</h1>
      {lead ? <p className="m-0 text-[17px] leading-relaxed text-muted">{lead}</p> : null}
    </div>
  );
}

type Tone = "info" | "success" | "warn" | "danger";
const tones: Record<Tone, { cls: string; icon: IconName }> = {
  info: { cls: "bg-sunken text-body", icon: "alert" },
  success: { cls: "bg-success-soft text-[#1E5440]", icon: "shield" },
  warn: { cls: "bg-clay-soft text-clay-text-hover text-[#7E3A20]", icon: "alert" },
  danger: { cls: "bg-danger-soft text-danger-text", icon: "alert" },
};

/** Inline notice (Noma's Banner). Danger announces itself to screen readers. */
export function Banner({ tone = "info", title, children, icon }: { tone?: Tone; title?: ReactNode; children?: ReactNode; icon?: IconName }) {
  const t = tones[tone];
  return (
    <div role={tone === "danger" ? "alert" : "note"} className={cx("flex items-start gap-3 rounded-xl px-4 py-3.5 text-sm leading-normal", t.cls)}>
      <Icon name={icon ?? t.icon} size={18} className="mt-px shrink-0" />
      <div className="flex flex-col gap-1">
        {title ? <strong className="text-[15px]">{title}</strong> : null}
        {children ? <span>{children}</span> : null}
      </div>
    </div>
  );
}

type PillTone = "neutral" | "clay" | "success" | "danger";
const pillTones: Record<PillTone, string> = {
  neutral: "bg-sunken text-body",
  clay: "bg-clay-soft text-clay-text",
  success: "bg-success-soft text-success",
  danger: "bg-danger-soft text-[#9C2F2F]",
};

export function Pill({ tone = "neutral", icon, children, className }: { tone?: PillTone; icon?: IconName; children: ReactNode; className?: string }) {
  return (
    <span className={cx("inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 text-[13px] font-semibold", pillTones[tone], className)}>
      {icon ? <Icon name={icon} size={14} strokeWidth={2.5} /> : null}
      {children}
    </span>
  );
}

export function KindBadge({ type }: { type: "human" | "agent" }) {
  return (
    <span className={cx("rounded-full px-2.5 py-[3px] text-xs font-semibold", type === "agent" ? "bg-clay-soft text-clay-text" : "bg-sunken text-body")}>
      {type === "agent" ? "Agent" : "Human"}
    </span>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return <div className={cx("rounded-md bg-sunken", className)} />;
}

export function SectionHeader({ title, lead, action, id }: { title: string; lead?: string; action?: ReactNode; id?: string }) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-4">
      <div className="flex flex-col gap-1.5">
        <h2 id={id} className="m-0 font-display text-[32px] font-medium">{title}</h2>
        {lead ? <p className="m-0 text-[15px] text-muted">{lead}</p> : null}
      </div>
      {action}
    </div>
  );
}

/** Label/value row for reviews (Noma's ReviewRow). */
export function ReviewRow({ label, value, mono }: { label: string; value: ReactNode; mono?: boolean }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="text-[13px] text-muted">{label}</dt>
      <dd className={cx("m-0 font-semibold", mono && "font-mono font-normal")}>{value}</dd>
    </div>
  );
}
