import { ChainLogo } from "./ChainLogo";
import { cx } from "./cx";
import { tokenLogoSvg } from "./token-logos";

/**
 * A token's logo by symbol, falling back to its initials. With `chain`, the
 * chain's logo sits on the corner so "USDC on Base" and "USDC on Solana" differ.
 */
export function TokenLogo({ symbol, chain, size = 28, className }: { symbol: string; chain?: string; size?: number; className?: string }) {
  const svg = tokenLogoSvg(symbol);
  const badge = Math.round(size * 0.42);
  return (
    <span className={cx("relative inline-flex shrink-0", className)} style={{ width: size, height: size }} aria-hidden="true">
      {svg ? (
        // As an <img> the markup stays inert and gradient ids can't collide between copies.
        // eslint-disable-next-line @next/next/no-img-element
        <img src={`data:image/svg+xml;utf8,${encodeURIComponent(svg)}`} alt="" width={size} height={size} />
      ) : (
        <span className="flex size-full items-center justify-center rounded-full bg-sunken font-semibold text-muted"
          style={{ fontSize: Math.max(9, Math.round(size * 0.34)) }}>
          {symbol.replace(/^\$/, "").slice(0, 3).toUpperCase()}
        </span>
      )}
      {chain ? (
        <span className="absolute -bottom-1 -right-1.5 flex rounded-full bg-surface p-px shadow-[0_0_0_1px_var(--line)]">
          <ChainLogo chain={chain} size={badge} />
        </span>
      ) : null}
    </span>
  );
}
