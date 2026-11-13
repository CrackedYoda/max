import { Handle } from "@/components/identity/Handle";
import { ButtonLink } from "@/components/ui/Button";
import { ChainLogo } from "@/components/ui/ChainLogo";
import { Icon } from "@/components/ui/Icon";

// Landing page sections below the hero. Names and addresses here are examples.

const CHAINS: [key: string, name: string, receiveOnly?: boolean][] = [
  ["ethereum", "Ethereum"], ["base", "Base"], ["arbitrum", "Arbitrum"], ["optimism", "Optimism"],
  ["polygon", "Polygon"], ["bnb", "BNB Chain"], ["avalanche", "Avalanche"], ["solana", "Solana"],
  ["bitcoin", "Bitcoin", true], ["litecoin", "Litecoin", true], ["tron", "Tron", true],
];

function SectionTitle({ id, kicker, title, lead }: { id: string; kicker: string; title: string; lead: string }) {
  return (
    <div className="flex max-w-[680px] flex-col gap-4">
      <span className="font-mono text-sm font-semibold text-clay-text">{kicker}</span>
      <h2 id={id} className="m-0 font-display text-[clamp(32px,4vw,40px)] font-medium leading-[1.1] tracking-[-0.01em]">{title}</h2>
      <p className="m-0 text-lg leading-relaxed text-muted">{lead}</p>
    </div>
  );
}

function FlowEnd({ label, chain, asset, network }: { label: string; chain: string; asset: string; network: string }) {
  return (
    <div className="flex flex-col gap-3 rounded-xl border border-line bg-ivory p-5">
      <span className="text-[13px] font-semibold uppercase tracking-[0.06em] text-muted">{label}</span>
      <div className="flex items-center gap-3">
        <ChainLogo chain={chain} size={32} />
        <div className="flex flex-col">
          <span className="text-2xl font-semibold">{asset}</span>
          <span className="text-sm text-muted">on {network}</span>
        </div>
      </div>
    </div>
  );
}

function FlowArrow() {
  return (
    <span aria-hidden="true" className="flex items-center justify-center text-clay-text">
      <Icon name="arrowRight" size={22} className="rotate-90 md:rotate-0" />
    </span>
  );
}

