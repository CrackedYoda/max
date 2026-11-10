"use client";

import { normalizeName, type ReservedClaim, type XVerification } from "@max/sdk";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";

import { max } from "@/api/client";
import { keys } from "@/api/keys";
import { Handle } from "@/components/identity/Handle";
import { Button } from "@/components/ui/Button";
import { Banner, Card, Pill, SectionHeader, Skeleton } from "@/components/ui/Blocks";
import { cx } from "@/components/ui/cx";
import { HandleInput } from "@/components/ui/Fields";
import { copyForCode, errorMessage } from "@/error/messages";
import { useAccountId, useAvailability, useReservedClaims, useVerifications } from "@/hooks/queries";
import { useDebounced } from "@/hooks/useDebounced";
import { useNow } from "@/hooks/useNow";
import { toast } from "@/store/flows";
import { formatDate, relativeTime } from "@/utils/format";
import { stripPrefix } from "@/validation/identity";

// Mirrors X_CLAIM_FRESHNESS on the server; the server is authoritative.
const CLAIM_FRESHNESS_MS = 15 * 60_000;
// The name being claimed survives the round trip to X. Not sensitive.
const PENDING_KEY = "max.pendingReservedName";

const CALLBACK_COPY: Record<string, string> = {
  OAUTH_DENIED: "You cancelled on X. Nothing changed.",
  OAUTH_STATE_INVALID: "That X sign-in expired. Start again.",
  OAUTH_SESSION_MISMATCH: "Finish X verification in the same browser you started it in.",
  X_ACCOUNT_LINKED_ELSEWHERE: "That X account is already verified by another MAX account.",
  X_PROVIDER_ERROR: "X didn’t respond. Try again in a moment.",
};

function readPending(): string {
  try {
    return sessionStorage.getItem(PENDING_KEY) ?? "";
  } catch {
    return "";
  }
}

export function VerifyView({ fromCallback = false }: { fromCallback?: boolean }) {
  const params = useSearchParams();
  const router = useRouter();
  const verifications = useVerifications();
  const claims = useReservedClaims();
  const [name, setName] = useState(() => stripPrefix(params.get("name") ?? (fromCallback ? readPending() : "")));

  const result = fromCallback ? params.get("x") : null;
  const renewed = Number(params.get("renewed") ?? 0);
  const active = (verifications.data ?? []).filter((v) => !v.revokedAt);

  return (
    <main className="mx-auto flex max-w-[960px] flex-col gap-12 px-6 pb-24 pt-14">
      <div className="flex max-w-[680px] flex-col gap-2">
        <h1 className="m-0 font-display text-[40px] font-medium tracking-[-0.01em]">Reserved names</h1>
        <p className="m-0 text-[15px] leading-relaxed text-muted">
          Well-known names are held for their owners on X. Prove you control the matching X account, then claim the name. MAX only reads your public profile.
        </p>
      </div>

      {result === "verified" ? (
        <Banner tone="success" title={`Verified @${params.get("username")}`}>
          {renewed > 0 ? `Also renewed ${renewed} reserved ${renewed === 1 ? "name" : "names"}. ` : ""}
          {name ? "Claim your name below now. The verification is valid for 15 minutes." : "You can claim names reserved for this account."}
        </Banner>
      ) : result === "error" ? (
        <Banner tone="danger" title="X verification didn’t finish">
          {CALLBACK_COPY[params.get("code") ?? ""] ?? copyForCode(params.get("code"))}
        </Banner>
      ) : null}

      <ClaimSection name={name} setName={setName} verifications={active} onClaimed={() => router.replace("/verify")} />
      <XAccountsSection loading={verifications.isLoading} error={verifications.error} accounts={verifications.data ?? []} pendingName={name} />
      <ClaimsSection loading={claims.isLoading} error={claims.error} claims={claims.data ?? []} pendingName={name} />
    </main>
  );
}

// ---- start X ----

function useStartX() {
  return useMutation({
    mutationFn: async (pendingName: string) => {
      try {
        if (pendingName) sessionStorage.setItem(PENDING_KEY, pendingName);
        else sessionStorage.removeItem(PENDING_KEY);
      } catch {
        // storage blocked: the user re-types the name after returning
      }
      const { authorizeUrl } = await max().verifications.startX();
      // Full-page redirect in this tab: the callback must land in the same session.
      window.location.assign(authorizeUrl);
    },
  });
}

// ---- claim ----

