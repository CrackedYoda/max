import type { Payment, RenameView } from '@max/sdk';
import * as Crypto from 'expo-crypto';
import { create } from 'zustand';

/**
 * Memory-only flow state (Noma flowStores). Secrets never go into route params,
 * which can end up in logs and navigation state.
 */

// ---- onboarding: the new phrase lives here until it's saved behind a passcode ----

type OnboardingState = {
  phrase: string | null;
  /** Wallet chosen on the import screen, saved at the passcode step. */
  imported: { kind: 'phrase'; phrase: string } | { kind: 'key'; namespace: 'evm' | 'solana'; secret: string } | null;
  setPhrase(p: string | null): void;
  setImported(i: OnboardingState['imported']): void;
  clear(): void;
};

export const useOnboarding = create<OnboardingState>((set) => ({
  phrase: null,
  imported: null,
  setPhrase: (phrase) => set({ phrase }),
  setImported: (imported) => set({ imported }),
  clear: () => set({ phrase: null, imported: null }),
}));

// ---- send ----

export type SendTerms = {
  handle: string;
  chain: string;
  asset: string; // "native" or contract/mint
  amount: string;
  senderAddress: string;
};

/** A signed transaction kept so retries re-broadcast it instead of signing twice. */
export type SignedTx = { hash: string; raw: string; namespace: 'evm' | 'solana' };

type SendState = {
  terms: SendTerms | null;
  /** One idempotency key per exact set of terms. */
  fingerprint: string | null;
  idempotencyKey: string | null;
  payment: Payment | null;
  signed: SignedTx | null;
  setTerms(t: SendTerms): void;
  keyFor(fingerprint: string): string;
  setPayment(p: Payment | null): void;
  setSigned(s: SignedTx | null): void;
  reset(): void;
};

export const useSendDraft = create<SendState>((set, get) => ({
  terms: null,
  fingerprint: null,
  idempotencyKey: null,
  payment: null,
  signed: null,
  setTerms: (terms) => set({ terms }),
  keyFor: (fingerprint) => {
    const s = get();
    if (s.fingerprint === fingerprint && s.idempotencyKey) return s.idempotencyKey;
    const key = Crypto.randomUUID();
    set({ fingerprint, idempotencyKey: key, payment: null, signed: null });
    return key;
  },
  setPayment: (payment) => set({ payment }),
  setSigned: (signed) => set({ signed }),
  reset: () => set({ terms: null, fingerprint: null, idempotencyKey: null, payment: null, signed: null }),
}));

// ---- rename: the fee payment is signed once; retries reuse these bytes ----

type RenameState = {
  /** One idempotency key per exact rename terms, so a retry never reserves twice. */
  fingerprint: string | null;
  idempotencyKey: string | null;
  view: RenameView | null;
  signed: SignedTx | null;
  keyFor(fingerprint: string): string;
  setView(v: RenameView | null): void;
  setSigned(s: SignedTx | null): void;
  reset(): void;
};

export const useRenameDraft = create<RenameState>((set, get) => ({
  fingerprint: null,
  idempotencyKey: null,
  view: null,
  signed: null,
  keyFor: (fingerprint) => {
    const s = get();
    if (s.fingerprint === fingerprint && s.idempotencyKey) return s.idempotencyKey;
    const key = Crypto.randomUUID();
    set({ fingerprint, idempotencyKey: key, view: null, signed: null });
    return key;
  },
  setView: (view) => set({ view }),
  setSigned: (signed) => set({ signed }),
  reset: () => set({ fingerprint: null, idempotencyKey: null, view: null, signed: null }),
}));

// ---- toasts ----

type Toast = { id: number; text: string };
let nextToast = 1;

export const useToasts = create<{ toasts: Toast[]; dismiss(id: number): void }>((set) => ({
  toasts: [],
  dismiss: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}));

export function toast(text: string): void {
  const id = nextToast++;
  useToasts.setState((s) => ({ toasts: [...s.toasts.slice(-2), { id, text }] }));
  setTimeout(() => useToasts.getState().dismiss(id), 3200);
}

// ---- step-up: confirm it's you before sensitive actions ----

type StepUpRequest = { reason: string; resolve(ok: boolean): void };

export const useStepUp = create<{ request: StepUpRequest | null; set(r: StepUpRequest | null): void }>((set) => ({
  request: null,
  set: (request) => set({ request }),
}));
