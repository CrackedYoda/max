import type { Payment } from "@max/sdk";
import { create } from "zustand";

/**
 * Send flow state, memory only (never in the URL). The idempotency key is
 * minted once per exact set of reviewed terms and reused for every retry of
 * them; the tx hash is kept so a failed "record it" call can be retried
 * without signing again.
 */
type SendDraft = {
  fingerprint: string | null;
  idempotencyKey: string | null;
  payment: Payment | null;
  txHash: string | null;
  /** Returns the key to use for these terms. */
  keyFor(fingerprint: string): string;
  setPayment(p: Payment): void;
  setTxHash(h: string): void;
  reset(): void;
};

export const useSendDraft = create<SendDraft>((set, get) => ({
  fingerprint: null,
  idempotencyKey: null,
  payment: null,
  txHash: null,
  keyFor: (fingerprint) => {
    const cur = get();
    if (cur.fingerprint === fingerprint && cur.idempotencyKey) return cur.idempotencyKey;
    const key = crypto.randomUUID();
    set({ fingerprint, idempotencyKey: key, payment: null, txHash: null });
    return key;
  },
  setPayment: (payment) => set({ payment }),
  setTxHash: (txHash) => set({ txHash }),
  reset: () => set({ fingerprint: null, idempotencyKey: null, payment: null, txHash: null }),
}));

type Toast = { id: number; message: string; tone: "success" | "danger" };
let nextToast = 1;

export const useToasts = create<{ toasts: Toast[]; push(message: string, tone?: Toast["tone"]): void; dismiss(id: number): void }>((set) => ({
  toasts: [],
  push: (message, tone = "success") => {
    const id = nextToast++;
    set((s) => ({ toasts: [...s.toasts, { id, message, tone }] }));
    setTimeout(() => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })), 4000);
  },
  dismiss: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}));

export const toast = (message: string, tone?: Toast["tone"]) => useToasts.getState().push(message, tone);
