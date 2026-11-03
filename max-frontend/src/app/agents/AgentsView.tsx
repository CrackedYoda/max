"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

import { max } from "@/api/client";
import { keys } from "@/api/keys";
import { Button } from "@/components/ui/Button";
import { Banner, Card, Pill, Skeleton } from "@/components/ui/Blocks";
import { TextField } from "@/components/ui/Fields";
import { Icon } from "@/components/ui/Icon";
import { errorMessage } from "@/error/messages";
import { useAccountId, useAgents } from "@/hooks/queries";
import { toast } from "@/store/flows";
import { formatDate } from "@/utils/format";

export function AgentsView() {
  const agents = useAgents();
  const [creating, setCreating] = useState(false);

  return (
    <main className="mx-auto flex max-w-[1200px] flex-col gap-8 px-6 pb-24 pt-14">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex max-w-[640px] flex-col gap-1.5">
          <h1 className="m-0 font-display text-[40px] font-medium tracking-[-0.01em]">Agents</h1>
          <p className="m-0 text-[15px] leading-relaxed text-muted">
            Give an AI agent its own handle and an API key. It can propose payments; your limits decide which go through and which wait for you.
          </p>
        </div>
        {!creating ? <Button size="sm" className="!rounded-xl" onClick={() => setCreating(true)}>New agent</Button> : null}
      </div>

      {creating ? <CreateAgent onDone={() => setCreating(false)} /> : null}
      {agents.error ? <Banner tone="danger">{errorMessage(agents.error)}</Banner> : null}

      <div className="grid gap-5 [grid-template-columns:repeat(auto-fill,minmax(320px,1fr))]">
        {agents.isLoading ? (
          <>
            <Skeleton className="h-[148px] rounded-2xl" />
            <Skeleton className="h-[148px] rounded-2xl" />
          </>
        ) : agents.data?.length ? (
          agents.data.map((a) => (
            <Link key={a.id} href={`/agents/${a.id}`} className="group rounded-2xl no-underline !text-ink focus-visible:outline-2">
              <Card className="flex h-full flex-col gap-3 p-6 transition-colors group-hover:border-line-strong">
                <div className="flex items-center justify-between gap-3">
                  <h2 className="m-0 text-lg font-semibold">{a.name}</h2>
                  <Pill tone={a.status === "active" ? "success" : "neutral"}>{a.status === "active" ? "Active" : "Paused"}</Pill>
                </div>
                <p className="m-0 line-clamp-2 min-h-[44px] text-sm leading-relaxed text-muted">{a.description || "No description."}</p>
                <div className="mt-auto flex items-center justify-between text-[13px] text-muted">
                  <span>Created {formatDate(a.createdAt)}</span>
                  <span className="inline-flex items-center gap-1 font-semibold text-clay-text">Manage <Icon name="arrowRight" size={14} /></span>
                </div>
              </Card>
            </Link>
          ))
        ) : !creating ? (
          <Card className="flex flex-col items-start gap-3 p-6">
            <h2 className="m-0 text-lg font-semibold">No agents yet</h2>
            <p className="m-0 text-[15px] text-muted">Create one to give a bot or assistant a payable handle and controlled spending.</p>
            <Button size="sm" onClick={() => setCreating(true)}>Create an agent</Button>
          </Card>
        ) : null}
      </div>
    </main>
  );
}

function CreateAgent({ onDone }: { onDone: () => void }) {
  const qc = useQueryClient();
  const accountId = useAccountId();
  const router = useRouter();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const create = useMutation({
    mutationFn: () => max().agents.create({ name: name.trim(), description: description.trim() || undefined }),
    onSuccess: (a) => {
      qc.invalidateQueries({ queryKey: keys.agents(accountId) });
      toast(`${a.name} created`);
      router.push(`/agents/${a.id}`);
    },
  });

  return (
    <Card as="form" className="flex max-w-[640px] flex-col gap-5 p-6">
      <h2 className="m-0 text-lg font-semibold">New agent</h2>
      <TextField label="Name" placeholder="Research assistant" value={name} maxLength={64} onChange={(e) => setName(e.target.value)} autoFocus />
      <TextField label="Description" hint="Optional. Only you see this." placeholder="Buys datasets and API credits" value={description} maxLength={280} onChange={(e) => setDescription(e.target.value)} />
      {create.error ? <Banner tone="danger">{errorMessage(create.error)}</Banner> : null}
      <div className="flex gap-2">
        <Button type="submit" loading={create.isPending} disabled={!name.trim()} onClick={(e) => { e.preventDefault(); create.mutate(); }}>Create agent</Button>
        <Button type="button" variant="ghost" onClick={onDone}>Cancel</Button>
      </div>
    </Card>
  );
}