function ClaimSection({
  name,
  setName,
  verifications,
  onClaimed,
}: {
  name: string;
  setName: (v: string) => void;
  verifications: XVerification[];
  onClaimed: () => void;
}) {
  const qc = useQueryClient();
  const accountId = useAccountId();
  const debounced = useDebounced(name);
  const canonical = normalizeName(debounced);
  const avail = useAvailability(debounced);
  const startX = useStartX();
  const settled = name === debounced;
  const a = settled ? avail.data : undefined;

  const reservedFor = a && !a.available && a.reason === "reserved" ? a.xHandle?.toLowerCase() : undefined;
  const blocked = a?.reason === "reserved" && a.claimPolicy === "blocked";
  const match = reservedFor ? verifications.find((v) => v.username.toLowerCase() === reservedFor) : undefined;
  const now = useNow(15_000);
  const fresh = match ? now - new Date(match.verifiedAt).getTime() < CLAIM_FRESHNESS_MS : false;

  const claim = useMutation({
    mutationFn: () => max().reservedClaims.claim({ name: canonical! }),
    onSuccess: (c) => {
      qc.invalidateQueries({ queryKey: keys.account(accountId) });
      try {
        sessionStorage.removeItem(PENDING_KEY);
      } catch {
        // ignore
      }
      toast(c.status === "pending_review" ? `${c.handle} submitted for review` : `${c.handle} is yours`);
      setName("");
      onClaimed();
    },
  });

  let state: { tone: "muted" | "ok" | "bad" | "clay"; text: string } = { tone: "muted", text: "Enter the reserved name you want." };
  if (name.trim()) {
    if (!settled || (canonical && avail.isFetching && !avail.data)) state = { tone: "muted", text: "Checking…" };
    else if (!canonical) state = { tone: "bad", text: "Use 3–32 lowercase letters, numbers or hyphens." };
    else if (a?.available) state = { tone: "bad", text: `${a.handle} isn’t reserved. Anyone can claim it.` };
    else if (blocked) state = { tone: "bad", text: `${a!.handle} is permanently reserved and can’t be claimed.` };
    else if (reservedFor && match && fresh) state = { tone: "ok", text: `Reserved for @${reservedFor}. You verified that account ${relativeTime(match.verifiedAt)}.` };
    else if (reservedFor && match) state = { tone: "clay", text: `You verified @${reservedFor} ${relativeTime(match.verifiedAt)}. Claims need a verification from the last 15 minutes. Verify again.` };
    else if (reservedFor) state = { tone: "clay", text: `Reserved for @${reservedFor} on X. Verify that account to claim it.` };
    else if (a) state = { tone: "bad", text: `${a.handle} isn’t available.` };
  }

  const ready = !!(reservedFor && match && fresh && !blocked);
  const err = claim.error ?? startX.error;

  return (
    <section aria-labelledby="claim-reserved" className="flex flex-col gap-5">
      <SectionHeader id="claim-reserved" title="Claim a reserved name" />
      <Card as="form" className="flex flex-col gap-5 p-7">
        <div className="flex flex-col gap-2.5">
          <label htmlFor="reserved-name" className="text-sm font-semibold">Name</label>
          <HandleInput id="reserved-name" scale="lg" value={name} onChange={(e) => setName(stripPrefix(e.target.value))} placeholder="yourbrand" tone={state.tone === "ok" ? "ok" : state.tone === "bad" ? "bad" : "neutral"} />
          <p aria-live="polite" className={cx("m-0 min-h-5 text-sm", state.tone === "ok" ? "text-success" : state.tone === "bad" ? "text-danger" : state.tone === "clay" ? "text-clay-text" : "text-muted")}>{state.text}</p>
        </div>
        <p className="m-0 text-[13px] text-muted">
          Personal accounts are claimed instantly. Brand and organisation names are reviewed by MAX first. Keep your X account: names must be re-verified every 180 days.
        </p>
        {err ? <Banner tone="danger">{errorMessage(err)}</Banner> : null}
        <div className="flex flex-wrap gap-2">
          {ready ? (
            <Button type="submit" loading={claim.isPending} onClick={(e) => { e.preventDefault(); claim.mutate(); }}>Claim max.{canonical}</Button>
          ) : (
            <Button
              type="submit"
              disabled={!reservedFor || blocked}
              loading={startX.isPending}
              onClick={(e) => {
                e.preventDefault();
                startX.mutate(canonical ?? "");
              }}>
              <XLogo /> {reservedFor ? `Verify @${reservedFor} on X` : "Verify with X"}
            </Button>
          )}
        </div>
      </Card>
    </section>
  );
}

// ---- verified X accounts ----

