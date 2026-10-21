import type { OperationInput, Operations, V2ContractTypes } from '@kindle/sdk';
import type { BadgeStatus } from '@/ui/primitives';
import { CORE_DEPLOYMENT } from '@/core/network/deployment';
import { toUnsupported, type Unsupported } from '@/core/market/unsupported';

export type Bounty = V2ContractTypes['Bounty'];
export type BountyState = Bounty['state'];
export type BountyListResponse = V2ContractTypes['BountyListResponse'];
export type BountiesStatsResponse = V2ContractTypes['BountiesStatsResponse'];
export type BountyGetResponse = V2ContractTypes['BountyGetResponse'];
export type PoolStats = V2ContractTypes['PoolStats'];
export type Token = V2ContractTypes['Token'];
export type BountyListQuery = NonNullable<OperationInput<'IF_BOUNTY_LIST'>['query']>;
export type BountyListState = NonNullable<BountyListQuery['state']>;

export interface BountyListFilter { state?: BountyListState; poolAddress?: string }
export interface BountyBadge { status: BadgeStatus; label: string }
export type RewardAssetKind = 'usdl' | 'token';
export interface BountyValuation { usdlRaw: bigint; exact: boolean; source: 'service' | 'usdl' | 'pool-valuation' }
export interface ValuationContext { token: Token | null; stats: PoolStats | null }
export type BountiesAvailability = { state: 'available'; generatedAt: string } | Unsupported;
export type BountyFailure = Unsupported | { state: 'error'; error: Error };

const ADDRESS = /^0x[0-9a-fA-F]{40}$/;
const UINT = /^(0|[1-9][0-9]{0,77})$/;
const POSITIVE_UINT = /^[1-9][0-9]{0,77}$/;
const STATES: ReadonlySet<string> = new Set<BountyState>(['draft', 'funding', 'open', 'reviewing', 'paid', 'cancelled', 'expired']);

function quarantined(response: { quality: string; watermark: { integrity?: string } | null }): boolean {
  return response.quality === 'quarantined' || response.watermark?.integrity === 'quarantined';
}

function deadlineMs(bounty: Bounty): number | null {
  if (!bounty.deadlineAt) return null;
  const parsed = Date.parse(bounty.deadlineAt);
  return Number.isNaN(parsed) ? null : parsed;
}

const OPERATIONS = { list: 'IF_BOUNTY_LIST', stats: 'IF_BOUNTIES_STATS', get: 'IF_BOUNTY_GET' } as const;
const PAGE_LIMIT = 24;
const PROBE_LIMIT = 1;
const USDL_ADDRESS = CORE_DEPLOYMENT.usdl.toLowerCase();
const USDL_DECIMALS = CORE_DEPLOYMENT.usdlDecimals;
const ENDING_SOON_MS = 48 * 3600_000;

/** One real list request with the smallest page; the answer decides whether the service exists. */
async function probe(operations: Operations, signal?: AbortSignal): Promise<BountiesAvailability> {
  try {
    const response = await operations.call('IF_BOUNTY_LIST', { query: { limit: PROBE_LIMIT } }, { signal });
    return { state: 'available', generatedAt: response.generatedAt };
  } catch (error) {
    const marker = toUnsupported(OPERATIONS.list, error);
    if (marker) return marker;
    throw error;
  }
}

function mapError(operation: string, error: unknown): BountyFailure {
  const marker = toUnsupported(operation, error);
  if (marker) return marker;
  return { state: 'error', error: error instanceof Error ? error : new Error(String(error)) };
}

/** Query object without undefined members (the compiled validators reject present-but-undefined keys). */
function listQuery(filter: BountyListFilter, cursor: string | null, limit: number = PAGE_LIMIT): BountyListQuery {
  if (!Number.isSafeInteger(limit) || limit < 1 || limit > 100) throw new RangeError('Invalid bounty page bound');
  const query: BountyListQuery = { limit };
  if (filter.state) query.state = filter.state;
  if (filter.poolAddress) query.poolAddress = filter.poolAddress.toLowerCase();
  if (cursor) query.cursor = cursor;
  return query;
}

function listFilterKey(filter: BountyListFilter): string {
  return `${filter.state ?? 'all'}:${filter.poolAddress?.toLowerCase() ?? '*'}`;
}

function validateBounty(bounty: Bounty): Bounty {
  if (!bounty.bountyId || !STATES.has(bounty.state) || !ADDRESS.test(bounty.creatorWallet) || !ADDRESS.test(bounty.assetAddress)) throw new Error('Bounty shape disagreement');
  if (!POSITIVE_UINT.test(bounty.budgetRaw)) throw new Error('Bounty budget disagreement');
  if (bounty.poolAddress !== null && !ADDRESS.test(bounty.poolAddress)) throw new Error('Bounty pool address disagreement');
  if (bounty.rewardUsdlRaw !== undefined && !UINT.test(bounty.rewardUsdlRaw)) throw new Error('Bounty valuation disagreement');
  return bounty;
}

function validateList(response: BountyListResponse, filter: BountyListFilter): BountyListResponse {
  if (quarantined(response)) throw new Error('Bounty list integrity quarantined');
  const seen = new Set<string>();
  for (const item of response.data.items) {
    validateBounty(item);
    if (filter.state && item.state !== filter.state) throw new Error('Bounty list state disagreement');
    if (filter.poolAddress && item.poolAddress?.toLowerCase() !== filter.poolAddress.toLowerCase()) throw new Error('Bounty list pool disagreement');
    if (seen.has(item.bountyId)) throw new Error('Bounty list identity disagreement');
    seen.add(item.bountyId);
  }
  return response;
}

