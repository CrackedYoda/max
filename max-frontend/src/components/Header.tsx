"use client";

import { useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import type { ReactNode } from "react";

import { useWallets } from "@/hooks/queries";
import { logout } from "@/services/session";
import { useSession } from "@/store/session";
import { shortAddress } from "@/utils/format";

import { cx } from "./ui/cx";

function NavLink({ href, children }: { href: string; children: ReactNode }) {
  const path = usePathname();
  const active = path === href || path.startsWith(`${href}/`);
  return (
    <Link
      href={href}
      aria-current={active ? "page" : undefined}
      className={cx(
        "rounded-[10px] px-3.5 py-3 text-[15px] no-underline",
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
      <div className="mx-auto flex max-w-[1200px] flex-wrap items-center justify-between gap-3 px-6 py-4">
        <Link href="/" className="font-mono text-[22px] font-semibold !text-accent no-underline" aria-label="MAX home">max.</Link>
        {variant === "action" ? action : <MainNav />}
      </div>
    </header>
  );
}

function MainNav() {
  const status = useSession((s) => s.status);
  const wallets = useWallets();
  const qc = useQueryClient();
  const router = useRouter();
  const first = wallets.data?.[0];
  return (
    <nav aria-label="Main" className="flex flex-wrap items-center gap-1">
      <NavLink href="/send">Send</NavLink>
      <NavLink href="/market">Market</NavLink>
      <NavLink href="/dashboard">Dashboard</NavLink>
      {status === "authenticated" ? <NavLink href="/agents">Agents</NavLink> : null}
      {status === "authenticated" ? (
        <>
          {first ? (
            <span className="ml-2 rounded-[10px] border border-line bg-surface px-3.5 py-2.5 font-mono text-sm" title={first.address}>
              {shortAddress(first.address)}
            </span>
          ) : null}
          <button
            type="button"
            onClick={async () => {
              await logout(qc);
              router.push("/");
            }}
            className="ml-1 rounded-[10px] border border-line bg-surface px-3.5 py-3 text-[15px] font-medium text-ink hover:bg-sunken">
            Sign out
          </button>
        </>
      ) : status === "anonymous" ? (
        <Link href="/connect" className="ml-2 rounded-xl bg-clay px-[18px] py-3 text-[15px] font-semibold !text-white no-underline hover:bg-clay-hover">
          Connect wallet
        </Link>
      ) : null}
    </nav>
  );
}
