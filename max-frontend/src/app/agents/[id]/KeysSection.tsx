"use client";

import type { AgentCredential, AgentDetail, AgentScope } from "@max/sdk";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { max } from "@/api/client";
import { keys } from "@/api/keys";
import { Button } from "@/components/ui/Button";
import { Banner, Card, Pill, SectionHeader } from "@/components/ui/Blocks";
import { CopyButton } from "@/components/ui/Copy";
import { TextField } from "@/components/ui/Fields";
import { errorMessage } from "@/error/messages";
import { useAccountId } from "@/hooks/queries";
import { useNow } from "@/hooks/useNow";
import { toast } from "@/store/flows";
import { formatDate, relativeTime } from "@/utils/format";

const SCOPES: { value: AgentScope; label: string; hint: string; default: boolean }[] = [
  { value: "payments:read", label: "Read payments", hint: "See its own payments and their status.", default: true },
  { value: "payments:propose", label: "Propose payments", hint: "Ask to pay a handle. Your policy decides.", default: true },
  { value: "authorizations:read", label: "Read authorizations", hint: "Fetch MAX’s signed approval to sign the transaction.", default: true },
  { value: "payments:submit", label: "Submit transactions", hint: "Report the transaction hash after signing.", default: true },
  { value: "wallets:link", label: "Link wallets", hint: "Add receiving wallets by itself.", default: false },
  { value: "ids:write", label: "Edit its profile", hint: "Change its handle’s display name and description.", default: false },
];

export function KeysSection({ agent }: { agent: AgentDetail }) {
  const qc = useQueryClient();
  const accountId = useAccountId();
  const invalidate = () => qc.invalidateQueries({ queryKey: keys.agent(accountId, agent.id) });
  const [issuing, setIssuing] = useState(false);
  const [secret, setSecret] = useState<AgentCredential | null>(null);
  const live = agent.credentials.filter((c) => !c.revokedAt);
  const revoked = agent.credentials.filter((c) => c.revokedAt);

  return (
    <section aria-labelledby="keys" className="flex flex-col gap-5">
      <SectionHeader
        id="keys"
        title="API keys"
        lead="The agent authenticates with a key. A key can only do what its permissions and your policy allow."
        action={!issuing ? <Button variant="secondary" size="sm" className="!rounded-xl" onClick={() => { setSecret(null); setIssuing(true); }}>New key</Button> : null}
      />
      {secret ? <SecretReveal cred={secret} onDone={() => setSecret(null)} /> : null}
      {issuing ? (
        <IssueKey
          agentId={agent.id}
          onIssued={(c) => {
            setIssuing(false);
            setSecret(c);
            invalidate();
          }}
          onCancel={() => setIssuing(false)}
        />
      ) : null}
      {live.length ? (
        <div className="flex flex-col gap-3">
          {live.map((c) => <KeyRow key={c.id} agentId={agent.id} cred={c} onRotated={(n) => { setSecret(n); invalidate(); }} onRevoked={invalidate} />)}
        </div>
      ) : !issuing ? (
        <p className="m-0 text-sm text-muted">No active keys. The agent can’t call MAX until you issue one.</p>
      ) : null}
      {revoked.length ? (
        <details className="text-sm">
          <summary className="cursor-pointer text-muted">{revoked.length} revoked {revoked.length === 1 ? "key" : "keys"}</summary>
          <ul className="m-0 mt-3 flex list-none flex-col gap-1 p-0 text-muted">
            {revoked.map((c) => <li key={c.id}><span className="font-mono">{c.prefix}…</span> {c.name} · revoked {formatDate(c.revokedAt)}</li>)}
          </ul>
        </details>
      ) : null}
    </section>
  );
}

function SecretReveal({ cred, onDone }: { cred: AgentCredential; onDone: () => void }) {
  return (
    <Card className="flex flex-col gap-3 border-clay p-6">
      <h3 className="m-0 text-base font-semibold">Copy “{cred.name || "key"}” now</h3>
      <p className="m-0 text-sm text-muted">This is the only time it’s shown. Store it in your agent’s secret manager. MAX keeps only a hash.</p>
      <div className="flex items-center gap-2 rounded-xl border border-line bg-ivory py-1 pl-4 pr-1">
        <code className="min-w-0 flex-1 break-all py-2 font-mono text-sm">{cred.secret}</code>
        <CopyButton value={cred.secret ?? ""} label="Copy API key" />
      </div>
      <Button variant="secondary" size="sm" className="w-fit" onClick={onDone}>I’ve stored it</Button>
    </Card>
  );
}

