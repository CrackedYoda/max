import { View } from 'react-native';
import { SvgXml } from 'react-native-svg';

import { colors } from '@/theme';

import { AppText } from './AppText';
import { ChainLogo } from './ChainLogo';
import { tokenLogoSvg } from './token-logos';

/**
 * A token's logo by symbol, falling back to its initials. With `chain`, the
 * chain's logo sits on the corner so USDC on Base and USDC on Solana differ.
 * `plain` shows initials only: an unverified token must not borrow a real logo.
 */
export function TokenLogo({ symbol, chain, size = 28, plain }: { symbol: string; chain?: string; size?: number; plain?: boolean }) {
  const xml = plain ? null : tokenLogoSvg(symbol);
  const badge = Math.round(size * 0.42);
  return (
    <View style={{ width: size, height: size }} accessibilityElementsHidden importantForAccessibility="no-hide-descendants">
      {xml ? (
        <SvgXml xml={xml} width={size} height={size} />
      ) : (
        <View style={{ width: size, height: size, borderRadius: size / 2, backgroundColor: colors.sunken, alignItems: 'center', justifyContent: 'center' }}>
          <AppText weight="semibold" size={Math.max(9, Math.round(size * 0.32))} color={colors.muted}>
            {symbol.replace(/^\$/, '').slice(0, 3).toUpperCase()}
          </AppText>
        </View>
      )}
      {chain ? (
        <View style={{ position: 'absolute', right: -5, bottom: -4, backgroundColor: colors.surface, borderRadius: 999, padding: 1, borderWidth: 1, borderColor: colors.line }}>
          <ChainLogo chain={chain} size={badge} />
        </View>
      ) : null}
    </View>
  );
}
