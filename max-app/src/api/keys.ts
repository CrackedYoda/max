/**
 * Query keys. Everything the signed-in account owns starts with its id, so
 * removing the wallet or switching accounts drops it in one call.
 */
export const keys = {
  chains: ['chains'] as const,
  availability: (name: string) => ['availability', name] as const,
  resolve: (handle: string, chain: string, asset: string) => ['resolve', handle, chain, asset] as const,
  balances: (addresses: string) => ['balances', addresses] as const,
  account: (accountId: string) => ['account', accountId] as const,
  ids: (accountId: string) => [...keys.account(accountId), 'ids'] as const,
  wallets: (accountId: string) => [...keys.account(accountId), 'wallets'] as const,
  transfers: (accountId: string, direction: 'sent' | 'received') => [...keys.account(accountId), 'transfers', direction] as const,
  payment: (accountId: string, id: string) => [...keys.account(accountId), 'payment', id] as const,
  rename: (accountId: string, maxId: string) => [...keys.account(accountId), 'rename', maxId] as const,
  renameRequest: (accountId: string, maxId: string, requestId: string) => [...keys.account(accountId), 'rename', maxId, requestId] as const,
  verifications: (accountId: string) => [...keys.account(accountId), 'verifications'] as const,
  reservedClaims: (accountId: string) => [...keys.account(accountId), 'reserved-claims'] as const,
};
