// In-memory keychain. `fake.failDelete` / `fake.keepOnDelete` / `fake.deleteError`
// simulate storage that refuses or silently ignores a delete.
export const WHEN_UNLOCKED_THIS_DEVICE_ONLY = 'WHEN_UNLOCKED_THIS_DEVICE_ONLY';

const slot = (key, opts) => `${opts?.keychainService ?? ''}/${key}`;

export const fake = {
  items: new Map(),
  failDelete: new Set(),
  keepOnDelete: new Set(),
  deleteError: 'Keychain error: errSecInteractionNotAllowed',
  reset() {
    this.items.clear();
    this.failDelete.clear();
    this.keepOnDelete.clear();
    this.deleteError = 'Keychain error: errSecInteractionNotAllowed';
  },
};

export async function getItemAsync(key, opts) {
  return fake.items.get(slot(key, opts)) ?? null;
}

export async function setItemAsync(key, value, opts) {
  fake.items.set(slot(key, opts), value);
}

export async function deleteItemAsync(key, opts) {
  if (fake.failDelete.has(key)) throw new Error(fake.deleteError);
  if (fake.keepOnDelete.has(key)) return;
  fake.items.delete(slot(key, opts));
}
