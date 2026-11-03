/**
 * Query keys. Everything the signed-in account owns starts with its id, so a
 * sign-out or account switch can drop it in one call (Noma architecture §7).
 */
export const keys = {
  chains: ["chains"] as const,
  availability: (name: string) => ["availability", name] as const,
  profile: (handle: string) => ["profile", handle] as const,
  resolve: (handle: string, chain: string, asset: string) => ["resolve", handle, chain, asset] as const,
  account: (accountId: string) => ["account", accountId] as const,
  me: ["me"] as const,
  ids: (accountId: string) => [...keys.account(accountId), "ids"] as const,
  wallets: (accountId: string) => [...keys.account(accountId), "wallets"] as const,
  transfers: (accountId: string, direction: "sent" | "received") => [...keys.account(accountId), "transfers", direction] as const,
  payment: (accountId: string, id: string) => [...keys.account(accountId), "payment", id] as const,
  agents: (accountId: string) => [...keys.account(accountId), "agents"] as const,
  agent: (accountId: string, id: string) => [...keys.account(accountId), "agents", id] as const,
  agentTransfers: (accountId: string, id: string, decision: string) => [...keys.account(accountId), "agents", id, "transfers", decision] as const,
  rename: (accountId: string, maxId: string) => [...keys.account(accountId), "rename", maxId] as const,
  renameRequest: (accountId: string, maxId: string, requestId: string) => [...keys.account(accountId), "rename", maxId, requestId] as const,
  verifications: (accountId: string) => [...keys.account(accountId), "verifications"] as const,
  reservedClaims: (accountId: string) => [...keys.account(accountId), "reserved-claims"] as const,
};