function IssueKey({ agentId, onIssued, onCancel }: { agentId: string; onIssued: (c: AgentCredential) => void; onCancel: () => void }) {
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState<AgentScope[]>(SCOPES.filter((s) => s.default).map((s) => s.value));
  const [expiry, setExpiry] = useState("90");
  const issue = useMutation({
    mutationFn: () => max().agents.issueKey(agentId, { name: name.trim() || undefined, scopes, expiresInDays: Number(expiry) }),
    onSuccess: onIssued,
  });
  return (
    <Card as="form" className="flex max-w-[720px] flex-col gap-5 p-6">
      <h3 className="m-0 text-base font-semibold">New API key</h3>
      <TextField label="Name" hint="So you can tell keys apart, e.g. where it’s deployed." placeholder="production" value={name} maxLength={64} onChange={(e) => setName(e.target.value)} />
      <fieldset className="m-0 flex flex-col gap-2 border-0 p-0">
        <legend className="mb-2 text-sm font-semibold">Permissions</legend>
        {SCOPES.map((s) => (
          <label key={s.value} className="flex cursor-pointer items-start gap-3 rounded-xl border border-line p-3 hover:bg-ivory">
            <input
              type="checkbox"
              checked={scopes.includes(s.value)}
              onChange={(e) => setScopes((cur) => (e.target.checked ? [...cur, s.value] : cur.filter((x) => x !== s.value)))}
              className="mt-0.5 size-5 accent-clay"
            />
            <span className="flex flex-col gap-0.5">
              <span className="text-sm font-semibold">{s.label} <span className="font-mono text-xs font-normal text-muted">{s.value}</span></span>
              <span className="text-[13px] text-muted">{s.hint}</span>
            </span>
          </label>
        ))}
      </fieldset>
      <label className="flex max-w-[240px] flex-col gap-1.5 text-sm font-semibold">
        Expires
        <select value={expiry} onChange={(e) => setExpiry(e.target.value)} className="field min-h-11 rounded-xl border border-line bg-surface px-3 text-[15px] font-normal">
          <option value="30">In 30 days</option>
          <option value="90">In 90 days</option>
          <option value="365">In 1 year</option>
          <option value="0">Never</option>
        </select>
      </label>
      {issue.error ? <Banner tone="danger">{errorMessage(issue.error)}</Banner> : null}
      <div className="flex gap-2">
        <Button type="submit" disabled={!scopes.length} loading={issue.isPending} onClick={(e) => { e.preventDefault(); issue.mutate(); }}>Create key</Button>
        <Button type="button" variant="ghost" onClick={onCancel}>Cancel</Button>
      </div>
    </Card>
  );
}

function KeyRow({ agentId, cred, onRotated, onRevoked }: { agentId: string; cred: AgentCredential; onRotated: (c: AgentCredential) => void; onRevoked: () => void }) {
  const [confirm, setConfirm] = useState<"rotate" | "revoke" | null>(null);
  const rotate = useMutation({ mutationFn: () => max().agents.rotateKey(agentId, cred.id), onSuccess: (c) => { setConfirm(null); onRotated(c); } });
  const revoke = useMutation({ mutationFn: () => max().agents.revokeKey(agentId, cred.id), onSuccess: () => { setConfirm(null); toast("Key revoked"); onRevoked(); } });
  const now = useNow();
  const expired = cred.expiresAt ? new Date(cred.expiresAt).getTime() < now : false;
  const err = rotate.error ?? revoke.error;

  return (
    <Card className="flex flex-col gap-3 p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <span className="flex flex-wrap items-center gap-2">
            <span className="font-semibold">{cred.name || "Unnamed key"}</span>
            <span className="font-mono text-sm text-muted">{cred.prefix}…</span>
            {expired ? <Pill tone="danger" className="!px-2 !py-0.5 !text-xs">Expired</Pill> : null}
          </span>
          <span className="text-[13px] text-muted">
            {cred.lastUsedAt ? `Last used ${relativeTime(cred.lastUsedAt)}` : "Never used"} · {cred.expiresAt ? `${expired ? "expired" : "expires"} ${formatDate(cred.expiresAt)}` : "no expiry"}
          </span>
        </div>
        {confirm ? (
          <span className="flex flex-wrap items-center gap-2">
            <span className="text-sm">{confirm === "rotate" ? "The old key stops working now." : "The agent loses access now."}</span>
            <Button
              variant={confirm === "revoke" ? "danger" : "primary"}
              size="sm"
              loading={rotate.isPending || revoke.isPending}
              onClick={() => (confirm === "rotate" ? rotate.mutate() : revoke.mutate())}>
              {confirm === "rotate" ? "Rotate" : "Revoke"}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setConfirm(null)}>Cancel</Button>
          </span>
        ) : (
          <span className="flex gap-1">
            <Button variant="secondary" size="sm" onClick={() => setConfirm("rotate")}>Rotate</Button>
            <Button variant="danger" size="sm" onClick={() => setConfirm("revoke")}>Revoke</Button>
          </span>
        )}
      </div>
      <ul className="m-0 flex list-none flex-wrap gap-1.5 p-0">
        {cred.scopes.map((s) => <li key={s} className="rounded-md bg-sunken px-2 py-0.5 font-mono text-xs text-body">{s}</li>)}
      </ul>
      {err ? <Banner tone="danger">{errorMessage(err)}</Banner> : null}
    </Card>
  );
}
