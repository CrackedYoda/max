"use client";

import { normalizeName, type AgentDetail, type AgentTransfer } from "@max/sdk";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";

import { max } from "@/api/client";
import { keys } from "@/api/keys";
import { Handle } from "@/components/identity/Handle";
import { StatusPill } from "@/components/payments/PaymentRow";
import { Button } from "@/components/ui/Button";
import { Banner, Card, Pill, SectionHeader, Skeleton } from "@/components/ui/Blocks";
import { HandleInput, Segmented } from "@/components/ui/Fields";
import { Icon } from "@/components/ui/Icon";
import { errorMessage } from "@/error/messages";
import { useAccountId, useAgent, useAgentTransfers, useAvailability } from "@/hooks/queries";
import { useDebounced } from "@/hooks/useDebounced";
import { useNow } from "@/hooks/useNow";
import { useEvmProviders } from "@/hooks/useEvmProviders";
import { connectFor, WalletError } from "@/services/wallets";
import { toast } from "@/store/flows";
import { formatDate, humanizeReason, namespaceLabel, relativeTime, shortAddress, timeUntil } from "@/utils/format";
import { stripPrefix } from "@/validation/identity";

import { KeysSection } from "./KeysSection";
import { PolicySection } from "./PolicySection";

export function AgentDetailView() {
  const { id } = useParams<{ id: string }>();
  const agent = useAgent(id);

  if (agent.isLoading) {
    return (
      <main className="mx-auto flex max-w-[1100px] flex-col gap-6 px-6 pb-24 pt-14">
        <Skeleton className="h-12 w-72" />
        <Skeleton className="h-40 rounded-2xl" />
      </main>
    );
  }
  if (agent.error || !agent.data) {
    return (
      <main className="mx-auto flex max-w-[720px] flex-col gap-4 px-6 pb-24 pt-14">
        <Banner tone="danger">{errorMessage(agent.error)}</Banner>
        <Link href="/agents">Back to agents</Link>
      </main>
    );
  }
  const a = agent.data;
  return (
    <main className="mx-auto flex max-w-[1100px] flex-col gap-14 px-6 pb-24 pt-14">
      <Overview agent={a} />
      <Approvals agent={a} />
      <IdsSection agent={a} />
      <WalletsSection agent={a} />
      <PolicySection agent={a} />
      <KeysSection agent={a} />
      <ActivitySection agent={a} />
    </main>
  );
}

function useInvalidateAgent(id: string) {
  const qc = useQueryClient();
  const accountId = useAccountId();
  return () => {
    qc.invalidateQueries({ queryKey: keys.agent(accountId, id) });
    qc.invalidateQueries({ queryKey: keys.agents(accountId) });
  };
}

// ---- overview ----

function Overview({ agent }: { agent: AgentDetail }) {
  const invalidate = useInvalidateAgent(agent.id);
  const toggle = useMutation({
    mutationFn: () => (agent.status === "active" ? max().agents.pause(agent.id) : max().agents.resume(agent.id)),
    onSuccess: (a) => {
      invalidate();
      toast(a.status === "active" ? `${a.name} resumed` : `${a.name} paused`);
    },
  });
  return (
    <section className="flex flex-col gap-4">
      <Link href="/agents" className="inline-flex w-fit items-center gap-1 text-sm font-medium">
        <span aria-hidden="true">←</span> Agents
      </Link>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex max-w-[680px] flex-col gap-2">
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="m-0 font-display text-[40px] font-medium tracking-[-0.01em]">{agent.name}</h1>
            <Pill tone={agent.status === "active" ? "success" : "neutral"}>{agent.status === "active" ? "Active" : "Paused"}</Pill>
          </div>
          <p className="m-0 text-[15px] text-muted">{agent.description || "No description."}</p>
          <p className="m-0 text-[13px] text-muted">Created {formatDate(agent.createdAt)}</p>
        </div>
        <Button variant={agent.status === "active" ? "secondary" : "primary"} size="sm" className="!rounded-xl" loading={toggle.isPending} onClick={() => toggle.mutate()}>
          {agent.status === "active" ? "Pause agent" : "Resume agent"}
        </Button>
      </div>
      {agent.status === "paused" ? (
        <Banner tone="warn" title="Paused">Its API keys still authenticate, but every payment it proposes is refused until you resume it.</Banner>
      ) : null}
      {toggle.error ? <Banner tone="danger">{errorMessage(toggle.error)}</Banner> : null}
    </section>
  );
}

