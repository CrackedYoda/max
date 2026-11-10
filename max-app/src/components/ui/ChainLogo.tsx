import { SvgXml } from 'react-native-svg';

import { chainLogoSvg } from './chain-logos';

/** A chain's logo, by chain key ('bnb') or namespace ('evm'). Renders nothing for unknown keys. */
export function ChainLogo({ chain, size = 18 }: { chain: string; size?: number }) {
  const xml = chainLogoSvg(chain);
  if (!xml) return null;
  return <SvgXml xml={xml} width={size} height={size} accessibilityElementsHidden importantForAccessibility="no" />;
}
