import type { Account } from "@max/sdk";
import { create } from "zustand";

/**
 * Session status only. Nothing is persisted: the HttpOnly cookie is the
 * session, and boot asks the API who we are (Noma: no stored isAuthenticated).
 */
export type SessionStatus = "booting" | "anonymous" | "authenticated" | "unavailable";

type SessionState = {
  status: SessionStatus;
  account: Account | null;
  /** Bumped on sign-out; late results from an older generation are dropped. */
  generation: number;
  setAuthenticated(account: Account): void;
  setAnonymous(): void;
  setUnavailable(): void;
  bump(): number;
};

export const useSession = create<SessionState>((set, get) => ({
  status: "booting",
  account: null,
  generation: 0,
  setAuthenticated: (account) => set({ status: "authenticated", account }),
  setAnonymous: () => set({ status: "anonymous", account: null }),
  setUnavailable: () => set({ status: "unavailable" }),
  bump: () => {
    const g = get().generation + 1;
    set({ generation: g });
    return g;
  },
}));
