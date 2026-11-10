"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { normalizeName } from "@max/sdk";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Controller, useForm, useWatch } from "react-hook-form";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Banner, Heading } from "@/components/ui/Blocks";
import { HandleInput, Segmented, TextField } from "@/components/ui/Fields";
import { Icon } from "@/components/ui/Icon";
import { errorCode, errorDetails, errorMessage } from "@/error/messages";
import { useAvailability, useClaimId, useMe } from "@/hooks/queries";
import { useDebounced } from "@/hooks/useDebounced";
import { toast } from "@/store/flows";
import { handleSchema, stripPrefix } from "@/validation/identity";

const schema = z.object({
  name: handleSchema,
  type: z.enum(["human", "agent"]),
  displayName: z.string().trim().max(64).optional(),
});

type Status = { kind: "ok" | "bad" | "soft" | "idle"; msg: string; verifyName?: string };

export function ClaimForm() {
  const params = useSearchParams();
  const router = useRouter();
  const claim = useClaimId();
  const me = useMe();
  const defaultHandle = me.data?.defaultMaxId?.handle;
  const { control, register, handleSubmit, formState } = useForm<z.input<typeof schema>, unknown, z.output<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: { name: stripPrefix(params.get("name") ?? ""), type: "human", displayName: "" },
  });
  const raw = useWatch({ control, name: "name" });
  const name = useDebounced(raw);
  // Until the debounce catches up, any verdict would be about a different name.
  const settled = raw === name;
  const canonical = normalizeName(name);
  const avail = useAvailability(name);

  const gated = errorCode(claim.error) === "INSUFFICIENT_REGISTRATION_BALANCE";
  const status: Status = (() => {
    if (claim.error && !gated) return { kind: "bad", msg: errorMessage(claim.error) };
    if (!settled) return { kind: "idle", msg: raw.trim() ? "Checking…" : "" };
    if (!name.trim()) return { kind: "idle", msg: "3–32 lowercase letters, numbers or hyphens." };
    if (!canonical) return { kind: "bad", msg: "Use 3–32 lowercase letters, numbers or hyphens, not starting or ending with a hyphen." };
    const a = avail.data;
    if (!a) return { kind: "idle", msg: avail.isFetching ? "Checking…" : "" };
    if (a.available) return { kind: "ok", msg: `${a.handle} is available.` };
    if (a.reason === "reserved") {
      return a.claimPolicy === "blocked" || !a.xHandle
        ? { kind: "bad", msg: `${a.handle} is reserved and can’t be claimed.` }
        : { kind: "soft", msg: `Reserved for @${a.xHandle} on X. Verify that account to claim it.`, verifyName: canonical };
    }
    if (a.reason === "quarantined") return { kind: "bad", msg: `${a.handle} was released recently and is temporarily unavailable.` };
    if (a.reason === "invalid") return { kind: "bad", msg: `${a.handle} uses a reserved format. Try another name.` };
    return { kind: "bad", msg: `Someone already claimed ${a.handle}. Try another name.` };
  })();

  const ready = status.kind === "ok";
  const onSubmit = handleSubmit((v) =>
    claim.mutate(
      { name: v.name, type: v.type, displayName: v.displayName || undefined },
      {
        onSuccess: (id) => {
          toast(`${id.handle} is yours`);
          router.push("/dashboard");
        },
      },
    ),
  );

  return (
    <main className="mx-auto flex max-w-[640px] flex-col gap-8 px-6 pb-24 pt-16">
      <Heading title="Claim your MAX ID" lead="This is the name people will type to pay you." />
      {defaultHandle ? <p className="m-0 -mt-4 text-[15px] text-muted">You already have <span className="font-mono font-semibold text-ink">{defaultHandle}</span> for receiving.</p> : null}
      <form onSubmit={onSubmit} noValidate className="flex flex-col gap-7 rounded-2xl border border-line bg-surface p-7 shadow-[0_8px_24px_rgba(20,20,19,0.04)]">
        <div className="flex flex-col gap-2.5">
          <label htmlFor="claim-name" className="text-sm font-semibold">Handle</label>
          <Controller
            control={control}
            name="name"
            render={({ field }) => (
              <HandleInput
                id="claim-name"
                value={field.value}
                onChange={(e) => {
                  claim.reset();
                  field.onChange(stripPrefix(e.target.value));
                }}
                onBlur={field.onBlur}
                aria-describedby="claim-msg"
                tone={status.kind === "ok" ? "ok" : status.kind === "bad" ? "bad" : "neutral"}
              />
            )}
          />
          <StatusLine status={status} />
        </div>

        <fieldset className="m-0 flex flex-col gap-2.5 border-0 p-0">
          <legend className="mb-2.5 p-0 text-sm font-semibold">Who is this for?</legend>
          <Controller
            control={control}
            name="type"
            render={({ field }) => (
              <Segmented label="Who is this for?" value={field.value} onChange={field.onChange}
                options={[{ value: "human", label: "Human" }, { value: "agent", label: "AI agent" }]} />
            )}
          />
          <p className="m-0 text-[13px] text-muted">Agents get an Agent badge on their profile so senders know who they’re paying.</p>
        </fieldset>

        <TextField
          id="claim-display"
          label={<>Display name <span className="font-normal text-muted">(optional)</span></>}
          placeholder="How your name appears on your profile"
          maxLength={64}
          {...register("displayName")}
          error={formState.errors.displayName?.message}
        />

        {gated ? <RegistrationGate details={errorDetails(claim.error)} /> : null}

        <div className="flex flex-wrap items-center gap-3">
          <Button type="submit" disabled={!ready} loading={claim.isPending}>
            {ready && canonical ? `Claim max.${canonical}` : "Claim"}
          </Button>
          <span className="text-[13px] text-muted">You can choose where payments go after claiming.</span>
        </div>
      </form>
    </main>
  );
}