// ---- approvals ----

function Approvals({ agent }: { agent: AgentDetail }) {
  const pending = useAgentTransfers(agent.id, "pending_approval");
  const list = pending.data ?? [];
  if (!pending.isLoading && !list.length && !pending.error) return null;
  return (
    <section aria-labelledby="approvals" className="flex flex-col gap-5">
      <SectionHeader id="approvals" title="Waiting for you" lead="These went over a limit or to an unlisted recipient. Nothing is sent unless you approve." />
      {pending.error ? <Banner tone="danger">{errorMessage(pending.error)}</Banner> : null}
      <div className="flex flex-col gap-3">
        {list.map((t) => <ApprovalCard key={t.payment.id} agent={agent} t={t} />)}
      </div>
    </section>
  );
}

function ApprovalCard({ agent, t }: { agent: AgentDetail; t: AgentTransfer }) {
  const qc = useQueryClient();
  const accountId = useAccountId();
  const [denying, setDenying] = useState(false);
  const [reason, setReason] = useState("");
  const done = (msg: string) => {
    qc.invalidateQueries({ queryKey: [...keys.agent(accountId, agent.id), "transfers"] });
    toast(msg);
  };
  const approve = useMutation({ mutationFn: () => max().agents.approve(t.payment.id), onSuccess: () => done("Approved. The agent can sign it now.") });
  const deny = useMutation({ mutationFn: () => max().agents.deny(t.payment.id, reason.trim() || undefined), onSuccess: () => done("Denied") });
  const now = useNow(15_000);
  const p = t.payment;
  const symbol = p.asset.symbol ?? (p.asset.kind === "native" ? p.chain.nativeCoin : shortAddress(p.asset.address ?? ""));
  const error = approve.error ?? deny.error;
  const expires = timeUntil(p.expiresAt, now);

  return (
    <Card className="flex flex-col gap-4 p-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <span className="font-display text-[28px] font-medium">{p.amount.display} <span className="text-lg text-muted">{symbol}</span></span>
          <span className="text-[15px]">to <Handle name={p.recipient.handle} /> <span className="text-muted">on {p.chain.name}</span></span>
        </div>
        <span className={`text-[13px] ${expires === "expired" ? "text-danger" : "text-muted"}`}>{expires === "expired" ? "Expired" : `Expires ${expires}`}</span>
      </div>
      <dl className="m-0 grid gap-3 text-sm [grid-template-columns:repeat(auto-fit,minmax(220px,1fr))]">
        <div className="flex flex-col gap-1"><dt className="text-[13px] text-muted">Resolved address</dt><dd className="m-0 break-all font-mono text-[13px]">{p.recipient.address}</dd></div>
        <div className="flex flex-col gap-1"><dt className="text-[13px] text-muted">From</dt><dd className="m-0 font-mono text-[13px]">{shortAddress(p.senderAddress)}</dd></div>
        <div className="flex flex-col gap-1"><dt className="text-[13px] text-muted">Why it’s held</dt><dd className="m-0">
          {t.authorization.reasons.length ? (
            <ul className="m-0 list-disc pl-4">{t.authorization.reasons.map((r) => <li key={r}>{humanizeReason(r, p.asset.decimals, symbol)}</li>)}</ul>
          ) : "Needs approval"}
        </dd></div>
      </dl>
      {t.authorization.instruction ? (
        <div className="rounded-xl border border-dashed border-line-strong bg-ivory p-3.5">
          <p className="m-0 mb-1 text-xs font-semibold uppercase tracking-[0.06em] text-muted">Agent’s note: written by the agent, not verified</p>
          <p className="m-0 whitespace-pre-wrap text-sm">{t.authorization.instruction}</p>
        </div>
      ) : null}
      {denying ? (
        <div className="flex flex-col gap-2">
          <label htmlFor={`deny-${p.id}`} className="text-sm font-semibold">Reason (optional, shared with the agent)</label>
          <input id={`deny-${p.id}`} value={reason} maxLength={200} onChange={(e) => setReason(e.target.value)} className="field min-h-12 rounded-xl border border-line bg-surface px-4 text-base outline-none" />
        </div>
      ) : null}
      {error ? <Banner tone="danger">{errorMessage(error)}</Banner> : null}
      <div className="flex flex-wrap gap-2">
        {denying ? (
          <>
            <Button variant="danger" size="sm" loading={deny.isPending} onClick={() => deny.mutate()}>Deny payment</Button>
            <Button variant="ghost" size="sm" onClick={() => setDenying(false)}>Cancel</Button>
          </>
        ) : (
          <>
            <Button size="sm" loading={approve.isPending} onClick={() => approve.mutate()}>Approve</Button>
            <Button variant="secondary" size="sm" onClick={() => setDenying(true)}>Deny</Button>
          </>
        )}
      </div>
    </Card>
  );
}

