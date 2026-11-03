import { Redirect } from 'expo-router';
import { View } from 'react-native';

import { useSession } from '@/store/session';
import { colors } from '@/theme';

/** Anchor route: sends you to onboarding or the wallet; blank under the lock screen. */
export default function Index() {
  const vault = useSession((s) => s.vault);
  if (vault === 'none') return <Redirect href="/welcome" />;
  if (vault === 'unlocked') return <Redirect href="/home" />;
  return <View style={{ flex: 1, backgroundColor: colors.ivory }} />;
}
