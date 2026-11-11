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
    let live = true;
    vault
      .revealPhrase()
      .then((p) => {
        if (!live) return; // revoked while reading: never show it
        if (p) setPhrase(p);
        else setMissing(true);
      })
      .catch(() => live && setMissing(true));
    // Drop the phrase from memory when leaving or when the gate is revoked (lock, blur).
    return () => {
      live = false;
      setPhrase(null);
      setShown(false);
    };
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
