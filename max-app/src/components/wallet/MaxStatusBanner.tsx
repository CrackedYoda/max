import { View } from 'react-native';

import { Banner } from '@/components/ui/Blocks';
import { LinkButton } from '@/components/ui/Button';
import { connectMax } from '@/services/session';
import { useSession } from '@/store/session';
import { colors } from '@/theme';

/** Shown when the MAX API is unreachable: the wallet works, names don't. */
export function MaxStatusBanner() {
  const status = useSession((s) => s.max);
  const error = useSession((s) => s.maxError);
  if (status !== 'unavailable') return null;
  return (
    <Banner tone="clay" icon="alert" title="Offline from MAX">
      <View style={{ gap: 2 }}>
        <Banner.Text>{error ?? 'Couldn’t reach MAX.'}</Banner.Text>
        <LinkButton label="Try again" onPress={() => void connectMax()} weight="semibold" color={colors.clayText} size={14} center={false} />
      </View>
    </Banner>
  );
}