/** Any chain in, any chain out: the sender pays with what they hold, the recipient gets what they chose. */
export function AnyChain() {
  return (
    <section aria-labelledby="any-chain" className="border-t border-line bg-surface">
      <div className="mx-auto flex max-w-[1200px] flex-col gap-10 px-6 py-[88px]">
        <SectionTitle id="any-chain" kicker="Any chain in. Any chain out."
          title="Pay anyone, on any chain, with just their name."
          lead="You pay with whatever you hold. They receive on the chain they picked. MAX finds the route, so neither of you has to think about bridges, swaps or networks." />

        <div className="grid items-center gap-4 md:[grid-template-columns:1fr_auto_1.1fr_auto_1fr]">
          <FlowEnd label="You send" chain="solana" asset="USDC" network="Solana" />
          <FlowArrow />
          <div className="flex flex-col items-center gap-2 rounded-xl border border-clay-soft bg-clay-soft/40 px-5 py-6 text-center">
            <Handle name="alice" className="text-[28px]" />
            <span className="text-sm text-muted">One name. MAX looks up where Alice wants to be paid.</span>
          </div>
          <FlowArrow />
          <FlowEnd label="Alice receives" chain="bitcoin" asset="BTC" network="Bitcoin" />
        </div>

        <p className="m-0 flex max-w-[760px] items-start gap-3 text-[15px] leading-relaxed text-body">
          <Icon name="shield" size={18} className="mt-0.5 shrink-0 text-clay-text" />
          <span>When the chains or tokens differ, the swap settles through NEAR Intents inside the same payment. You sign once. No bridge screens, and nobody holds your money along the way.</span>
        </p>

        <ul aria-label="Supported chains" className="m-0 flex list-none flex-wrap gap-2 p-0">
          {CHAINS.map(([key, name, receiveOnly]) => (
            <li key={key} className="flex items-center gap-2 rounded-full border border-line bg-ivory px-3.5 py-2 text-sm font-medium">
              <ChainLogo chain={key} size={18} />
              {name}
              {receiveOnly ? <span className="text-xs font-normal text-muted">receive</span> : null}
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}

const BEFORE = [
  "Copy a 42-character address and hope you pasted it right",
  "Check you’re on the network they asked for",
  "Bridge or swap first when your chains don’t match",
  "One wrong character and the money is gone",
];
const AFTER = [
  "Type a name you can read and say out loud",
  "MAX picks their wallet for the chain you’re paying on",
  "Different chain or token? Handled in the same payment",
  "You see who you’re paying before you sign",
];

/** Before/after: what MAX takes away. */
export function NoComplexity() {
  return (
    <section aria-labelledby="no-complexity">
      <div className="mx-auto flex max-w-[1200px] flex-col gap-10 px-6 py-[88px]">
        <SectionTitle id="no-complexity" kicker="Less to get wrong"
          title="We’re taking the complexity out of crypto."
          lead="Sending money onchain still asks people to act like engineers. MAX hides the parts that cause mistakes and keeps the part that matters: you stay in control of your own wallet." />
        <div className="grid gap-6 [grid-template-columns:repeat(auto-fit,minmax(280px,1fr))]">
          <div className="flex flex-col gap-5 rounded-2xl border border-line bg-surface p-7">
            <h3 className="m-0 text-lg font-semibold text-muted">Sending crypto today</h3>
            <ul className="m-0 flex list-none flex-col gap-4 p-0">
              {BEFORE.map((t) => (
                <li key={t} className="flex items-start gap-3 text-[15px] leading-relaxed text-muted">
                  <Icon name="x" size={18} className="mt-0.5 shrink-0 text-danger" />{t}
                </li>
              ))}
            </ul>
          </div>
          <div className="flex flex-col gap-5 rounded-2xl border border-clay-soft bg-surface p-7 shadow-[var(--shadow-card)]">
            <h3 className="m-0 text-lg font-semibold">Sending with <span className="font-mono text-clay-text">max.</span></h3>
            <ul className="m-0 flex list-none flex-col gap-4 p-0">
              {AFTER.map((t) => (
                <li key={t} className="flex items-start gap-3 text-[15px] leading-relaxed text-body">
                  <Icon name="check" size={18} className="mt-0.5 shrink-0 text-success-strong" />{t}
                </li>
              ))}
            </ul>
          </div>
        </div>
      </div>
    </section>
  );
}

const LINKED: [chain: string, label: string, addr: string, via: React.ReactNode][] = [
  ["evm", "EVM chains", "0x6b1d…93fa", "EVM wallet"],
  ["solana", "Solana", "4Hq8…tLm2", "Solana wallet"],
  ["bitcoin", "Bitcoin", "bc1q…7x2k",
    <span key="u" title="Pasted, not proven by a wallet signature." className="rounded-full border border-line-strong bg-sunken px-2 py-[2px] text-xs font-semibold text-body">Unverified</span>],
];

/** Setup is linking wallets you already have. */
export function LinkWallets() {
  return (
    <section aria-labelledby="link-wallets" className="border-t border-line bg-surface">
      <div className="mx-auto grid max-w-[1200px] items-center gap-12 px-6 py-[88px] [grid-template-columns:repeat(auto-fit,minmax(320px,1fr))]">
        <div className="flex flex-col gap-6">
          <SectionTitle id="link-wallets" kicker="Setup takes a minute"
            title="Just link your wallets."
            lead="No new wallet, no deposits, no seed phrase to hand over. Sign in with the wallets you already use and your name points to them." />
          <ul className="m-0 flex list-none flex-col gap-3 p-0 text-[15px] leading-relaxed text-body">
            <li className="flex items-start gap-3"><Icon name="check" size={18} className="mt-0.5 shrink-0 text-clay-text" />Connect any EVM-compatible or Solana wallet. Signing in is a message, never a payment.</li>
            <li className="flex items-start gap-3"><Icon name="check" size={18} className="mt-0.5 shrink-0 text-clay-text" />Choose which wallet receives on each chain, and change it any time.</li>
            <li className="flex items-start gap-3"><Icon name="check" size={18} className="mt-0.5 shrink-0 text-clay-text" />Paste a receive-only address for chains you don’t sign in with, like Bitcoin.</li>
          </ul>
          <div><ButtonLink href="/connect">Connect a wallet</ButtonLink></div>
        </div>

        <div className="flex flex-col gap-4 rounded-2xl border border-line bg-ivory p-7 shadow-[var(--shadow-card)]">
          <div className="flex items-baseline justify-between gap-3">
            <Handle name="alice" className="text-[26px]" />
            <span className="text-[13px] font-semibold uppercase tracking-[0.06em] text-muted">Receives on</span>
          </div>
          {LINKED.map(([chain, label, addr, via]) => (
            <div key={chain} className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-line bg-surface px-4 py-3.5">
              <span className="flex items-center gap-2.5 text-[15px] font-semibold"><ChainLogo chain={chain} size={20} />{label}</span>
              <span className="flex items-center gap-3">
                <span className="font-mono text-sm text-muted">{addr}</span>
                {typeof via === "string" ? <span className="text-sm text-muted">{via}</span> : via}
              </span>
            </div>
          ))}
          <span className="text-[13px] text-muted">Example account. Your keys never leave your wallet.</span>
        </div>
      </div>
    </section>
  );
}
