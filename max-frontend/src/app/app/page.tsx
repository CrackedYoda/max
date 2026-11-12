import type { Metadata } from "next";
import Image from "next/image";

import { Footer } from "@/components/Footer";
import { Header } from "@/components/Header";
import { Pill } from "@/components/ui/Blocks";
import { ButtonLink } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";

export const metadata: Metadata = {
  title: "MAX · Mobile app",
  description: "A self-custody wallet you can pay by name. Coming soon to TestFlight on iPhone.",
};

// Screens from the MAX app, shown with an example account (max.maya).
const STEPS: [img: string, title: string, body: string][] = [
  ["welcome", "Create a wallet, or bring yours", "Keys are generated and encrypted on your phone. MAX can’t see them or recover them for you. Already have a wallet? Import it instead."],
  ["claim", "Pick your name", "Type the name you want and claim it. From then on, people pay you by typing it instead of an address."],
  ["home", "Every chain on one screen", "Your name sits on top, with balances across Ethereum, Base, Solana and more underneath."],
  ["send", "Send to a name", "Type their name, choose the network and token. They can receive the same token, or BTC, LTC or TRX."],
  ["review", "Check it, then confirm", "Review the amount, network and fee, then confirm with Face ID. Nothing moves until you do."],
  ["activity", "Get paid without lifting a finger", "Payments to your name land straight in your wallet and show up in your activity."],
];

function Shot({ name, alt, priority }: { name: string; alt: string; priority?: boolean }) {
  return (
    <Image src={`/app/${name}.webp`} alt={alt} width={600} height={1252} priority={priority}
      className="h-auto w-full max-w-[280px] drop-shadow-[0_24px_40px_rgba(20,20,19,0.18)]" />
  );
}

export default function MobileAppPage() {
  return (
    <div className="min-h-screen">
      <Header />
      <main>
        <section className="mx-auto grid max-w-[1200px] items-center gap-12 px-6 pb-16 pt-20 [grid-template-columns:repeat(auto-fit,minmax(320px,1fr))]">
          <div className="flex flex-col items-start gap-6">
            <Pill tone="clay">Coming soon to TestFlight</Pill>
            <h1 className="m-0 font-display text-[clamp(40px,6vw,64px)] font-medium leading-[1.05] tracking-[-0.02em]">
              MAX, in your pocket.
            </h1>
            <p className="m-0 max-w-[520px] text-lg leading-relaxed text-muted">
              A self-custody wallet you can pay by name. Claim your name, see every chain in one place, and send to anyone by typing theirs.
            </p>
            <div className="flex flex-wrap items-center gap-3">
              <span aria-disabled="true" className="inline-flex cursor-not-allowed items-center gap-2 rounded-[10px] bg-line-strong px-5 py-3 text-[15px] font-semibold text-white">
                <Icon name="lock" size={16} />TestFlight beta, coming soon
              </span>
              <ButtonLink href="/" variant="secondary">Use the web app</ButtonLink>
            </div>
            <p className="m-0 text-sm text-muted">iPhone first. Your name and wallets work the same on the web and in the app.</p>
          </div>
          <div className="flex justify-center">
            <Shot name="home" alt="MAX app home screen showing max.maya and balances on Ethereum, Base and Solana" priority />
          </div>
        </section>

        <section aria-labelledby="how-app" className="border-t border-line bg-surface">
          <div className="mx-auto flex max-w-[1200px] flex-col gap-12 px-6 py-[88px]">
            <div className="flex max-w-[680px] flex-col gap-4">
              <span className="font-mono text-sm font-semibold text-clay-text">How the app works</span>
              <h2 id="how-app" className="m-0 font-display text-[clamp(32px,4vw,40px)] font-medium leading-[1.1] tracking-[-0.01em]">From a new wallet to your first payment in six screens.</h2>
            </div>
            <ol className="m-0 grid list-none gap-x-8 gap-y-16 p-0 sm:grid-cols-2 lg:grid-cols-3">
              {STEPS.map(([img, title, body], i) => (
                <li key={img} className="flex flex-col items-center gap-5">
                  <Shot name={img} alt={`MAX app: ${title}`} />
                  <div className="flex w-full max-w-[300px] flex-col gap-2 text-center">
                    <span className="font-mono text-sm font-semibold text-clay-text">{String(i + 1).padStart(2, "0")}</span>
                    <h3 className="m-0 text-xl font-semibold">{title}</h3>
                    <p className="m-0 text-[15px] leading-relaxed text-muted">{body}</p>
                  </div>
                </li>
              ))}
            </ol>
            <p className="m-0 text-sm text-muted">Screens shown with an example account. Balances and addresses are not real.</p>
          </div>
        </section>

        <section aria-labelledby="testflight">
          <div className="mx-auto flex max-w-[880px] flex-col items-center gap-5 px-6 py-[88px] text-center">
            <h2 id="testflight" className="m-0 font-display text-[clamp(28px,3.5vw,36px)] font-medium tracking-[-0.01em]">The iPhone beta opens on TestFlight soon.</h2>
            <p className="m-0 max-w-[560px] text-lg leading-relaxed text-muted">
              Until then, everything works on the web: claim your name, link your wallets and get paid today.
            </p>
            <ButtonLink href="/claim">Claim your name</ButtonLink>
          </div>
        </section>
      </main>
      <Footer />
    </div>
  );
}
