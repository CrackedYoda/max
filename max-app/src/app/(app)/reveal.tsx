import { router } from 'expo-router';
import { useEffect, useState } from 'react';

import { Banner, Heading } from '@/components/ui/Blocks';
import { Button } from '@/components/ui/Button';
import { Screen, TitleHeader } from '@/components/ui/Screen';
import { PhraseGrid, PhraseWarnings } from '@/components/wallet/PhraseGrid';
import { useSecureScreen } from '@/hooks/useAppLock';
import { useStepUpGate } from '@/hooks/useStepUpGate';
import * as vault from '@/services/vault';

export default function Reveal() {
  useSecureScreen('reveal');
  const allowed = useStepUpGate('Confirm it’s you to see your recovery phrase.');
  const [phrase, setPhrase] = useState<string | null>(null);
  const [shown, setShown] = useState(false);
  const [missing, setMissing] = useState(false);

  useEffect(() => {
    if (!allowed) return;
    vault
      .revealPhrase()
      .then((p) => (p ? setPhrase(p) : setMissing(true)))
      .catch(() => setMissing(true));
    // Drop the phrase from memory when leaving.
    return () => setPhrase(null);
  }, [allowed]);

  return (
    <Screen scroll header={<TitleHeader title="Recovery phrase" onBack={() => router.back()} />} footer={<Button label="Done" variant="dark" onPress={() => router.back()} />}>
      <Heading title="Your recovery phrase" lead="Anyone with these words can take everything in this wallet." />
      {missing ? <Banner tone="danger">This wallet was imported from a private key, so it has no recovery phrase.</Banner> : null}
      {phrase ? <PhraseGrid phrase={phrase} hidden={!shown} onReveal={() => setShown(true)} /> : null}
      <PhraseWarnings />
    </Screen>
  );
}
