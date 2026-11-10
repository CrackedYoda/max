"use client";

import { isMaxApiError, normalizeName, type Chain, type Payment, type Resolution } from "@max/sdk";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";

import { max } from "@/api/client";
import { Handle } from "@/components/identity/Handle";
import { StatusPill } from "@/components/payments/PaymentRow";
import { Button } from "@/components/ui/Button";
import { Banner, Card, KindBadge, ReviewRow, Skeleton } from "@/components/ui/Blocks";
import { CopyButton } from "@/components/ui/Copy";
import { cx } from "@/components/ui/cx";
import { HandleInput, Segmented } from "@/components/ui/Fields";
import { Icon } from "@/components/ui/Icon";
import { errorCode, errorMessage } from "@/error/messages";
import { useChains, usePayment, useResolve } from "@/hooks/queries";
import { useDebounced } from "@/hooks/useDebounced";
import { signAndSend } from "@/services/transfer";
import { connectEvm, connectPhantom, discoverEvmProviders, WalletError, type ConnectedWallet, type EvmProviderInfo } from "@/services/wallets";
import { useSendDraft } from "@/store/flows";
import { useWallet } from "@/store/wallet";
import { shortAddress } from "@/utils/format";
import { stripPrefix } from "@/validation/identity";
import { amountSchema, tokenAddressError } from "@/validation/payment";

type Phase =
  | { kind: "review" }
  | { kind: "working"; step: "connect" | "prepare" | "sign" | "record" }
  | { kind: "record_failed"; payment: Payment; txHash: string; error: string }
  | { kind: "sent"; paymentId: string };

const DEFAULT_CHAIN = process.env.NEXT_PUBLIC_DEFAULT_CHAIN ?? "base";

