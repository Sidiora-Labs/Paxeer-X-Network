import type { QueryClient } from '@tanstack/react-query';
const LEGACY_PRIVATE_RESOURCES = new Set(['wallet-balances', 'address-counters', 'explorer-txs', 'explorer-transfers', 'portfolio', 'pnl', 'watchlist', 'notifications', 'messages', 'sessions', 'reward-eligibility', 'private-account', 'wallet', 'balance', 'allowance', 'session']);
export function isPrivateFoundationQuery(query: { queryKey: readonly unknown[]; meta?: Record<string, unknown> }): boolean {
  const key = query.queryKey;
  return query.meta?.private === true || query.meta?.scope === 'wallet' || query.meta?.scope === 'session' || key[0] === 'foundation-v1' && (key[3] === 'wallet' || key[3] === 'session') || typeof key[0] === 'string' && LEGACY_PRIVATE_RESOURCES.has(key[0]);
}
export async function clearPrivateFoundationCache(client: QueryClient): Promise<void> {
  if (typeof window !== 'undefined') window.dispatchEvent(new Event('kindle:private-state-cleared'));
  const filters = { predicate: isPrivateFoundationQuery };
  client.getMutationCache().clear();
  const canceled = client.cancelQueries(filters);
  client.removeQueries(filters);
  await canceled;
  client.removeQueries(filters);
}
export async function changeFoundationPrincipal(client: QueryClient, publishNext: () => void): Promise<void> {
  await clearPrivateFoundationCache(client);
  publishNext();
}
export async function invalidateFoundationDeployment(client: QueryClient, previousDeployment: string): Promise<void> {
  const filters = { predicate: (query: { queryKey: readonly unknown[]; meta?: Record<string, unknown> }) => query.queryKey[0] === 'foundation-v1' && query.queryKey[2] === previousDeployment };
  const canceled = client.cancelQueries(filters); client.removeQueries(filters); await canceled;
}