const usd = (v: unknown): string | null =>
  typeof v === "number" && Number.isFinite(v) ? `$${v.toLocaleString("en-US", { minimumFractionDigits: 0, maximumFractionDigits: 2 })}` : null;

/** INSUFFICIENT_REGISTRATION_BALANCE: counted vs required USD, both from the API. */
function RegistrationGate({ details }: { details?: Record<string, unknown> }) {
  const required = usd(details?.requiredUsd);
  const counted = usd(details?.countedUsd);
  return (
    <Banner tone="warn" title="Your linked wallets don’t hold enough to claim yet">
      {required && counted ? <>MAX counted {counted} across your linked wallets; claiming needs {required}. </> : null}
      Hold at least {required ?? "the minimum"} in USDC/USDT/ETH/SOL/POL in a linked wallet, then try again. Nothing is spent or locked.{" "}
      <Link href="/connect?mode=link&next=/claim" className="font-semibold">Link another wallet</Link>
    </Banner>
  );
}

function StatusLine({ status }: { status: Status }) {
  if (status.kind === "ok") {
    return <p id="claim-msg" className="m-0 flex items-center gap-1.5 text-sm font-medium text-success"><Icon name="check" strokeWidth={2.5} />{status.msg}</p>;
  }
  if (status.kind === "bad") {
    return <p id="claim-msg" role="alert" className="m-0 flex items-start gap-1.5 text-sm font-medium leading-normal text-danger"><Icon name="alert" className="mt-0.5 shrink-0" />{status.msg}</p>;
  }
  if (status.kind === "soft") {
    return (
      <p id="claim-msg" className="m-0 self-start rounded-[10px] bg-clay-soft px-3 py-2 text-sm font-medium text-clay-text">
        {status.msg}
        {status.verifyName ? <> <Link href={`/verify?name=${encodeURIComponent(status.verifyName)}`} className="font-semibold">Verify on X →</Link></> : null}
      </p>
    );
  }
  return <p id="claim-msg" aria-live="polite" className="m-0 min-h-5 text-sm text-muted">{status.msg}</p>;
}
