"use client";

import type { Challenge } from "@max/sdk";
import { useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState, useSyncExternalStore } from "react";

import { max } from "@/api/client";
import { Header } from "@/components/Header";
import { Button } from "@/components/ui/Button";
import { Banner, Heading } from "@/components/ui/Blocks";
import { cx } from "@/components/ui/cx";
import { Icon } from "@/components/ui/Icon";
import { errorCode, errorMessage } from "@/error/messages";
import { connectEvm, connectPhantom, discoverEvmProviders, phantomProvider, type ConnectedWallet, type EvmProviderInfo } from "@/services/wallets";
import { toast } from "@/store/flows";
import { useSession } from "@/store/session";
import { useWallet } from "@/store/wallet";
import { shortAddress } from "@/utils/format";

type Step = "connect" | "sign" | "done";

export function ConnectFlow() {
  const params = useSearchParams();
  const router = useRouter();
  const qc = useQueryClient();
  const status = useSession((s) => s.status);
  // Linking adds a wallet to the signed-in account; otherwise this is sign-in.
  const purpose: "login" | "link" = params.get("mode") === "link" && status === "authenticated" ? "link" : "login";
  const next = safeNext(params.get("next"));

  const [evm, setEvm] = useState<EvmProviderInfo[]>([]);
  // Server snapshot is false so hydration matches; the client then reads the injected provider.
  const hasPhantom = useSyncExternalStore(noSubscribe, () => !!phantomProvider(), () => false);
  const [wallet, setWallet] = useState<ConnectedWallet | null>(null);
  const [challenge, setChallenge] = useState<Challenge | null>(null);
  const [busy, setBusy] = useState<"connect" | "sign" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [step, setStep] = useState<Step>("connect");

  useEffect(() => discoverEvmProviders(setEvm), []);

  async function choose(connect: () => Promise<ConnectedWallet>) {
    setError(null);
    setBusy("connect");
    try {
      const w = await connect();
      setWallet(w);
      useWallet.getState().set(w);
      await loadChallenge(w);
      setStep("sign");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  async function loadChallenge(w: ConnectedWallet) {
    setChallenge(await max().auth.challenge({ purpose, namespace: w.namespace, address: w.address }));
  }

  async function sign() {
    if (!wallet || !challenge) return;
    setError(null);
    setBusy("sign");
    try {
      const signature = await wallet.signMessage(challenge.message);
      const res = await max().auth.verify({ nonce: challenge.nonce, signature });
      setStep("done");
      if (purpose === "link") {
        await qc.invalidateQueries({ queryKey: ["account"] });
        toast(`${wallet.namespace === "evm" ? "EVM" : "Solana"} wallet linked`);
        router.push(next ?? "/dashboard");
        return;
      }
      useSession.getState().setAuthenticated(res.account);
      const ids = await max().ids.list().catch(() => []);
      router.push(next ?? (ids.some((i) => !i.isDefault) ? "/dashboard" : "/claim"));
    } catch (err) {
      setError(errorMessage(err));
      // A used or expired nonce can't be retried; fetch a fresh message to sign.
      if (errorCode(err) === "CHALLENGE_INVALID" || errorCode(err) === "SIGNATURE_INVALID") {
        await loadChallenge(wallet).catch(() => {});
      }
    } finally {
      setBusy(null);
    }
  }

  function reset() {
    setWallet(null);
    setChallenge(null);
    setError(null);
    setStep("connect");
  }

  const metamask = evm.find((p) => /metamask/i.test(p.id) || /metamask/i.test(p.name));
  const otherEvm = evm.filter((p) => p !== metamask);

  return (
    <div className="min-h-screen">
      <Header variant="action" action={<Link href={purpose === "link" ? "/dashboard" : "/"} className="rounded-[10px] px-3.5 py-3 text-[15px] font-medium !text-ink no-underline hover:bg-sunken">Cancel</Link>} />
      <main className="mx-auto flex max-w-[1040px] flex-col gap-10 px-6 pb-24 pt-16">
        <Heading
          title={purpose === "link" ? "Link another wallet" : "Sign in with your wallet"}
          lead={purpose === "link"
            ? "Prove you own another wallet so it can receive payments sent to your MAX IDs."
            : "No password, no email. You prove you own a wallet by signing a short message."}
        />

        <Progress step={step} />

        <div className="flex flex-wrap items-start gap-6">
          <section aria-labelledby="choose" className={cx("flex flex-col gap-3", wallet ? "flex-[1_1_300px]" : "w-full max-w-[440px]")}>
            <h2 id="choose" className="mb-1 mt-0 text-[15px] font-semibold text-muted">Choose a wallet</h2>
            <WalletOption
              letter="P" name="Phantom" sub="Solana"
              selected={wallet?.kind === "phantom"} disabled={!!busy}
              unavailable={!hasPhantom ? { label: "Install Phantom", href: "https://phantom.com/download" } : undefined}
              onClick={() => choose(connectPhantom)}
            />
            <WalletOption
              letter="M" name={metamask?.name ?? "MetaMask"} sub="Ethereum and EVM chains"
              selected={wallet?.kind === "evm" && wallet.label === (metamask?.name ?? "MetaMask")} disabled={!!busy}
              unavailable={!metamask ? { label: "Install MetaMask", href: "https://metamask.io/download" } : undefined}
              onClick={() => metamask && choose(() => connectEvm(metamask))}
            />
            {otherEvm.map((p) => (
              <WalletOption key={p.id} letter={p.name[0] ?? "W"} name={p.name} sub="EVM chains"
                selected={wallet?.kind === "evm" && wallet.label === p.name} disabled={!!busy}
                onClick={() => choose(() => connectEvm(p))} />
            ))}
            {wallet ? (
              <div className="flex items-center gap-2.5 rounded-xl bg-sunken px-3.5 py-3 text-sm">
                <span className="text-muted">Wallet</span>
                <span className="font-mono" title={wallet.address}>{shortAddress(wallet.address)}</span>
              </div>
            ) : busy === "connect" ? (
              <p role="status" className="m-0 text-sm text-muted">Waiting for your wallet…</p>
            ) : null}
            {!wallet && error ? <p role="alert" className="m-0 text-sm text-danger">{error}</p> : null}
          </section>

          {wallet ? (
          <section aria-labelledby="sign" className="flex min-w-0 flex-[999_1_460px] flex-col gap-5 rounded-2xl border border-line bg-surface p-7 shadow-[0_8px_24px_rgba(20,20,19,0.04)]">
            <div className="flex flex-col gap-1.5">
              <h2 id="sign" className="m-0 text-xl font-semibold">Review the message</h2>
              <p className="m-0 text-[15px] text-muted">{wallet.label} will ask you to sign exactly this text.</p>
            </div>
            <pre aria-label="Message to sign" className="m-0 min-h-[160px] whitespace-pre-wrap break-all rounded-xl border border-line bg-ivory p-5 font-mono text-[13px] leading-[1.7] text-ink">
              {challenge?.message ?? ""}
            </pre>
            <Banner tone="success" icon="shield" title="This does not authorize any payment.">
              Signing only proves you own this wallet. No funds move and no fees are charged.
            </Banner>
            <div className="flex flex-wrap gap-3">
              <Button onClick={sign} loading={busy === "sign"} disabled={!challenge || !!busy}>Sign message</Button>
              <Button variant="secondary" className="!font-medium" onClick={reset} disabled={!!busy}>Use a different wallet</Button>
            </div>
            {error ? <p role="alert" className="m-0 text-sm text-danger">{error}</p> : null}
          </section>
          ) : null}
        </div>
      </main>
    </div>
  );
}

function WalletOption({ letter, name, sub, selected, disabled, unavailable, onClick }: {
  letter: string; name: string; sub: string; selected: boolean; disabled: boolean;
  unavailable?: { label: string; href: string }; onClick(): void;
}) {
  const body = (
    <>
      <span aria-hidden="true" className="inline-flex size-10 items-center justify-center rounded-[10px] bg-sunken font-semibold text-muted">{letter}</span>
      <span className="flex flex-1 flex-col gap-0.5"><span className="text-base font-semibold text-ink">{name}</span><span className="text-[13px] text-muted">{sub}</span></span>
      {selected ? <span className="text-[13px] font-semibold text-success">Connected</span> : unavailable ? <span className="text-[13px] font-semibold text-clay-text">{unavailable.label}</span> : null}
    </>
  );
  const cls = cx("flex min-h-[72px] items-center gap-3.5 rounded-[14px] bg-surface p-4 text-left no-underline", selected ? "border-2 border-accent" : "border border-line hover:bg-sunken");
  if (unavailable) return <a href={unavailable.href} target="_blank" rel="noreferrer" className={cls}>{body}</a>;
  return <button type="button" aria-pressed={selected} disabled={disabled} onClick={onClick} className={cx(cls, "cursor-pointer disabled:cursor-wait")}>{body}</button>;
}

function Progress({ step }: { step: Step }) {
  const steps: [Step, string][] = [["connect", "Connect"], ["sign", "Sign"], ["done", "Done"]];
  const idx = steps.findIndex(([s]) => s === step);
  return (
    <ol aria-label="Progress" className="m-0 flex list-none flex-wrap gap-3 p-0 text-sm font-semibold">
      {steps.map(([s, label], i) => (
        <li key={s} className="contents">
          {i > 0 ? <span aria-hidden="true" className="h-px w-6 self-center bg-line-strong" /> : null}
          <span aria-current={i === idx ? "step" : undefined} className={cx("flex items-center gap-2", i < idx ? "text-success" : i === idx ? "text-ink" : "text-muted")}>
            <span className={cx("inline-flex size-6 items-center justify-center rounded-full text-xs", i < idx ? "bg-success-soft" : i === idx ? "bg-clay text-white" : "border border-line")}>
              {i < idx ? <Icon name="check" size={14} strokeWidth={2.5} /> : i + 1}
            </span>
            {label}
          </span>
        </li>
      ))}
    </ol>
  );
}

const noSubscribe = () => () => {};

/** Only same-site relative paths, so `?next=` can't redirect off-site. */
function safeNext(v: string | null): string | null {
  return v && v.startsWith("/") && !v.startsWith("//") ? v : null;
}
