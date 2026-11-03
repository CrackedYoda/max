import { Redirect, router } from 'expo-router';
import { useState } from 'react';

import { Heading } from '@/components/ui/Blocks';
import { Button } from '@/components/ui/Button';
import { CheckRow } from '@/components/ui/Fields';
import { Screen, StepHeader } from '@/components/ui/Screen';
import { PhraseGrid, PhraseWarnings } from '@/components/wallet/PhraseGrid';
import { useSecureScreen } from '@/hooks/useAppLock';
import { useOnboarding } from '@/store/flows';

export default function Backup() {
  useSecureScreen('backup');
  const phrase = useOnboarding((s) => s.phrase);
  const [shown, setShown] = useState(false);
  const [saved, setSaved] = useState(false);
  if (!phrase) return <Redirect href="/welcome" />;

  return (
    <Screen
      scroll
      header={<StepHeader step={1} total={3} onBack={() => router.back()} />}
      footer={
        <>
          <CheckRow checked={saved} onChange={setSaved}>
            I’ve written down my recovery phrase and stored it safely.
          </CheckRow>
          <Button label="Continue" disabled={!saved || !shown} onPress={() => router.push('/verify')} />
        </>
      }>
      <Heading title="Your recovery phrase" lead="These 12 words are the only way to restore this wallet. Write them down in order and keep them offline." />
      <PhraseGrid phrase={phrase} hidden={!shown} onReveal={() => setShown(true)} />
      <PhraseWarnings />
    </Screen>
  );
}
