import { router } from 'expo-router';
import { useState } from 'react';

import { Banner } from '@/components/ui/Blocks';
import { Screen, TitleHeader } from '@/components/ui/Screen';
import { ImportForm } from '@/components/wallet/ImportForm';
import { errorMessage } from '@/error/messages';
import { refreshAccounts, stepUp } from '@/services/session';
import * as vault from '@/services/vault';
import { toast } from '@/store/flows';

/** Adds a private-key account to the existing wallet (one recovery phrase per phone). */
export default function AddAccount() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  return (
    <Screen scroll header={<TitleHeader title="Import account" onBack={() => router.back()} />}>
      {error ? <Banner tone="danger">{error}</Banner> : null}
      <ImportForm
        allowPhrase={false}
        busy={busy}
        onSubmit={async (r) => {
          if (r.kind !== 'key') return;
          if (!(await stepUp('Confirm adding this account to your phone.'))) return;
          setBusy(true);
          setError(null);
          try {
            await vault.addImported(r.namespace, r.secret);
            await refreshAccounts();
            toast('Account imported');
            router.back();
          } catch (err) {
            setError(errorMessage(err));
          } finally {
            setBusy(false);
          }
        }}
      />
    </Screen>
  );
}
