"use client";

import { normalizeName } from "@max/sdk";
import Link from "next/link";
import { useState } from "react";

import { Footer } from "@/components/Footer";
import { Header } from "@/components/Header";
import { Handle } from "@/components/identity/Handle";
import { AnyChain, LinkWallets, NoComplexity } from "@/components/landing/Sections";
import { ButtonLink } from "@/components/ui/Button";
import { Pill } from "@/components/ui/Blocks";
import { HandleInput } from "@/components/ui/Fields";
import { errorMessage } from "@/error/messages";
import { useAvailability } from "@/hooks/queries";
import { useDebounced } from "@/hooks/useDebounced";
import { stripPrefix } from "@/validation/identity";

export default function Landing() {
  const [q, setQ] = useState("");
  const debounced = useDebounced(q);
  const name = normalizeName(debounced);
  const avail = useAvailability(debounced);

  let badge: React.ReactNode = null;
  let detail: React.ReactNode = "Handles are 3–32 characters: lowercase letters, numbers and hyphens.";
  if (q !== debounced) {
    // Never show a verdict about a different name than the one in the field.
    badge = q.trim() ? <Pill>Checking…</Pill> : badge;
    detail = q.trim() ? "" : detail;
  } else if (debounced.trim() && !name) {
    badge = <Pill tone="danger">Invalid</Pill>;
    detail = "Use 3–32 lowercase letters, numbers or hyphens, not starting or ending with a hyphen.";
  } else if (name && avail.isFetching && !avail.data) {
    badge = <Pill>Checking…</Pill>;
    detail = "";
  } else if (avail.error) {
    badge = <Pill tone="danger">Unavailable</Pill>;
    detail = errorMessage(avail.error);
  } else if (avail.data) {
    const a = avail.data;
    if (a.available) {
      badge = <Pill tone="success" icon="check">Available</Pill>;
      detail = `${a.handle} is available.`;
    } else if (a.reason === "reserved") {
      badge = <Pill tone="clay">Reserved</Pill>;
      detail = a.xHandle && a.claimPolicy !== "blocked"
        ? <>{a.handle} is reserved for @{a.xHandle} on X. Its owner can claim it by verifying that account. <Link href={`/verify?name=${encodeURIComponent(a.name)}`} className="font-semibold">Is that you? →</Link></>
        : `${a.handle} is reserved and can’t be claimed.`;
    } else {
      badge = <Pill tone="danger">{a.reason === "invalid" ? "Invalid" : "Taken"}</Pill>;
      detail = a.reason === "quarantined"
        ? `${a.handle} was released recently and is temporarily unavailable.`
        : a.reason === "pending_rename" ? `${a.handle} is being claimed right now.`
        : a.reason === "invalid" ? `${a.handle} uses a reserved format. Try another name.`
        : `${a.handle} is already claimed. Try another name.`;
    }
  }

  const claimHref = q === debounced && name && avail.data?.available ? `/claim?name=${encodeURIComponent(name)}` : "/claim";

  return (
    <div className="min-h-screen">
      <Header />
      <main>
        <section className="mx-auto flex max-w-[880px] flex-col items-center gap-6 px-6 pb-16 pt-24 text-center">
          <Pill tone="clay">A payment identity for people and AI agents</Pill>
          <h1 className="m-0 font-display text-[clamp(40px,6vw,68px)] font-medium leading-[1.05] tracking-[-0.02em]">
            Send crypto to a name, not an address.
          </h1>
          <p className="m-0 max-w-[600px] text-lg leading-relaxed text-muted">
            Claim a handle, link the wallets you already own, and get paid by anyone who types it. MAX never holds your funds or keys.
          </p>

          <div className="mt-4 flex w-full max-w-[640px] flex-col gap-3 text-left">
            <label htmlFor="handle-search" className="text-sm font-semibold">Find your handle</label>
            <HandleInput
              id="handle-search"
              scale="xl"
              value={q}
              onChange={(e) => setQ(stripPrefix(e.target.value))}
              placeholder="yourname"
              className="!rounded-2xl py-1 pl-5 pr-2 shadow-[0_1px_2px_rgba(20,20,19,0.04),0_8px_24px_rgba(20,20,19,0.04)]"
              trailing={badge}
            />
            <p aria-live="polite" className="m-0 min-h-[22px] text-sm text-muted">{detail}</p>
            <div className="flex flex-wrap gap-3">
              <ButtonLink href={claimHref}>Claim yours</ButtonLink>
              <ButtonLink href="/max.yoda" variant="secondary" className="!font-medium">See an example profile</ButtonLink>
            </div>
          </div>
        </section>

        <section aria-label="How a handle resolves" className="mx-auto max-w-[880px] px-6 pb-24">
          <div className="grid items-center gap-8 rounded-2xl border border-line bg-surface p-8 [grid-template-columns:repeat(auto-fit,minmax(260px,1fr))]">
            <div className="flex flex-col gap-2">
              <span className="text-[13px] font-semibold uppercase tracking-[0.06em] text-muted">Example</span>
              <Handle name="yoda" className="text-[32px]" />
              <span className="text-[15px] leading-normal text-muted">
                The sender picks a chain. MAX returns the one wallet linked for it, or tells them there isn’t one.
              </span>
            </div>
            <div className="flex flex-col gap-3">
              {[
                ["Ethereum", "0x5aAe…eAed"],
                ["Solana", "7xKX…gAsU"],
              ].map(([chain, addr]) => (
                <div key={chain} className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-line px-4 py-3.5">
                  <span className="text-[15px] font-semibold">{chain}</span>
                  <span className="font-mono text-sm text-muted">{addr}</span>
                </div>
              ))}
              <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-dashed border-line px-4 py-3.5">
                <span className="text-[15px] font-semibold text-muted">Polygon</span>
                <span className="text-sm text-muted">No wallet on this chain</span>
              </div>
            </div>
          </div>
        </section>

        <AnyChain />
        <NoComplexity />
        <LinkWallets />

        <section aria-labelledby="how" className="border-t border-line">
          <div className="mx-auto flex max-w-[1200px] flex-col gap-10 px-6 py-[88px]">
            <h2 id="how" className="m-0 font-display text-[40px] font-medium tracking-[-0.01em]">Three steps. No custody.</h2>
            <ol className="m-0 grid list-none gap-6 p-0 [grid-template-columns:repeat(auto-fit,minmax(240px,1fr))]">
              {[
                ["01", "Connect", "Sign in with Phantom or MetaMask. You sign a message to prove ownership, never a payment."],
                ["02", "Claim", "Pick a handle for yourself or your agent, then choose which wallet receives on each chain."],
                ["03", "Get paid", "Share your handle. Senders sign the transfer; funds land straight in your wallet. You do nothing."],
              ].map(([n, title, body]) => (
                <li key={n} className="flex flex-col gap-3 rounded-2xl border border-line bg-surface p-7">
                  <span className="font-mono text-sm font-semibold text-clay-text">{n}</span>
                  <h3 className="m-0 text-xl font-semibold">{title}</h3>
                  <p className="m-0 text-[15px] leading-relaxed text-muted">{body}</p>
                </li>
              ))}
            </ol>
          </div>
        </section>
      </main>
      <Footer />
    </div>
  );
}