// ---- handles ----

function IdsSection({ agent }: { agent: AgentDetail }) {
  const invalidate = useInvalidateAgent(agent.id);
  const [q, setQ] = useState("");
  const debounced = useDebounced(q);
  const name = normalizeName(debounced);
  const avail = useAvailability(debounced);
  const claim = useMutation({
    mutationFn: () => max().agents.claimId(agent.id, { name: name!, displayName: agent.name }),
    onSuccess: (r) => {
      invalidate();
      setQ("");
      toast(`${r.handle} is ${agent.name}’s`);
    },
  });
  const settled = q === debounced;
  const ok = settled && !!name && !!avail.data?.available;
  // The default ID every agent gets doesn't count as a claimed handle.
  const hasClaimed = agent.ids.some((i) => !i.isDefault);
  const msg = !q.trim() ? "3–32 lowercase letters, numbers or hyphens." : !settled || (name && !avail.data) ? "Checking…" : !name ? "Use 3–32 lowercase letters, numbers or hyphens." : avail.data?.available ? `${avail.data.handle} is available.` : `${avail.data?.handle} isn’t available.`;

  return (
    <section aria-labelledby="ids" className="flex flex-col gap-5">
      <SectionHeader id="ids" title="Handles" lead="Anyone can pay this agent by name. Payments land in the agent’s linked wallet." />
      {agent.ids.length ? (
        <ul className="m-0 flex list-none flex-wrap gap-3 p-0">
          {agent.ids.map((i) => (
            <li key={i.id} className="flex items-center gap-2.5 rounded-xl border border-line bg-surface px-4 py-3">
              <Handle name={i.handle} className="text-lg" />
              <Pill tone="clay">Agent</Pill>
              {i.isDefault ? <Pill>Default ID</Pill> : null}
              <Link href={`/${i.handle}`} className="text-sm">Profile</Link>
            </li>
          ))}
        </ul>
      ) : null}
      <Card as="form" className="flex max-w-[640px] flex-col gap-3 p-6">
        <label htmlFor="agent-handle" className="text-sm font-semibold">{hasClaimed ? "Claim another handle" : "Claim a handle for this agent"}</label>
        <HandleInput id="agent-handle" scale="md" value={q} onChange={(e) => setQ(stripPrefix(e.target.value))} placeholder="research-bot" tone={!settled || !q ? "neutral" : ok ? "ok" : "bad"} />
        <p aria-live="polite" className={`m-0 text-sm ${!q || !settled ? "text-muted" : ok ? "text-success" : "text-danger"}`}>{msg}</p>
        {claim.error ? <Banner tone="danger">{errorMessage(claim.error)}</Banner> : null}
        <Button type="submit" size="sm" className="w-fit" disabled={!ok} loading={claim.isPending} onClick={(e) => { e.preventDefault(); claim.mutate(); }}>
          {ok ? `Claim max.${name}` : "Claim"}
        </Button>
      </Card>
    </section>
  );
}

// ---- wallets ----

