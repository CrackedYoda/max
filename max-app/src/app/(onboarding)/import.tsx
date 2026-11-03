import { router } from 'expo-router';

import { Screen, TitleHeader } from '@/components/ui/Screen';
import { ImportForm } from '@/components/wallet/ImportForm';
import { useOnboarding } from '@/store/flows';

export default function Import() {
  const setImported = useOnboarding((s) => s.setImported);
  return (
    <Screen scroll header={<TitleHeader title="Import wallet" onBack={() => router.back()} />}>
      <ImportForm
        onSubmit={(r) => {
          setImported(r.kind === 'phrase' ? { kind: 'phrase', phrase: r.phrase } : { kind: 'key', namespace: r.namespace, secret: r.secret });
          router.push('/passcode');
        }}
      />
    </Screen>
  );
}
