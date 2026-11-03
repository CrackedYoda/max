"use client";

import { normalizeName, type AgentDetail } from "@max/sdk";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { max } from "@/api/client";
import { keys } from "@/api/keys";
import { Button } from "@/components/ui/Button";
import { Banner, Card, SectionHeader } from "@/components/ui/Blocks";
import { Segmented } from "@/components/ui/Fields";
import { errorMessage } from "@/error/messages";
import { useAccountId, useChains } from "@/hooks/queries";
import { toast } from "@/store/flows";
import { formatDate, shortAddress } from "@/utils/format";
import { stripPrefix } from "@/validation/identity";
import { tokenAddressError } from "@/validation/payment";

type Row = { key: number; chain: string; asset: string; maxPerTransfer: string; maxPer24h: string };

const amountOk = (v: string) => /^\d*\.?\d+$/.test(v.trim()) && Number(v) > 0;

/**
 * Spending policy editor. Limits are per chain + asset; anything without a
 * limit, or over one, waits for the owner's approval. Server re-validates all of it.
 */
export function PolicySection({ agent }: { agent: AgentDetail }) {
  const qc = useQueryClient();
  const accountId = useAccountId();
  const chains = useChains();
  const p = agent.policy;
  const [rows, setRows] = useState<Row[]>(() =>
    p.limits.map((l, i) => ({ key: i, chain: l.chain, asset: l.asset || "native", maxPerTransfer: l.maxPerTransfer, maxPer24h: l.maxPer24h })),
  );
  const [counter, setCounter] = useState(rows.length);
  const [anyone, setAnyone] = useState<"any" | "list">(p.allowAnyRecipient ? "any" : "list");
  const [recipients, setRecipients] = useState<string[]>(p.allowedRecipients);
  const [draft, setDraft] = useState("");

  const save = useMutation({
    mutationFn: () =>
      max().agents.setPolicy(agent.id, {
        limits: rows.map((r) => ({ chain: r.chain, asset: r.asset === "native" ? "native" : r.asset.trim(), maxPerTransfer: r.maxPerTransfer.trim(), maxPer24h: r.maxPer24h.trim() })),
        allowedRecipients: anyone === "list" ? recipients : [],
        allowAnyRecipient: anyone === "any",
      }),
    onSuccess: (np) => {
      qc.invalidateQueries({ queryKey: keys.agent(accountId, agent.id) });
      toast(`Policy saved (version ${np.version})`);
    },
  });

  const chainOf = (key: string) => chains.data?.find((c) => c.key === key);
  const rowError = (r: Row): string | null => {
    if (!r.chain) return "Pick a network.";
    const c = chainOf(r.chain);
    if (r.asset !== "native" && c) {
      const e = tokenAddressError(c.namespace, r.asset);
      if (e) return e;
    }
    if (!amountOk(r.maxPerTransfer) || !amountOk(r.maxPer24h)) return "Enter positive amounts.";
    if (Number(r.maxPerTransfer) > Number(r.maxPer24h)) return "Per-payment limit can’t be more than the daily limit.";
    return null;
  };
  const dupes = new Set<string>();
  const hasDupe = rows.some((r) => {
    const k = `${r.chain}|${r.asset.trim().toLowerCase()}`;
    if (dupes.has(k)) return true;
    dupes.add(k);
    return false;
  });
  const invalid = rows.some((r) => rowError(r)) || hasDupe;

  const update = (key: number, patch: Partial<Row>) => setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...patch } : r)));
  const addRecipient = () => {
    const n = normalizeName(stripPrefix(draft));
    if (!n) return;
    if (!recipients.includes(`max.${n}`)) setRecipients((rs) => [...rs, `max.${n}`]);
    setDraft("");
  };

  return (
    <section aria-labelledby="policy" className="flex flex-col gap-5">
      <SectionHeader
        id="policy"
        title="Spending policy"
        lead="Payments inside these limits go through automatically. Anything else waits for your approval."
      />
      <Card className="flex flex-col gap-6 p-6">
        <div className="flex flex-col gap-3">
          <div className="flex items-baseline justify-between gap-3">
            <h3 className="m-0 text-base font-semibold">Limits</h3>
            <span className="text-[13px] text-muted">Version {p.version} · updated {formatDate(p.updatedAt)}</span>
          </div>
          {rows.length === 0 ? (
            <p className="m-0 rounded-xl bg-ivory p-4 text-sm text-muted">No limits set: every payment this agent proposes will wait for you.</p>
          ) : (
            <div className="flex flex-col gap-3">
              {rows.map((r) => {
                const c = chainOf(r.chain);
                const symbol = r.asset === "native" ? (c?.nativeCoin ?? "") : "tokens";
                const err = rowError(r);
                return (
                  <fieldset key={r.key} className="m-0 flex flex-col gap-3 rounded-xl border border-line p-4">
                    <legend className="sr-only">Limit for {c?.name ?? "a network"}</legend>
                    <div className="grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(170px,1fr))]">
                      <label className="flex flex-col gap-1.5 text-[13px] font-semibold">
                        Network
                        <select value={r.chain} onChange={(e) => update(r.key, { chain: e.target.value })} className="field min-h-11 rounded-xl border border-line bg-surface px-3 text-[15px] font-normal">
                          <option value="">Choose…</option>
                          {(chains.data ?? []).map((ch) => <option key={ch.key} value={ch.key}>{ch.name}</option>)}
                        </select>
                      </label>
                      <label className="flex flex-col gap-1.5 text-[13px] font-semibold">
                        Asset
                        <select value={r.asset === "native" ? "native" : "token"} onChange={(e) => update(r.key, { asset: e.target.value === "native" ? "native" : "" })} className="field min-h-11 rounded-xl border border-line bg-surface px-3 text-[15px] font-normal">
                          <option value="native">{c?.nativeCoin ?? "Native coin"}</option>
                          <option value="token">Token…</option>
                        </select>
                      </label>
                      <label className="flex flex-col gap-1.5 text-[13px] font-semibold">
                        Max per payment
                        <input inputMode="decimal" value={r.maxPerTransfer} onChange={(e) => update(r.key, { maxPerTransfer: e.target.value })} placeholder={`0.5 ${symbol}`} className="field min-h-11 rounded-xl border border-line bg-surface px-3 text-[15px] font-normal outline-none" />
                      </label>
                      <label className="flex flex-col gap-1.5 text-[13px] font-semibold">
                        Max per 24 hours
                        <input inputMode="decimal" value={r.maxPer24h} onChange={(e) => update(r.key, { maxPer24h: e.target.value })} placeholder={`2 ${symbol}`} className="field min-h-11 rounded-xl border border-line bg-surface px-3 text-[15px] font-normal outline-none" />
                      </label>
                    </div>
                    {r.asset !== "native" ? (
                      <label className="flex flex-col gap-1.5 text-[13px] font-semibold">
                        Token contract / mint
                        <input value={r.asset} onChange={(e) => update(r.key, { asset: e.target.value })} placeholder={c?.namespace === "solana" ? "Mint address" : "0x…"} className="field min-h-11 rounded-xl border border-line bg-surface px-3 font-mono text-sm font-normal outline-none" />
                      </label>
                    ) : null}
                    <div className="flex items-center justify-between gap-3">
                      <span role={err ? "alert" : undefined} className="text-[13px] text-danger">{err ?? ""}</span>
                      <button type="button" onClick={() => setRows((rs) => rs.filter((x) => x.key !== r.key))} className="rounded-lg px-2.5 py-2 text-[13px] font-medium text-danger hover:bg-danger-soft">Remove</button>
                    </div>
                  </fieldset>
                );
              })}
            </div>
          )}
          {hasDupe ? <p role="alert" className="m-0 text-sm text-danger">Each network and asset can only have one limit.</p> : null}
          <Button
            variant="secondary"
            size="sm"
            className="w-fit"
            onClick={() => {
              setRows((rs) => [...rs, { key: counter, chain: "", asset: "native", maxPerTransfer: "", maxPer24h: "" }]);
              setCounter((c) => c + 1);
            }}>
            Add a limit
          </Button>
        </div>

        <div className="flex flex-col gap-3 border-t border-line pt-6">
          <h3 className="m-0 text-base font-semibold">Who it can pay automatically</h3>
          <div className="max-w-[420px]">
            <Segmented label="Recipients" value={anyone} onChange={setAnyone} options={[{ value: "list", label: "Only these handles" }, { value: "any", label: "Anyone" }]} />
          </div>
          {anyone === "any" ? (
            <Banner tone="warn">Any handle, within the limits above. Prompt-injected or mistaken payments would only be capped by the amounts.</Banner>
          ) : (
            <>
              <div className="flex max-w-[520px] gap-2">
                <div className="field flex min-h-12 flex-1 items-center rounded-xl border border-line bg-surface px-4">
                  <span aria-hidden="true" className="font-mono font-semibold text-accent">max.</span>
                  <input
                    aria-label="Add a recipient handle"
                    value={draft}
                    onChange={(e) => setDraft(stripPrefix(e.target.value))}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        e.preventDefault();
                        addRecipient();
                      }
                    }}
                    placeholder="vendor"
                    className="min-w-0 flex-1 border-0 bg-transparent font-mono text-base outline-none"
                  />
                </div>
                <Button variant="secondary" size="sm" disabled={!normalizeName(draft)} onClick={addRecipient}>Add</Button>
              </div>
              {recipients.length ? (
                <ul className="m-0 flex list-none flex-wrap gap-2 p-0">
                  {recipients.map((h) => (
                    <li key={h} className="inline-flex items-center gap-1 rounded-full border border-line bg-ivory py-1 pl-3 pr-1 font-mono text-sm">
                      {h}
                      <button type="button" aria-label={`Remove ${h}`} onClick={() => setRecipients((rs) => rs.filter((x) => x !== h))} className="inline-flex size-7 items-center justify-center rounded-full text-muted hover:bg-sunken">×</button>
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="m-0 text-sm text-muted">No handles listed: every payment will wait for you.</p>
              )}
            </>
          )}
        </div>

        {save.error ? <Banner tone="danger">{errorMessage(save.error)}</Banner> : null}
        <div className="flex items-center gap-3 border-t border-line pt-6">
          <Button disabled={invalid} loading={save.isPending} onClick={() => save.mutate()}>Save policy</Button>
          <span className="text-[13px] text-muted">Applies to new proposals immediately. {agent.wallets[0] ? `Agent pays from ${shortAddress(agent.wallets[0].address)}.` : ""}</span>
        </div>
      </Card>
    </section>
  );
}