function WalletsSection({ agent }: { agent: AgentDetail }) {
  const invalidate = useInvalidateAgent(agent.id);
  const evm = useEvmProviders();
  const [ns, setNs] = useState<"evm" | "solana">("solana");
  const link = useMutation({
    mutationFn: async () => {
      const w = await connectFor(ns, evm);
      return max().agents.linkWallet(agent.id, w);
    },
    onSuccess: (w) => {
      invalidate();
      toast(`${namespaceLabel(w.namespace)} wallet linked to ${agent.name}`);
    },
  });
  const err = link.error;

  return (
    <section aria-labelledby="wallets" className="flex flex-col gap-5">
      <SectionHeader id="wallets" title="Agent wallets" lead="The agent signs from these. Use wallets that belong to the agent, not your personal ones." />
      {agent.wallets.length ? (
        <div className="overflow-x-auto rounded-2xl border border-line bg-surface">
          <table className="w-full min-w-[480px] border-collapse text-sm">
            <thead>
              <tr className="text-left text-muted">
                {["Network", "Address", "Linked"].map((h) => <th key={h} scope="col" className="border-b border-line px-5 py-3.5 text-xs font-semibold uppercase tracking-[0.06em]">{h}</th>)}
              </tr>
            </thead>
            <tbody>
              {agent.wallets.map((w, i) => (
                <tr key={w.id}>
                  <td className={`px-5 py-3.5 font-semibold ${i < agent.wallets.length - 1 ? "border-b border-line" : ""}`}>{namespaceLabel(w.namespace)}</td>
                  <td className={`px-5 py-3.5 font-mono ${i < agent.wallets.length - 1 ? "border-b border-line" : ""}`} title={w.address}>{shortAddress(w.address, 6, 6)}</td>
                  <td className={`px-5 py-3.5 text-muted ${i < agent.wallets.length - 1 ? "border-b border-line" : ""}`}>{formatDate(w.verifiedAt)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <Banner tone="info">No wallet yet. Until you link one, payments to this agent’s handle have nowhere to land and it can’t pay.</Banner>
      )}
      <Card className="flex max-w-[640px] flex-col gap-4 p-6">
        <h3 className="m-0 text-base font-semibold">Link a wallet</h3>
        <p className="m-0 text-sm text-muted">Switch your browser wallet to the agent’s account, then sign a message to prove it. Signing never moves funds.</p>
        <div className="max-w-[320px]">
          <Segmented label="Network" value={ns} onChange={setNs} options={[{ value: "solana", label: "Solana (Phantom)" }, { value: "evm", label: "EVM" }]} />
        </div>
        {err ? (
          <Banner tone="danger">
            {err instanceof WalletError ? err.message : errorMessage(err)}
          </Banner>
        ) : null}
        <Button size="sm" className="w-fit" loading={link.isPending} onClick={() => link.mutate()}>Connect and sign</Button>
      </Card>
    </section>
  );
}

// ---- activity ----

const DECISION: Record<string, { label: string; tone: "neutral" | "success" | "danger" | "clay" }> = {
  auto_approved: { label: "Within limits", tone: "success" },
  approved: { label: "Approved by you", tone: "success" },
  pending_approval: { label: "Waiting for you", tone: "clay" },
  denied: { label: "Denied", tone: "danger" },
};

function ActivitySection({ agent }: { agent: AgentDetail }) {
  const all = useAgentTransfers(agent.id, "all");
  return (
    <section aria-labelledby="agent-activity" className="flex flex-col gap-5">
      <SectionHeader id="agent-activity" title="Activity" lead="Every payment this agent proposed, and what your policy decided." />
      <Card className="px-4 py-2">
        {all.isLoading ? (
          <p className="m-0 py-4 text-sm text-muted">Loading…</p>
        ) : all.error ? (
          <p role="alert" className="m-0 py-4 text-sm text-danger">{errorMessage(all.error)}</p>
        ) : all.data?.length ? (
          <ul className="m-0 list-none p-0">
            {all.data.map((t) => {
              const d = DECISION[t.authorization.decision] ?? { label: t.authorization.decision, tone: "neutral" as const };
              const p = t.payment;
              return (
                <li key={p.id} className="flex flex-wrap items-center gap-3 border-b border-sunken px-1 py-3 last:border-b-0">
                  <span aria-hidden="true" className="inline-flex size-9 shrink-0 items-center justify-center rounded-full bg-sunken text-body"><Icon name="send" /></span>
                  <div className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate text-sm font-medium">To <span className="font-mono">{p.recipient.handle}</span></span>
                    <span className="text-xs text-muted">{p.chain.name} · {relativeTime(p.createdAt)}</span>
                  </div>
                  <Pill tone={d.tone} className="!px-2.5 !py-1 !text-xs">{d.label}</Pill>
                  <StatusPill status={p.status} />
                  <span className="w-[130px] text-right text-sm font-semibold">−{p.amount.display} {p.asset.symbol ?? p.chain.nativeCoin}</span>
                </li>
              );
            })}
          </ul>
        ) : (
          <p className="m-0 py-4 text-sm text-muted">Nothing yet. Give the agent an API key with “Propose payments” to get started.</p>
        )}
      </Card>
    </section>
  );
}