export function SendFlow() {
  const params = useSearchParams();
  const chains = useChains();
  const [to, setTo] = useState(stripPrefix(params.get("to") ?? ""));
  const [chainKey, setChainKey] = useState(DEFAULT_CHAIN);
  const [assetMode, setAssetMode] = useState<"native" | "token">("native");
  const [token, setToken] = useState("");
  const [amount, setAmount] = useState("");
  const [phase, setPhase] = useState<Phase>({ kind: "review" });
  const [error, setError] = useState<string | null>(null);
  const [evm, setEvm] = useState<EvmProviderInfo[]>([]);
  const wallet = useWallet((s) => s.wallet);

  useEffect(() => discoverEvmProviders(setEvm), []);

  const chain = chains.data?.find((c) => c.key === chainKey);
  const handle = normalizeName(useDebounced(to));
  const tokenErr = assetMode === "token" && chain ? tokenAddressError(chain.namespace, token) : null;
  const assetParam = assetMode === "token" && !tokenErr ? token.trim() : "native";
  const resolution = useResolve(handle, chainKey, assetMode === "token" && tokenErr ? "" : assetParam);
  const amountOk = amountSchema.safeParse(amount).success;
  const busy = phase.kind === "working";

  // Signing and recording must not be interrupted by an accidental tab close.
  useEffect(() => {
    if (!busy) return;
    const h = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", h);
    return () => window.removeEventListener("beforeunload", h);
  }, [busy]);

  // Editing the form after a send starts a new payment.
  const editing = () => {
    setError(null);
    if (phase.kind === "sent") setPhase({ kind: "review" });
  };

  async function ensureWallet(c: Chain): Promise<ConnectedWallet> {
    if (wallet && wallet.namespace === c.namespace) return wallet;
    setPhase({ kind: "working", step: "connect" });
    let w: ConnectedWallet;
    if (c.namespace === "solana") w = await connectPhantom();
    else {
      const p = evm.find((x) => /metamask/i.test(x.name)) ?? evm[0];
      if (!p) throw new WalletError("No EVM wallet found. Install MetaMask to send on this network.");
      w = await connectEvm(p);
    }
    useWallet.getState().set(w);
    return w;
  }

  async function confirm(r: Resolution) {
    if (!chain) return;
    setError(null);
    try {
      const w = await ensureWallet(chain);
      const draft = useSendDraft.getState();
      // One key per exact set of terms: a retry replays the same intent, never a second one.
      const key = draft.keyFor(JSON.stringify([r.maxId, r.resolutionVersion, chain.key, assetParam, amount.trim(), w.address]));

      setPhase({ kind: "working", step: "prepare" });
      const payment = await max().transfers.prepare({
        recipient: r.handle, chain: chain.key, asset: assetParam, amount: amount.trim(),
        senderAddress: w.address, idempotencyKey: key, resolutionVersion: r.resolutionVersion,
      });
      draft.setPayment(payment);
      if (payment.txHash) {
        setPhase({ kind: "sent", paymentId: payment.id });
        return;
      }

      setPhase({ kind: "working", step: "sign" });
      const txHash = draft.txHash ?? (await signAndSend(payment, w));
      draft.setTxHash(txHash);
      await record(payment, txHash);
    } catch (err) {
      setPhase({ kind: "review" });
      if (errorCode(err) === "RESOLUTION_STALE") resolution.refetch();
      setError(errorMessage(err));
    }
  }

  async function record(payment: Payment, txHash: string) {
    setPhase({ kind: "working", step: "record" });
    try {
      await max().transfers.submit(payment.id, txHash);
      setPhase({ kind: "sent", paymentId: payment.id });
      useSendDraft.getState().reset();
    } catch (err) {
      // The transaction is already broadcast; only MAX's record of it failed.
      setPhase({ kind: "record_failed", payment, txHash, error: errorMessage(err) });
    }
  }

  const chainList = useMemo(() => {
    // MAX can't send on receive-only chains (Bitcoin, Litecoin, Tron).
    const list = (chains.data ?? []).filter((c) => !c.receiveOnly);
    return [...list.filter((c) => !c.testnet), ...list.filter((c) => c.testnet)];
  }, [chains.data]);

  return (
    <main className="mx-auto flex max-w-[1100px] flex-col gap-8 px-6 pb-24 pt-14">
      <h1 className="m-0 font-display text-[44px] font-medium tracking-[-0.01em]">Send</h1>
      <div className="flex flex-wrap items-start gap-6">
        <form aria-label="Payment details" onSubmit={(e) => e.preventDefault()}
          className="flex flex-[1_1_380px] flex-col gap-6 rounded-2xl border border-line bg-surface p-7 shadow-[0_8px_24px_rgba(20,20,19,0.04)]">
          <div className="flex flex-col gap-2.5">
            <label htmlFor="to" className="text-sm font-semibold">To</label>
            <HandleInput id="to" scale="md" value={to} disabled={busy} placeholder="yourfriend"
              onChange={(e) => { editing(); setTo(stripPrefix(e.target.value)); }} />
          </div>

          <div className="flex flex-col gap-2.5">
            <span id="net-label" className="text-sm font-semibold">Network</span>
            <div role="radiogroup" aria-labelledby="net-label" className="flex flex-wrap gap-2">
              {chainList.map((c) => {
                const on = c.key === chainKey;
                return (
                  <button key={c.key} type="button" role="radio" aria-checked={on} disabled={busy}
                    onClick={() => { editing(); setChainKey(c.key); setToken(""); }}
                    className={cx("inline-flex min-h-11 items-center rounded-[10px] px-3.5 text-sm text-ink",
                      on ? "border-2 border-accent bg-clay-soft font-semibold" : "border border-line bg-surface font-medium hover:bg-sunken")}>
                    {c.name}
                    {c.testnet ? <span className="ml-1.5 rounded-full bg-sunken px-1.5 text-[11px] font-semibold text-muted">Testnet</span> : null}
                  </button>
                );
              })}
              {chains.isLoading ? <Skeleton className="h-11 w-full" /> : null}
            </div>
          </div>

          <fieldset className="m-0 flex flex-col gap-2.5 border-0 p-0">
            <legend className="mb-2.5 p-0 text-sm font-semibold">Asset</legend>
            <Segmented label="Asset" value={assetMode} onChange={(v) => { editing(); setAssetMode(v); }}
              options={[{ value: "native", label: `Native (${chain?.nativeCoin ?? "…"})` }, { value: "token", label: "Token by contract" }]} />
            <label htmlFor="token" className="text-[13px] text-muted">Token contract / mint address</label>
            <input id="token" value={token} disabled={assetMode !== "token" || busy}
              onChange={(e) => { editing(); setToken(e.target.value.trim()); }}
              placeholder="Paste the full contract or mint address" autoComplete="off" spellCheck={false}
              aria-invalid={assetMode === "token" && !!token && !!tokenErr}
              className="min-h-12 rounded-xl border border-line bg-surface px-4 font-mono text-sm outline-none focus:border-ink disabled:bg-ivory disabled:text-muted" />
            {assetMode === "token" && token && tokenErr ? <p role="alert" className="m-0 text-[13px] text-danger">{tokenErr}</p> : null}
            <p className="m-0 text-[13px] text-muted">Tokens are never picked by symbol — only by exact address.</p>
          </fieldset>

          <div className="flex flex-col gap-2.5">
            <label htmlFor="amount" className="text-sm font-semibold">Amount</label>
            <div className="field flex items-center gap-2 rounded-xl border border-line px-4 focus-within:border-ink">
              <input id="amount" value={amount} disabled={busy} inputMode="decimal" placeholder="0.00"
                onChange={(e) => { editing(); setAmount(e.target.value.replace(",", ".")); }}
                className="tabular min-h-12 min-w-0 flex-1 border-0 bg-transparent text-xl font-semibold outline-none" />
              <span className="text-[15px] font-semibold text-muted">{assetMode === "native" ? chain?.nativeCoin : "tokens"}</span>
            </div>
            {amount && !amountOk ? <p role="alert" className="m-0 text-[13px] text-danger">Enter an amount like 0.25</p> : null}
          </div>
        </form>

        <section aria-labelledby="confirm" aria-live="polite" className="flex min-w-0 flex-[1_1_420px] flex-col gap-4">
          <ReviewPanel
            handle={handle} chain={chain} resolution={resolution.data} resolving={resolution.isFetching && !resolution.data}
            resolveError={resolution.error} amount={amount} amountOk={amountOk} assetMode={assetMode} tokenOk={!tokenErr}
            phase={phase} error={error} wallet={wallet}
            onConfirm={(r) => confirm(r)}
            onRetryRecord={(p, h) => record(p, h)}
          />
        </section>
      </div>
    </main>
  );
}

