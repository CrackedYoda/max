"use client";

import { isMaxApiError, normalizeName } from "@max/sdk";
import { useParams } from "next/navigation";

import { Handle } from "@/components/identity/Handle";
import { MarketPanel } from "@/components/market/MarketPanel";
import { ButtonLink } from "@/components/ui/Button";
import { Banner, KindBadge, Skeleton } from "@/components/ui/Blocks";
import { Icon } from "@/components/ui/Icon";
import { errorMessage } from "@/error/messages";
import { useProfile } from "@/hooks/queries";
import { formatDate } from "@/utils/format";

export function ProfileView() {
  const { handle: raw } = useParams<{ handle: string }>();
  const name = normalizeName(decodeURIComponent(raw ?? ""));
  const profile = useProfile(name ?? "invalid");

  if (!name || isMaxApiError(profile.error, "MAX_ID_NOT_FOUND")) {
    return (
      <main className="mx-auto flex max-w-[640px] flex-col items-center gap-4 px-6 py-24 text-center">
        <h1 className="m-0 font-display text-[40px] font-medium">No one has this handle</h1>
        <p className="m-0 text-muted">{name ? <><Handle name={name} /> isn’t claimed yet.</> : "That isn’t a valid MAX ID."}</p>
        {name ? <ButtonLink href={`/claim?name=${name}`}>Claim max.{name}</ButtonLink> : null}
      </main>
    );
  }
  if (profile.error) {
    return <main className="mx-auto max-w-[640px] px-6 py-24"><p role="alert" className="text-danger">{errorMessage(profile.error)}</p></main>;
  }
  const p = profile.data;
  const reachable = p?.chains.filter((c) => c.reachable) ?? [];
  const unreachable = p?.chains.filter((c) => !c.reachable && !c.chain.testnet) ?? [];
  const sendable = reachable.some(({ chain }) => !chain.receiveOnly);
  const display = p && p.displayName && p.displayName !== p.name ? p.displayName : null;

  return (
    <main className="mx-auto flex max-w-[640px] flex-col items-center gap-6 px-6 pb-24 pt-20 text-center">
      <div aria-hidden="true" className="inline-flex size-24 items-center justify-center rounded-full bg-clay-soft font-display text-[40px] text-clay-text">
        {(display ?? name)[0]?.toUpperCase()}
      </div>
      <div className="flex flex-col items-center gap-2.5">
        {p ? <h1 className="m-0 font-display text-[40px] font-medium">{display ?? `max.${name}`}</h1> : <Skeleton className="h-10 w-48" />}
        <p className="m-0 flex flex-wrap items-center justify-center gap-2.5">
          {display ? <Handle name={name} className="text-[22px]" /> : null}
          {p ? <KindBadge type={p.type} /> : null}
        </p>
        {/* Owner-written text: rendered as plain text only. */}
        {p?.description ? <p className="mx-0 mb-0 mt-1.5 max-w-[480px] whitespace-pre-line text-base leading-relaxed text-muted">{p.description}</p> : null}
      </div>

      {/* The API only sends ownerChangedAt within 30 days of a sale. */}
      {p?.ownerChangedAt ? (
        <div className="w-full text-left">
          <Banner tone="warn" title={`Owner changed on ${formatDate(p.ownerChangedAt)}`}>
            This handle was sold recently. Its wallets and profile were reset when it changed hands, so make sure you’re paying who you think you are.
          </Banner>
        </div>
      ) : null}

      <section aria-labelledby="chains" className="flex w-full flex-col gap-3 rounded-2xl border border-line bg-surface p-6 text-left">
        <h2 id="chains" className="m-0 text-[15px] font-semibold">Can receive on</h2>
        {p ? (
          reachable.length ? (
            <>
            <ul className="m-0 flex list-none flex-wrap gap-2 p-0">
              {reachable.map(({ chain }) => (
                <li key={chain.key} className="inline-flex items-center gap-1.5 rounded-full bg-success-soft px-3 py-1.5 text-[13px] font-semibold text-success">
                  <Icon name="check" size={14} strokeWidth={2.5} />{chain.name}
                  {chain.testnet ? <span className="rounded-full bg-surface px-1.5 text-[11px] text-muted">Testnet</span> : null}
                  {chain.receiveOnly ? <span className="rounded-full bg-surface px-1.5 text-[11px] text-muted">Receive-only</span> : null}
                </li>
              ))}
            </ul>
            {reachable.some(({ chain }) => chain.receiveOnly) ? (
              <p className="m-0 text-[13px] text-muted">Receive-only: send from your own wallet</p>
            ) : null}
            </>
          ) : <p className="m-0 text-sm text-muted">No wallets linked yet, so this handle can’t receive payments.</p>
        ) : <Skeleton className="h-8 w-full" />}
        {unreachable.length ? (
          <>
            <h2 className="mb-0 mt-2 text-[15px] font-semibold text-muted">Not reachable yet</h2>
            <ul className="m-0 flex list-none flex-wrap gap-2 p-0">
              {unreachable.map(({ chain }) => (
                <li key={chain.key} className="inline-flex items-center gap-1.5 rounded-full border border-dashed border-line px-3 py-1.5 text-[13px] font-medium text-muted">
                  {chain.name}
                </li>
              ))}
            </ul>
          </>
        ) : null}
        {p?.settlement ? (
          <p className="m-0 mt-1 text-[13px] text-muted">
            Payments on other networks can be converted to {p.settlement.asset.kind === "native" ? p.settlement.chain.nativeCoin : "a token"} on {p.settlement.chain.name}, with a quote shown before you sign.
          </p>
        ) : null}
      </section>

      <MarketPanel name={name} />

      {p && !sendable && reachable.length ? null : (
        <ButtonLink href={`/send?to=${name}`} className="gap-2">Send to max.{name}<Icon name="arrowRight" size={18} /></ButtonLink>
      )}
      <p className="m-0 text-[13px] text-muted">Addresses are resolved fresh each time you send. MAX never holds funds.</p>
    </main>
  );
}
