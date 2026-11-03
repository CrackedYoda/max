import { create } from 'zustand';

import type { AccountInfo } from '@/services/vault';

/**
 * Two independent states, neither persisted (Noma: no stored "isAuthenticated"):
 *  - vault: is there a wallet on this phone, and is it unlocked in memory?
 *  - max:   is this phone signed in to the MAX API? The wallet works without it
 *           (balances, keys); handles and payments by name need it.
 */
export type VaultStatus = 'booting' | 'none' | 'locked' | 'unlocked';
export type MaxStatus = 'signed_out' | 'connecting' | 'ready' | 'unavailable';

type SessionState = {
  vault: VaultStatus;
  /** True once the wallet was unlocked in this process: lock keeps app screens mounted. */
  unlockedOnce: boolean;
  accounts: AccountInfo[];
  max: MaxStatus;
  maxError: string | null;
  accountId: string | null;
  /** Bumped on every lock/removal; late async results from older generations are dropped. */
  generation: number;
  biometricEnabled: boolean;
  biometricLabel: string;
  /** Set right after onboarding so the app opens on "pick your name". */
  pendingClaim: boolean;

  setVault(v: VaultStatus): void;
  setAccounts(a: AccountInfo[]): void;
  setMax(s: MaxStatus, error?: string | null): void;
  setAccountId(id: string | null): void;
  setBiometric(enabled: boolean, label?: string): void;
  setPendingClaim(v: boolean): void;
  bumpGeneration(): number;
  reset(): void;
};

export const useSession = create<SessionState>((set, get) => ({
  vault: 'booting',
  unlockedOnce: false,
  accounts: [],
  max: 'signed_out',
  maxError: null,
  accountId: null,
  generation: 0,
  biometricEnabled: false,
  biometricLabel: 'Face ID',
  pendingClaim: false,

  setVault: (vault) => set(vault === 'unlocked' ? { vault, unlockedOnce: true } : { vault }),
  setAccounts: (accounts) => set({ accounts }),
  setMax: (max, maxError = null) => set({ max, maxError }),
  setAccountId: (accountId) => set({ accountId }),
  setBiometric: (biometricEnabled, label) => set(label ? { biometricEnabled, biometricLabel: label } : { biometricEnabled }),
  setPendingClaim: (pendingClaim) => set({ pendingClaim }),
  bumpGeneration: () => {
    const next = get().generation + 1;
    set({ generation: next });
    return next;
  },
  reset: () =>
    set({
      vault: 'none',
      unlockedOnce: false,
      accounts: [],
      max: 'signed_out',
      maxError: null,
      accountId: null,
      biometricEnabled: false,
      pendingClaim: false,
    }),
}));

export const getGeneration = () => useSession.getState().generation;