function ReviewPanel(props: {
  handle: string | null; chain?: Chain; resolution?: Resolution; resolving: boolean; resolveError: unknown;
  amount: string; amountOk: boolean; assetMode: "native" | "token"; tokenOk: boolean;
  phase: Phase; error: string | null; wallet: ConnectedWallet | null;
  onConfirm(r: Resolution): void; onRetryRecord(p: Payment, h: string): void;
}) {
  const { handle, chain, resolution: r, phase } = props;

  if (phase.kind === "sent") return <SentCard paymentId={phase.paymentId} />;
  if (phase.kind === "record_failed") {
    return (
      <Card className="flex flex-col gap-4 p-7">
        <h2 id="confirm" className="m-0 text-xl font-semibold">Your transaction was sent</h2>
        <Banner tone="warn" title="MAX hasn’t recorded it yet.">
          Don’t send it again. The funds are already on their way; this only links the transaction to the payment so its status can be tracked.
        </Banner>
        <code className="break-all rounded-xl bg-ivory p-3 font-mono text-[13px]">{phase.txHash}</code>
        <p className="m-0 text-sm text-danger">{phase.error}</p>
        <Button onClick={() => props.onRetryRecord(phase.payment, phase.txHash)}>Try recording again</Button>
      </Card>
    );
  }
  if (!handle || !chain) {
    return (
      <Card className="flex flex-col gap-2 p-7">
        <h2 id="confirm" className="m-0 text-[15px] font-semibold text-muted">Confirm payment</h2>
        <p className="m-0 text-[15px] text-muted">Enter a handle and pick a network to see exactly where the money goes.</p>
      </Card>
    );
  }
  if (props.resolving) {
    return (
      <Card className="flex flex-col gap-4 p-7" >
        <Skeleton className="h-6 w-1/2" /><Skeleton className="h-12 w-full" /><Skeleton className="h-20 w-full" />
      </Card>
    );
  }
  if (isMaxApiError(props.resolveError, "NO_COMPATIBLE_WALLET")) {
    return (
      <div role="alert" className="flex flex-col gap-4 rounded-2xl border border-line bg-surface p-7">
        <span aria-hidden="true" className="inline-flex size-10 items-center justify-center rounded-full bg-sunken text-muted"><Icon name="ban" size={20} /></span>
        <h2 id="confirm" className="m-0 text-xl font-semibold"><Handle name={handle} className="[&>span]:text-clay-text" /> has no wallet on {chain.name} yet</h2>
        <p className="m-0 text-[15px] leading-relaxed text-muted">Nothing will be sent. Pick a network they can receive on, or ask them to add a {chain.name} wallet.</p>
        <Link href={`/max.${handle}`} className="text-[15px] font-medium">See where max.{handle} can receive</Link>
      </div>
    );
  }
  if (isMaxApiError(props.resolveError, "MAX_ID_NOT_FOUND")) {
    return (
      <Banner tone="danger" title={`No one has max.${handle}.`}>Check the spelling. MAX never guesses an address.</Banner>
    );
  }
  if (props.resolveError) return <Banner tone="danger">{errorMessage(props.resolveError)}</Banner>;
  if (!r) return null;

  const working = phase.kind === "working" ? phase.step : null;
  const ready = props.amountOk && (props.assetMode === "native" || props.tokenOk);
  const needsWallet = !props.wallet || props.wallet.namespace !== chain.namespace;
  const label = working === "connect" ? "Connecting wallet…" : working === "prepare" ? "Preparing…" : working === "sign" ? "Confirm in your wallet…" : working === "record" ? "Recording…" : needsWallet ? `Connect ${chain.namespace === "solana" ? "Phantom" : "wallet"} and sign` : "Confirm and sign in wallet";
  const assetText = props.assetMode === "native" ? `${props.amount || "0"} ${chain.nativeCoin} (native)` : `${props.amount || "0"} · ${shortAddress(r.asset.address ?? "")}`;

  return (
    <div className="flex flex-col gap-5 rounded-2xl border border-line bg-surface p-7 shadow-[0_8px_24px_rgba(20,20,19,0.04)]">
      <div className="flex flex-col gap-1.5">
        <h2 id="confirm" className="m-0 text-sm font-semibold text-muted">Confirm payment</h2>
        <p className="m-0 flex flex-wrap items-center gap-2.5"><Handle name={r.handle} className="text-[26px]" /><KindBadge type={r.type} /></p>
        {r.displayName && r.displayName !== handle ? <span className="text-sm text-muted">Shown as “{r.displayName}” — check it’s who you mean to pay.</span> : null}
      </div>
      <div className="flex flex-col gap-2">
        <span className="text-[13px] font-semibold text-muted">Resolved address</span>
        <div className="flex items-center gap-2 rounded-xl border border-line bg-ivory py-1 pl-4 pr-1">
          <code className="flex-1 break-all py-2.5 font-mono text-sm">{r.address}</code>
          <CopyButton value={r.address} label="Copy full address" />
        </div>
      </div>
      <dl className="m-0 grid grid-cols-2 gap-x-6 gap-y-4 text-[15px]">
        <ReviewRow label="Network" value={<>{chain.name}{chain.testnet ? <span className="ml-1.5 text-xs font-normal text-muted">Testnet</span> : null}</>} />
        <ReviewRow label="Asset" value={assetText} />
        <ReviewRow label="Resolution version" value={r.resolutionVersion} mono />
        <ReviewRow label="Network fee" value={<span className="font-normal">Set in your wallet</span>} />
      </dl>
      {r.notes.map((n) => (
        <div key={n} role="note" className="rounded-xl bg-clay-soft px-4 py-3.5 text-sm leading-normal text-[#7E3A20]"><strong>Note from resolver:</strong> {n}</div>
      ))}
      {props.error ? <Banner tone="danger">{props.error}</Banner> : null}
      <Button className="min-h-14 rounded-[14px] text-[17px]" loading={!!working} disabled={!ready || !!working} onClick={() => props.onConfirm(r)}>{label}</Button>
      {chain.namespace === "solana" && chain.testnet ? <p className="m-0 text-center text-[13px] text-muted">Set Phantom to Devnet before signing.</p> : null}
      <p className="m-0 text-center text-[13px] text-muted">Check the address. Crypto transfers can’t be reversed.</p>
    </div>
  );
}

