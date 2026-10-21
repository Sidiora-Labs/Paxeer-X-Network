import { CapabilityUnavailableError, HttpError, type OperationInput, type Operations, type V2ContractTypes } from '@kindle/sdk';
import { getAddress, type PublicClient } from 'viem';
import { CORE_DEPLOYMENT } from '@/core/network/deployment';
import { toUnsupported, type Unsupported } from '@/core/market/unsupported';
import { marketAddress, marketLimits } from '@/page-modules/token/market-session';

export type HolderRewardsStatsResponse = V2ContractTypes['HolderRewardsStatsResponse'];
export type HolderRewardsPoolsResponse = V2ContractTypes['HolderRewardsPoolsResponse'];
export type HolderRewardPool = V2ContractTypes['HolderRewardPool'];
export type StatsBatchResponse = V2ContractTypes['StatsBatchResponse'];
export type PoolStats = V2ContractTypes['PoolStats'];
export type HolderRewardsSort = NonNullable<NonNullable<OperationInput<'IF_HOLDER_REWARDS_POOLS'>['query']>['sort']>;

/** Contract operation ids this page binds to (client/packages/sdk/src/operations.gen.ts). */
export const holderRewardsOperations = Object.freeze({ stats: 'IF_HOLDER_REWARDS_STATS', pools: 'IF_HOLDER_REWARDS_POOLS', statsBatch: 'IF_STATS_BATCH' } as const);

/** One page of pools; equals the IF_STATS_BATCH bound so every page can be priced in one batch. */
export const holderRewardsPageSize = marketLimits.rankingPage;
export const holderRewardsSorts: readonly HolderRewardsSort[] = Object.freeze(['rewards', 'mcap', 'holders', 'age']);

const uint = /^(0|[1-9][0-9]{0,77})$/;

function quarantined(response: { quality: string; watermark: V2ContractTypes['Watermark'] | null }): boolean {
  return response.quality === 'quarantined' || response.watermark?.integrity === 'quarantined';
}

export function validateHolderRewardsStats(response: HolderRewardsStatsResponse): HolderRewardsStatsResponse {
  if (quarantined(response) || response.data.watermark?.integrity === 'quarantined') throw new Error('Holder rewards integrity quarantined');
  if (!uint.test(response.data.cumulativeDistributedRaw)) throw new TypeError('Holder rewards cumulative amount shape disagreement');
  if (!Number.isSafeInteger(response.data.walletsRewarded) || response.data.walletsRewarded < 0) throw new TypeError('Holder rewards wallet count shape disagreement');
  return response;
}

export function validateHolderRewardsPools(response: HolderRewardsPoolsResponse, limit: number): HolderRewardsPoolsResponse {
  if (quarantined(response)) throw new Error('Holder rewards integrity quarantined');
  const items = response.data.items;
  if (items.length > limit) throw new RangeError(`Holder rewards page exceeds ${limit}`);
  const seen = new Set<string>();
  for (const item of items) {
    const pool = marketAddress(item.poolAddress);
    marketAddress(item.tokenAddress);
    if (seen.has(pool)) throw new Error('Duplicate holder rewards pool identity');
    seen.add(pool);
    if (!uint.test(item.distributedRaw)) throw new TypeError('Holder rewards distributed amount shape disagreement');
    if (!Number.isSafeInteger(item.holdersPaid) || item.holdersPaid < 0) throw new TypeError('Holder rewards holders paid shape disagreement');
    if (Number.isNaN(Date.parse(item.createdAt))) throw new TypeError('Holder rewards createdAt shape disagreement');
  }
  return response;
}

/** IF_HOLDER_REWARDS_STATS `GET /v2/holder-rewards/stats`. Unserved today: the SDK raises CapabilityUnavailableError (real 404). */
export async function fetchHolderRewardsStats(operations: Operations, signal?: AbortSignal): Promise<HolderRewardsStatsResponse> {
  return validateHolderRewardsStats(await operations.call(holderRewardsOperations.stats, {}, { signal }));
}

export interface HolderRewardsPoolsQuery { sort: HolderRewardsSort; cursor: string | null; limit: number }

/** IF_HOLDER_REWARDS_POOLS `GET /v2/holder-rewards/pools?sort=&cursor=&limit=`. */
export async function fetchHolderRewardsPools(operations: Operations, query: HolderRewardsPoolsQuery, signal?: AbortSignal): Promise<HolderRewardsPoolsResponse> {
  if (!holderRewardsSorts.includes(query.sort)) throw new RangeError('Invalid holder rewards sort');
  if (!Number.isSafeInteger(query.limit) || query.limit < 1 || query.limit > holderRewardsPageSize) throw new RangeError('Invalid holder rewards page bound');
  const response = await operations.call(holderRewardsOperations.pools, { query: { sort: query.sort, limit: query.limit, ...(query.cursor ? { cursor: query.cursor } : {}) } }, { signal });
  return validateHolderRewardsPools(response, query.limit);
}

/**
 * Why the read model did not answer. `unindexed` is the real 404/501 from a
 * core/api without the holder-rewards handler; `degraded` is a served route
 * whose dependency is down (503/412) and worth retrying; everything else is a
 * genuine failure surfaced as such.
 */
export type HolderRewardsAvailability =
  | { kind: 'unindexed'; marker: Unsupported }
  | { kind: 'degraded'; marker: Unsupported }
  | { kind: 'failed'; operation: string; error: Error };

export function classifyHolderRewardsFailure(operation: string, error: unknown): HolderRewardsAvailability {
  const marker = toUnsupported(operation, error);
  if (marker) return { kind: marker.status === 404 || marker.status === 501 ? 'unindexed' : 'degraded', marker };
  const failure = error instanceof Error ? error : new Error(String(error));
  return { kind: 'failed', operation, error: failure };
}

export function isCapabilityGap(error: unknown): boolean {
  return error instanceof CapabilityUnavailableError || (error instanceof HttpError && (error.status === 404 || error.status === 501));
}

export function chunkPools(pools: readonly string[], size: number = marketLimits.rankingPage): string[][] {
  const unique = [...new Set(pools.map(marketAddress))];
  const chunks: string[][] = [];
  for (let index = 0; index < unique.length; index += size) chunks.push(unique.slice(index, index + size).sort());
  return chunks;
}

/** `IFeeAccumulator.getAirdropBalance(pool)` (contracts/contracts/interfaces/IFeeAccumulator.sol:117): USDL accrued for the pool's next holder airdrop. */
export const feeAccumulatorAbi = [
  { type: 'function', name: 'getAirdropBalance', stateMutability: 'view', inputs: [{ name: 'pool', type: 'address' }], outputs: [{ name: '', type: 'uint256' }] },
] as const;

export const feeAccumulatorAddress = getAddress(CORE_DEPLOYMENT.feeAccumulator);

export type AirdropBalanceReader = Pick<PublicClient, 'readContract'>;

/** Single on-chain read of a pool's pending airdrop balance; called only for the row under the pointer, never for the list. */
export async function readAirdropBalance(client: AirdropBalanceReader, poolAddress: string): Promise<bigint> {
  const pool = getAddress(marketAddress(poolAddress));
  const balance = await client.readContract({ address: feeAccumulatorAddress, abi: feeAccumulatorAbi, functionName: 'getAirdropBalance', args: [pool] });
  if (typeof balance !== 'bigint' || balance < 0n) throw new TypeError('Airdrop balance shape disagreement');
  return balance;
}
