import { create } from "zustand";

import type { ConnectedWallet } from "@/services/wallets";

/** The browser wallet connected in this tab. Memory only; reconnect after reload. */
export const useWallet = create<{
  wallet: ConnectedWallet | null;
  set(w: ConnectedWallet | null): void;
}>((set) => ({ wallet: null, set: (wallet) => set({ wallet }) }));
