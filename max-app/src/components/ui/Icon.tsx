import Svg, { Circle, Path, Polyline, Rect } from 'react-native-svg';

import { colors } from '@/theme';

export type IconName =
  | 'back'
  | 'close'
  | 'check'
  | 'copy'
  | 'share'
  | 'send'
  | 'receive'
  | 'scan'
  | 'home'
  | 'ids'
  | 'settings'
  | 'shield'
  | 'eye'
  | 'faceId'
  | 'delete'
  | 'chevronRight'
  | 'chevronDown'
  | 'lock'
  | 'alert'
  | 'external';

/** Feather-style 24px stroke icons, matching the canvas. Decorative unless labelled by the parent. */
export function Icon({ name, size = 22, color = colors.ink, strokeWidth = 2 }: { name: IconName; size?: number; color?: string; strokeWidth?: number }) {
  const p = { stroke: color, strokeWidth, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const, fill: 'none' };
  return (
    <Svg width={size} height={size} viewBox="0 0 24 24" accessibilityElementsHidden importantForAccessibility="no">
      {name === 'back' && <Polyline points="15 18 9 12 15 6" {...p} />}
      {name === 'close' && <Path d="M18 6 6 18M6 6l12 12" {...p} />}
      {name === 'check' && <Polyline points="20 6 9 17 4 12" {...p} />}
      {name === 'copy' && (
        <>
          <Rect x={9} y={9} width={13} height={13} rx={2} {...p} />
          <Path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" {...p} />
        </>
      )}
      {name === 'share' && (
        <>
          <Path d="M4 12v8a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-8" {...p} />
          <Polyline points="16 6 12 2 8 6" {...p} />
          <Path d="M12 2v13" {...p} />
        </>
      )}
      {name === 'send' && <Path d="M12 19V5M5 12l7-7 7 7" {...p} />}
      {name === 'receive' && <Path d="M12 5v14M19 12l-7 7-7-7" {...p} />}
      {name === 'scan' && <Path d="M3 7V5a2 2 0 0 1 2-2h2M17 3h2a2 2 0 0 1 2 2v2M21 17v2a2 2 0 0 1-2 2h-2M7 21H5a2 2 0 0 1-2-2v-2M7 12h10" {...p} />}
      {name === 'home' && (
        <>
          <Path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" {...p} />
          <Polyline points="9 22 9 12 15 12 15 22" {...p} />
        </>
      )}
      {name === 'ids' && (
        <>
          <Rect x={2} y={5} width={20} height={14} rx={2} {...p} />
          <Circle cx={8} cy={12} r={2} {...p} />
          <Path d="M13 10h5M13 14h3" {...p} />
        </>
      )}
      {name === 'settings' && (
        <>
          <Circle cx={12} cy={12} r={3} {...p} />
          <Path
            d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"
            {...p}
          />
        </>
      )}
      {name === 'shield' && <Path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" {...p} />}
      {name === 'eye' && (
        <>
          <Path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" {...p} />
          <Circle cx={12} cy={12} r={3} {...p} />
        </>
      )}
      {name === 'faceId' && (
        <Path d="M7 3H5a2 2 0 0 0-2 2v2M17 3h2a2 2 0 0 1 2 2v2M21 17v2a2 2 0 0 1-2 2h-2M7 21H5a2 2 0 0 1-2-2v-2M9 9v1M15 9v1M12 9v4h-1M9 16s1 1 3 1 3-1 3-1" {...p} />
      )}
      {name === 'delete' && (
        <>
          <Path d="M21 4H8l-7 8 7 8h13a2 2 0 0 0 2-2V6a2 2 0 0 0-2-2z" {...p} />
          <Path d="M18 9l-6 6M12 9l6 6" {...p} />
        </>
      )}
      {name === 'chevronRight' && <Polyline points="9 18 15 12 9 6" {...p} />}
      {name === 'chevronDown' && <Polyline points="6 9 12 15 18 9" {...p} />}
      {name === 'lock' && (
        <>
          <Rect x={3} y={11} width={18} height={11} rx={2} {...p} />
          <Path d="M7 11V7a5 5 0 0 1 10 0v4" {...p} />
        </>
      )}
      {name === 'alert' && (
        <>
          <Circle cx={12} cy={12} r={10} {...p} />
          <Path d="M12 8v4M12 16h.01" {...p} />
        </>
      )}
      {name === 'external' && <Path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6M15 3h6v6M10 14L21 3" {...p} />}
    </Svg>
  );
}