function SentCard({ paymentId }: { paymentId: string }) {
  const payment = usePayment(paymentId);
  const p = payment.data;
  if (!p) return <Card className="p-7"><Skeleton className="h-24 w-full" /></Card>;
  const symbol = p.asset.symbol ?? "tokens";
  const confirmed = p.status === "COMPLETED";
  return (
    <div role="status" className="flex flex-col gap-4 rounded-2xl border border-line bg-surface p-7">
      <span aria-hidden="true" className={cx("inline-flex size-11 items-center justify-center rounded-full", confirmed ? "bg-success-soft text-success" : "bg-sunken text-body")}>
        <Icon name="check" size={22} strokeWidth={2.5} />
      </span>
      <h2 id="confirm" className="m-0 font-display text-[30px] font-medium">
        {confirmed ? "Sent to " : "Sending to "}<Handle name={p.recipient.handle} className="text-[26px]" />
      </h2>
      <p className="m-0 text-[15px] text-muted">{p.amount.display} {symbol} on {p.chain.name}</p>
      <div className="flex items-center gap-3">
        <StatusPill status={p.status} />
        {p.status === "CONFIRMING" ? <span className="text-sm text-muted">{p.confirmations}/{p.requiredConfirmations} confirmations</span> : null}
        {p.status === "SUBMITTED" ? <span className="text-sm text-muted">Waiting for the network…</span> : null}
      </div>
      {p.statusReason && (p.status === "FAILED" || p.status === "MANUAL_REVIEW") ? <Banner tone="danger">{p.statusReason}</Banner> : null}
      {p.txHash ? (
        <div className="flex flex-col gap-1.5">
          <span className="text-[13px] font-semibold text-muted">Transaction hash</span>
          <code className="break-all rounded-xl bg-ivory p-3 font-mono text-[13px]">{p.txHash}</code>
        </div>
      ) : null}
      {p.explorerUrl ? <a href={p.explorerUrl} target="_blank" rel="noreferrer" className="text-[15px] font-semibold">View on explorer ↗</a> : null}
    </div>
  );
}
