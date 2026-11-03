import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';

import { Handle } from '@/components/ui/AppText';
import { Banner } from '@/components/ui/Blocks';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Fields';
import { Screen, TitleHeader } from '@/components/ui/Screen';
import { errorMessage } from '@/error/messages';
import { useMyIds, useUpdateProfile } from '@/hooks/queries';
import { toast } from '@/store/flows';

export default function EditId() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const ids = useMyIds();
  const maxId = ids.data?.find((x) => x.id === id);
  const update = useUpdateProfile();
  const [displayName, setDisplayName] = useState(maxId?.displayName ?? '');
  const [description, setDescription] = useState(maxId?.description ?? '');

  return (
    <Screen
      scroll
      topPadding={0}
      header={<TitleHeader title="Edit profile" icon="close" onBack={() => router.back()} />}
      footer={
        <Button
          label="Save"
          loading={update.isPending}
          disabled={!maxId || displayName.trim().length === 0}
          onPress={() =>
            update.mutate(
              { id: id!, displayName: displayName.trim(), description: description.trim() },
              {
                onSuccess: () => {
                  toast('Profile saved');
                  router.back();
                },
              },
            )
          }
        />
      }>
      {maxId ? <Handle name={maxId.handle} size={22} /> : null}
      <TextField label="Display name" value={displayName} onChangeText={setDisplayName} maxLength={64} autoCapitalize="words" autoCorrect />
      <TextField
        label="Description"
        hint="Shown on your public profile. Keep it short."
        value={description}
        onChangeText={setDescription}
        maxLength={280}
        multiline
        autoCapitalize="sentences"
        autoCorrect
        style={{ minHeight: 110 }}
      />
      {update.error ? <Banner tone="danger">{errorMessage(update.error)}</Banner> : null}
    </Screen>
  );
}