function XAccountsSection({ loading, error, accounts, pendingName }: { loading: boolean; error: unknown; accounts: XVerification[]; pendingName: string }) {
  const qc = useQueryClient();
  const accountId = useAccountId();
  const startX = useStartX();
  const [confirm, setConfirm] = useState<string | null>(null);
  const revoke = useMutation({
    mutationFn: (id: string) => max().verifications.revoke(id),
    onSuccess: () => {
      setConfirm(null);
      qc.invalidateQueries({ queryKey: keys.account(accountId) });
      toast("X account removed");
    },
  });

  return (
    <section aria-labelledby="x-accounts" className="flex flex-col gap-5">
      <SectionHeader
        id="x-accounts"
        title="Verified X accounts"
        action={<Button variant="secondary" size="sm" className="!rounded-xl" loading={startX.isPending} onClick={() => startX.mutate(normalizeName(pendingName) ?? "")}><XLogo /> Verify an account</Button>}
      />
      {startX.error ? <Banner tone="danger">{errorMessage(startX.error)}</Banner> : null}
      {error ? <Banner tone="danger">{errorMessage(error)}</Banner> : null}
      {loading ? (
        <Skeleton className="h-20 rounded-2xl" />
      ) : accounts.length ? (
        <Card className="px-5 py-1">
          <ul className="m-0 list-none p-0">
            {accounts.map((v) => (
              <li key={v.id} className="flex flex-wrap items-center gap-3 border-b border-sunken py-3.5 last:border-b-0">
                <span aria-hidden="true" className="inline-flex size-9 items-center justify-center rounded-full bg-ink text-white"><XLogo /></span>
                <div className="flex min-w-0 flex-1 flex-col">
                  <span className="font-semibold">@{v.username} <span className="font-normal text-muted">{v.displayName}</span></span>
                  <span className="text-[13px] text-muted">
                    {v.revokedAt ? `Removed ${formatDate(v.revokedAt)}${v.revokeReason ? ` · ${v.revokeReason}` : ""}` : `Verified ${relativeTime(v.verifiedAt)}`}
                  </span>
                </div>
                {v.revokedAt ? (
                  <Pill tone="neutral">Removed</Pill>
                ) : confirm === v.id ? (
                  <span className="flex flex-wrap items-center gap-2">
                    <span className="text-sm">Names reserved through it will lapse at their next re-verification.</span>
                    <Button variant="danger" size="sm" loading={revoke.isPending} onClick={() => revoke.mutate(v.id)}>Remove</Button>
                    <Button variant="ghost" size="sm" onClick={() => setConfirm(null)}>Cancel</Button>
                  </span>
                ) : (
                  <Button variant="danger" size="sm" className="!font-normal" onClick={() => setConfirm(v.id)}>Remove</Button>
                )}
              </li>
            ))}
          </ul>
        </Card>
      ) : (
        <p className="m-0 text-sm text-muted">No X accounts yet.</p>
      )}
      {revoke.error ? <Banner tone="danger">{errorMessage(revoke.error)}</Banner> : null}
    </section>
  );
}

// ---- claims ----

const CLAIM_STATUS: Record<ReservedClaim["status"], { label: string; tone: "neutral" | "success" | "danger" | "clay" }> = {
  pending_review: { label: "In review", tone: "clay" },
  active: { label: "Active", tone: "success" },
  lapsed: { label: "Re-verify to unlock", tone: "danger" },
  denied: { label: "Denied", tone: "danger" },
  revoked: { label: "Revoked", tone: "danger" },
};

function ClaimsSection({ loading, error, claims, pendingName }: { loading: boolean; error: unknown; claims: ReservedClaim[]; pendingName: string }) {
  const startX = useStartX();
  const now = useNow();
  if (!loading && !claims.length && !error) return null;
  return (
    <section aria-labelledby="my-claims" className="flex flex-col gap-5">
      <SectionHeader id="my-claims" title="Your reserved names" />
      {error ? <Banner tone="danger">{errorMessage(error)}</Banner> : null}
      {loading ? (
        <Skeleton className="h-20 rounded-2xl" />
      ) : (
        <div className="flex flex-col gap-3">
          {claims.map((c) => {
            const s = CLAIM_STATUS[c.status];
            const dueSoon = c.status === "active" && c.verifiedUntil && new Date(c.verifiedUntil).getTime() - now < 14 * 86_400_000;
            return (
              <Card key={c.id} className="flex flex-wrap items-center gap-4 p-5">
                <div className="flex min-w-0 flex-1 flex-col gap-1">
                  <span className="flex flex-wrap items-center gap-2.5">
                    <Handle name={c.handle} className="text-lg" />
                    <Pill tone={s.tone}>{s.label}</Pill>
                  </span>
                  <span className="text-[13px] text-muted">
                    Via @{c.xUsername}
                    {c.status === "active" && c.verifiedUntil ? ` · re-verify by ${formatDate(c.verifiedUntil)}` : ""}
                    {c.status === "pending_review" ? ` · submitted ${relativeTime(c.createdAt)}` : ""}
                  </span>
                  {c.reviewNote ? <span className="text-sm">MAX: {c.reviewNote}</span> : null}
                </div>
                {c.status === "lapsed" || dueSoon ? (
                  <Button size="sm" variant={c.status === "lapsed" ? "primary" : "secondary"} loading={startX.isPending} onClick={() => startX.mutate(pendingName)}>
                    Re-verify @{c.xUsername}
                  </Button>
                ) : c.status === "active" ? (
                  <Link href={`/${c.handle}`} className="text-sm font-medium">View profile</Link>
                ) : null}
              </Card>
            );
          })}
        </div>
      )}
      {startX.error ? <Banner tone="danger">{errorMessage(startX.error)}</Banner> : null}
    </section>
  );
}

function XLogo() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z" />
    </svg>
  );
}
