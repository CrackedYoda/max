import Link from "next/link";

/** Site footer for the marketing pages. */
export function Footer() {
  return (
    <footer className="border-t border-line">
      <div className="mx-auto flex max-w-[1200px] flex-wrap items-center justify-between gap-4 px-6 py-8 text-sm text-muted">
        <span><span className="font-mono font-semibold text-clay-text">max.</span> never holds funds or keys. Only the sender signs.</span>
        <nav aria-label="Footer" className="flex flex-wrap items-center gap-5">
          <Link href="/send" className="!text-muted no-underline hover:!text-ink">Send</Link>
          <Link href="/market" className="!text-muted no-underline hover:!text-ink">Market</Link>
          <Link href="/app" className="font-semibold no-underline">Mobile app</Link>
        </nav>
      </div>
    </footer>
  );
}
