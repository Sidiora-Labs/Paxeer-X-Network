import { normalizeAddress } from './navigation';
export type CacheScope = { kind: 'public' } | { kind: 'wallet'; address: string } | { kind: 'session'; principalId: string; epoch: number };
/** Private keys cannot alias public, another wallet, chain or deployment. No credentials in keys. */
export function scopedQueryKey(resource: string, deployment: string, scope: CacheScope, chainId = 125) {
  if (chainId !== 125 || typeof resource !== 'string' || !/^[a-z][a-z0-9-]{0,127}$/.test(resource) || typeof deployment !== 'string' || !/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,255}$/.test(deployment) || !scope || !['public', 'wallet', 'session'].includes(scope.kind)) throw new Error('Invalid cache identity');
  const identity = scope.kind === 'public' ? ['public'] : scope.kind === 'wallet'
    ? ['wallet', normalizeAddress(scope.address)]
    : ['session', scope.principalId, scope.epoch];
  if (scope.kind === 'session' && (typeof scope.principalId !== 'string' || !/^[a-zA-Z0-9_-]{1,128}$/.test(scope.principalId) || !Number.isSafeInteger(scope.epoch) || scope.epoch < 0)) throw new Error('Invalid session scope');
  return ['foundation-v1', chainId, deployment, ...identity, resource] as const;
}
export const cachePolicy = {
  public: { header: 'public, max-age=0, s-maxage=30', persist: false, staleMs: 30000 },
  wallet: { header: 'private, no-store', persist: false, staleMs: 0 },
  session: { header: 'private, no-store', persist: false, staleMs: 0 },
} as const;
export const renderingBoundaries = {
  public: ['navigation', 'pool identity', 'market status', 'public metadata', 'discovery'],
  wallet: ['balances', 'portfolio', 'approvals', 'reward eligibility'],
  session: ['messages', 'private profile', 'attribution'],
  accountChange: 'Cancel private queries, remove prior wallet/session values, then publish the new account epoch.',
  deploymentChange: 'Remove contract-derived data and require fresh quotes; retain nonsecret draft metadata only.',
} as const;
