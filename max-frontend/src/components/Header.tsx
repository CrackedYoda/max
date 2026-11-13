"use client";

import { useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useState, type ReactNode } from "react";

import { useMe, useWallets } from "@/hooks/queries";
import { logout } from "@/services/session";
import { useSession } from "@/store/session";
import { shortAddress } from "@/utils/format";

import { cx } from "./ui/cx";

function NavLink({ href, children, block }: { href: string; children: ReactNode; block?: boolean }) {
  const path = usePathname();
  const active = path === href || path.startsWith(`${href}/`);
  return (
    <Link
      href={href}
      aria-current={active ? "page" : undefined}
      className={cx(
        "rounded-[10px] px-3.5 py-3 text-[15px] no-underline",
        block && "block",
        active ? "bg-clay-soft font-semibold !text-clay-text" : "font-medium !text-ink hover:bg-sunken",
      )}>
      {children}
    </Link>
  );
}

/** Site header. `variant` matches the canvas: full nav, a single action, or nothing. */
export function Header({ variant = "nav", action }: { variant?: "nav" | "action"; action?: ReactNode }) {
  return (
    <header className="border-b border-line">
      <div className="relative mx-auto flex max-w-[1200px] items-center justify-between gap-3 px-4 py-3 sm:px-6 md:py-4">
        <Link href="/" className="font-mono text-[22px] font-semibold !text-accent no-underline" aria-label="MAX home">max.</Link>
        {variant === "action" ? action : <MainNav />}
      </div>
    </header>
  );
}

const chip = "rounded-[10px] border border-line bg-surface px-3.5 py-2.5 font-mono text-sm";

function MainNav() {
  const status = useSession((s) => s.status);
  const wallets = useWallets();
  const qc = useQueryClient();
  const router = useRouter();
  const path = usePathname();
  const me = useMe();
  // The menu belongs to the page it was opened on, so navigating closes it.
  const [openOn, setOpenOn] = useState<string | null>(null);
  const open = openOn === path;
  const first = wallets.data?.[0];
  const primary = me.data?.primaryMaxId;
  const authed = status === "authenticated";

  const identity = primary ? primary.handle : first ? shortAddress(first.address) : null;
  const signOut = async () => {
    setOpenOn(null);
    await logout(qc);
    router.push("/");
  };
  const connect = (
    <Link href="/connect" className="rounded-xl bg-clay px-[18px] py-3 text-[15px] font-semibold !text-white no-underline hover:bg-clay-hover">
      Connect wallet
    </Link>
  );

  return (
    <>
      {/* Wide screens: everything inline. */}
      <nav aria-label="Main" className="hidden items-center gap-1 md:flex">
        <NavLink href="/send">Send</NavLink>
        <NavLink href="/market">Market</NavLink>
        <NavLink href="/dashboard">Dashboard</NavLink>
        {authed ? <NavLink href="/agents">Agents</NavLink> : null}
        {authed ? (
          <>
            {identity ? <span className={cx("ml-2", chip)} title={first?.address}>{identity}</span> : null}
            <button type="button" onClick={signOut}
              className="ml-1 rounded-[10px] border border-line bg-surface px-3.5 py-3 text-[15px] font-medium text-ink hover:bg-sunken">
              Sign out
            </button>
          </>
        ) : status === "anonymous" ? <span className="ml-2">{connect}</span> : null}
      </nav>

      {/* Phones: one action plus a menu. */}
      <div className="flex items-center gap-2 md:hidden">
        {authed ? (identity ? <span className={cx(chip, "max-w-[150px] truncate px-3 py-2")} title={first?.address}>{identity}</span> : null)
          : status === "anonymous" ? connect : null}
        <button type="button" aria-label={open ? "Close menu" : "Open menu"} aria-expanded={open} aria-controls="mobile-menu"
          onClick={() => setOpenOn(open ? null : path)}
          className="flex h-11 w-11 items-center justify-center rounded-[10px] border border-line bg-surface text-ink hover:bg-sunken">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
            {open ? <path d="M6 6l12 12M18 6L6 18" /> : <path d="M4 7h16M4 12h16M4 17h16" />}
          </svg>
        </button>
      </div>
      {open ? (
        <nav id="mobile-menu" aria-label="Main"
          className="absolute inset-x-0 top-full z-40 flex flex-col gap-1 border-b border-line bg-ivory px-4 pb-4 pt-2 shadow-[var(--shadow-pop)] md:hidden">
          <NavLink href="/send" block>Send</NavLink>
          <NavLink href="/market" block>Market</NavLink>
          <NavLink href="/dashboard" block>Dashboard</NavLink>
          {authed ? <NavLink href="/agents" block>Agents</NavLink> : null}
          <NavLink href="/app" block>Mobile app</NavLink>
          {authed ? (
            <button type="button" onClick={signOut}
              className="mt-2 rounded-[10px] border border-line bg-surface px-3.5 py-3 text-left text-[15px] font-medium text-ink hover:bg-sunken">
              Sign out
            </button>
          ) : null}
        </nav>
      ) : null}
    </>
  );
}