function validateStats(response: BountiesStatsResponse): BountiesStatsResponse {
  if (quarantined(response)) throw new Error('Bounty totals integrity quarantined');
  const { openCount, submissionsCount, paidOutUsdlRaw, unclaimedPoolUsdlRaw } = response.data;
  if (!Number.isSafeInteger(openCount) || openCount < 0 || !Number.isSafeInteger(submissionsCount) || submissionsCount < 0) throw new Error('Bounty totals shape disagreement');
  if (!UINT.test(paidOutUsdlRaw) || !UINT.test(unclaimedPoolUsdlRaw)) throw new Error('Bounty totals amount disagreement');
  return response;
}

function validateBountyResponse(response: BountyGetResponse, bountyId: string): BountyGetResponse {
  if (quarantined(response)) throw new Error('Bounty integrity quarantined');
  if (validateBounty(response.data.bounty).bountyId !== bountyId) throw new Error('Bounty identity disagreement');
  return response;
}

/** Badge from the real state; "Ending soon" is derived only when a deadline is present and under 48h away. */
function badgeFor(bounty: Bounty, now: number = Date.now()): BountyBadge {
  switch (bounty.state) {
    case 'open': {
      const deadline = deadlineMs(bounty);
      if (deadline !== null && deadline > now && deadline - now < ENDING_SOON_MS) return { status: 'ending', label: 'Ending soon' };
      return { status: 'open', label: 'Open' };
    }
    case 'reviewing': return { status: 'neutral', label: 'Reviewing' };
    case 'paid': return { status: 'paid', label: 'Paid' };
    case 'cancelled': return { status: 'finished', label: 'Cancelled' };
    case 'expired': return { status: 'finished', label: 'Expired' };
    case 'funding': return { status: 'neutral', label: 'Funding' };
    case 'draft': return { status: 'neutral', label: 'Draft' };
  }
}

function rewardAssetKind(assetAddress: string): RewardAssetKind {
  return assetAddress.toLowerCase() === USDL_ADDRESS ? 'usdl' : 'token';
}

function isCreator(bounty: Bounty, wallet: string | null | undefined): boolean {
  return typeof wallet === 'string' && ADDRESS.test(wallet) && bounty.creatorWallet.toLowerCase() === wallet.toLowerCase();
}

function mine(items: readonly Bounty[], wallet: string | null | undefined): Bounty[] {
  return items.filter(item => isCreator(item, wallet));
}

/** True when the pool valuation path is the only way to value this reward. */
function needsPoolValuation(bounty: Bounty): boolean {
  return bounty.rewardUsdlRaw === undefined && rewardAssetKind(bounty.assetAddress) === 'token' && bounty.poolAddress !== null;
}

/**
 * USDL value of the escrowed reward. Exact when the service reports it or
 * the asset is USDL itself; approximate when derived from the linked pool's
 * supply valuation (IF_STATS totalSupplyValuationUsdlRaw / token supply).
 * Null when nothing real supports a number.
 */
function usdlValue(bounty: Bounty, context: ValuationContext): BountyValuation | null {
  if (bounty.rewardUsdlRaw !== undefined) return { usdlRaw: BigInt(bounty.rewardUsdlRaw), exact: true, source: 'service' };
  if (rewardAssetKind(bounty.assetAddress) === 'usdl') return { usdlRaw: BigInt(bounty.budgetRaw), exact: true, source: 'usdl' };
  const { token, stats } = context;
  if (!token || !stats || token.address.toLowerCase() !== bounty.assetAddress.toLowerCase()) return null;
  if (stats.supplyValuationQuality === 'unavailable' || stats.totalSupplyValuationUsdlRaw === null || token.totalSupplyRaw === null) return null;
  if (!UINT.test(stats.totalSupplyValuationUsdlRaw) || !POSITIVE_UINT.test(token.totalSupplyRaw)) return null;
  const usdlRaw = (BigInt(bounty.budgetRaw) * BigInt(stats.totalSupplyValuationUsdlRaw)) / BigInt(token.totalSupplyRaw);
  return { usdlRaw, exact: false, source: 'pool-valuation' };
}

/** Open bounties ordered by USDL value descending; rewards without a real value sort last, in list order. */
function rankByValue<T extends { bounty: Bounty; valuation: BountyValuation | null }>(rows: readonly T[]): T[] {
  return [...rows].sort((a, b) => {
    if (a.valuation && b.valuation) return a.valuation.usdlRaw === b.valuation.usdlRaw ? 0 : a.valuation.usdlRaw > b.valuation.usdlRaw ? -1 : 1;
    if (a.valuation) return -1;
    if (b.valuation) return 1;
    return 0;
  });
}

/**
 * Bounties read contract: IF_BOUNTY_LIST `GET /v2/bounties`, IF_BOUNTIES_STATS
 * `GET /v2/bounties/stats`, IF_BOUNTY_GET `GET /v2/bounties/{bountyId}` on
 * core/api. No handler serves them today, so every call answers 404 and the
 * SDK raises CapabilityUnavailableError; this module turns that into an
 * Unsupported marker and never substitutes rows.
 */
export const bountiesContract = {
  operations: OPERATIONS,
  pageLimit: PAGE_LIMIT,
  probeLimit: PROBE_LIMIT,
  usdlAddress: USDL_ADDRESS,
  usdlDecimals: USDL_DECIMALS,
  endingSoonMs: ENDING_SOON_MS,
  probe,
  mapError,
  listQuery,
  listFilterKey,
  validateBounty,
  validateList,
  validateStats,
  validateBountyResponse,
  badgeFor,
  rewardAssetKind,
  isCreator,
  mine,
  needsPoolValuation,
  usdlValue,
  rankByValue,
};

export type BountiesContract = typeof bountiesContract;
