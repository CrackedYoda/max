import { chainLogoSvg } from "./chain-logos";
import { cx } from "./cx";

/** A chain's logo, by chain key ("bnb") or namespace ("evm"). Renders nothing for unknown keys. */
export function ChainLogo({ chain, size = 18, className }: { chain: string; size?: number; className?: string }) {
  const svg = chainLogoSvg(chain);
  if (!svg) return null;
  // As an <img> the markup stays inert and gradient ids can't collide between copies.
  return (
    // eslint-disable-next-line @next/next/no-img-element
    <img src={`data:image/svg+xml;utf8,${encodeURIComponent(svg)}`} alt="" aria-hidden="true" width={size} height={size}
      className={cx("inline-block shrink-0", className)} />
  );
}
