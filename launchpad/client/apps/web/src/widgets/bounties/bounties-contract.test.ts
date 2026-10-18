import { describe, expect, it } from 'vitest';
import { CapabilityUnavailableError, HttpError } from '@kindle/sdk';
import { bountiesContract, type Bounty, type BountyListResponse, type BountiesStatsResponse, type PoolStats, type Token } from './bounties-contract';

const creator = '0x1111111111111111111111111111111111111111';
const pool = '0x2222222222222222222222222222222222222222';
const tokenAddress = '0x3333333333333333333333333333333333333333';
const usdl = bountiesContract.usdlAddress;
const now = Date.parse('2026-10-12T12:00:00Z');

function bounty(overrides: Partial<Bounty> = {}): Bounty {
  return { bountyId: 'b-1', title: 'Write a thread', description: 'Explain the fee strategy', creatorWallet: creator, assetAddress: usdl, budgetRaw: '250000000', poolAddress: pool, state: 'open', ...overrides };
}

function envelope<T>(data: T, quality: BountyListResponse['quality'] = 'complete') {
  return { data, generatedAt: '2026-10-12T12:00:00Z', nextCursor: null, quality, requestId: 'r-1', revision: null, watermark: null };
}

const position = { blockHash: '0x' + 'a'.repeat(64), chainId: '125' as const, height: '1', logIndex: '0', transactionHash: '0x' + 'b'.repeat(64), transactionIndex: '0' };
const watermark: PoolStats['watermark'] = { blockHash: position.blockHash, chainId: '125', height: '1', integrity: 'healthy', observedAt: '2026-10-12T12:00:00Z', projectionRevision: '1' };
const token: Token = { address: tokenAddress, decimals: 6, decimalsQuality: 'verified', logoAssetId: null, metadataRevision: '1', name: 'Launch', sourcePosition: position, supplyQuality: 'verified', symbol: 'LNCH', totalSupplyRaw: '1000000000000000' };
const stats: PoolStats = {
  buys24h: '0', changeBaselineQuality: 'complete', circulatingMarketCapUsdlRaw: '5000000000', holderCount: '10', indicativeTvlUsdlRaw: '1000000000', poolAddress: pool, priceChangeBps24h: '0', realUsdlBalanceRaw: '1000000000', sells24h: '0',
  sourceUpperPosition: position, supplyValuationQuality: 'verified', totalSupplyValuationUsdlRaw: '10000000000', valuationChangeUsdlRaw: '0', valuationMethod: 'spot', volume24hRaw: '0', watermark, windowEnd: '2026-10-12T12:00:00Z', windowStart: '2026-10-11T12:00:00Z',
};

describe('bountiesContract list requests', () => {
  it('builds a query without undefined members and honours the page bound', () => {
    expect(bountiesContract.listQuery({}, null)).toEqual({ limit: bountiesContract.pageLimit });
    expect(bountiesContract.listQuery({ state: 'paid', poolAddress: pool.toUpperCase() }, 'c-2', 10)).toEqual({ limit: 10, state: 'paid', poolAddress: pool, cursor: 'c-2' });
    expect(() => bountiesContract.listQuery({}, null, 101)).toThrow(RangeError);
    expect(() => bountiesContract.listQuery({}, null, 0)).toThrow(RangeError);
  });
  it('keys filters by state and pool', () => {
    expect(bountiesContract.listFilterKey({})).toBe('all:*');
    expect(bountiesContract.listFilterKey({ state: 'open', poolAddress: pool.toUpperCase() })).toBe(`open:${pool}`);
  });
  it('accepts a complete page that matches the filter', () => {
    const response = envelope({ items: [bounty(), bounty({ bountyId: 'b-2' })] });
    expect(bountiesContract.validateList(response, { state: 'open' })).toBe(response);
  });
  it('rejects quarantined pages, state disagreement, duplicate ids and malformed rows', () => {
    expect(() => bountiesContract.validateList(envelope({ items: [] }, 'quarantined'), {})).toThrow(/quarantined/);
    expect(() => bountiesContract.validateList(envelope({ items: [bounty({ state: 'paid' })] }), { state: 'open' })).toThrow(/state disagreement/);
    expect(() => bountiesContract.validateList(envelope({ items: [bounty(), bounty()] }), {})).toThrow(/identity disagreement/);
    expect(() => bountiesContract.validateList(envelope({ items: [bounty({ poolAddress: tokenAddress })] }), { poolAddress: pool })).toThrow(/pool disagreement/);
    expect(() => bountiesContract.validateList(envelope({ items: [bounty({ budgetRaw: '0' })] }), {})).toThrow(/budget/);
    expect(() => bountiesContract.validateList(envelope({ items: [bounty({ creatorWallet: 'nope' })] }), {})).toThrow(/shape/);
  });
  it('validates the totals envelope', () => {
    const response: BountiesStatsResponse = envelope({ openCount: 3, submissionsCount: 12, paidOutUsdlRaw: '1000000', unclaimedPoolUsdlRaw: '0' });
    expect(bountiesContract.validateStats(response)).toBe(response);
    expect(() => bountiesContract.validateStats(envelope({ ...response.data, openCount: -1 }))).toThrow(/shape/);
    expect(() => bountiesContract.validateStats(envelope({ ...response.data, paidOutUsdlRaw: '1.5' }))).toThrow(/amount/);
  });
  it('checks detail identity', () => {
    const response = envelope({ bounty: bounty() });
    expect(bountiesContract.validateBountyResponse(response, 'b-1')).toBe(response);
    expect(() => bountiesContract.validateBountyResponse(response, 'b-9')).toThrow(/identity/);
  });
});

describe('bountiesContract error mapping', () => {
  it('maps the real capability and gateway failures to Unsupported', () => {
    expect(bountiesContract.mapError('IF_BOUNTY_LIST', new CapabilityUnavailableError('Unavailable endpoint /v2/bounties', 404))).toEqual({ state: 'unsupported', operation: 'IF_BOUNTY_LIST', status: 404, message: 'Unavailable endpoint /v2/bounties' });
    expect(bountiesContract.mapError('IF_BOUNTIES_STATS', new HttpError(503))).toMatchObject({ state: 'unsupported', status: 503 });
  });
  it('keeps every other failure as an error', () => {
    const mapped = bountiesContract.mapError('IF_BOUNTY_LIST', new HttpError(500));
    expect(mapped.state).toBe('error');
    if (mapped.state === 'error') expect(mapped.error).toBeInstanceOf(HttpError);
    expect(bountiesContract.mapError('IF_BOUNTY_LIST', 'boom')).toMatchObject({ state: 'error', error: new Error('boom') });
  });
});

describe('bountiesContract presentation rules', () => {
  it('derives badges from the real state and only flags ending soon under 48h with a deadline', () => {
    expect(bountiesContract.badgeFor(bounty(), now)).toEqual({ status: 'open', label: 'Open' });
    expect(bountiesContract.badgeFor(bounty({ deadlineAt: '2026-10-13T11:00:00Z' }), now)).toEqual({ status: 'ending', label: 'Ending soon' });
    expect(bountiesContract.badgeFor(bounty({ deadlineAt: '2026-10-20T11:00:00Z' }), now)).toEqual({ status: 'open', label: 'Open' });
    expect(bountiesContract.badgeFor(bounty({ deadlineAt: '2026-10-11T11:00:00Z' }), now)).toEqual({ status: 'open', label: 'Open' });
    expect(bountiesContract.badgeFor(bounty({ state: 'paid' }), now)).toEqual({ status: 'paid', label: 'Paid' });
    expect(bountiesContract.badgeFor(bounty({ state: 'reviewing' }), now)).toEqual({ status: 'neutral', label: 'Reviewing' });
    expect(bountiesContract.badgeFor(bounty({ state: 'expired' }), now)).toEqual({ status: 'finished', label: 'Expired' });
    expect(bountiesContract.badgeFor(bounty({ state: 'cancelled' }), now)).toEqual({ status: 'finished', label: 'Cancelled' });
  });
  it('recognises the USDL reward asset and the creator wallet case-insensitively', () => {
    expect(bountiesContract.rewardAssetKind(usdl.toUpperCase().replace('0X', '0x'))).toBe('usdl');
    expect(bountiesContract.rewardAssetKind(tokenAddress)).toBe('token');
    expect(bountiesContract.isCreator(bounty(), creator.toUpperCase().replace('0X', '0x'))).toBe(true);
    expect(bountiesContract.isCreator(bounty(), pool)).toBe(false);
    expect(bountiesContract.isCreator(bounty(), undefined)).toBe(false);
    expect(bountiesContract.mine([bounty(), bounty({ bountyId: 'b-2', creatorWallet: pool })], creator)).toHaveLength(1);
  });
});

describe('bountiesContract valuation', () => {
  it('is exact for service-reported and USDL rewards', () => {
    expect(bountiesContract.usdlValue(bounty({ rewardUsdlRaw: '123' }), { token: null, stats: null })).toEqual({ usdlRaw: 123n, exact: true, source: 'service' });
    expect(bountiesContract.usdlValue(bounty(), { token: null, stats: null })).toEqual({ usdlRaw: 250000000n, exact: true, source: 'usdl' });
    expect(bountiesContract.needsPoolValuation(bounty())).toBe(false);
  });
  it('derives an approximate value for a launched-token reward from the pool supply valuation', () => {
    const tokenBounty = bounty({ assetAddress: tokenAddress, budgetRaw: '100000000000000' });
    expect(bountiesContract.needsPoolValuation(tokenBounty)).toBe(true);
    expect(bountiesContract.usdlValue(tokenBounty, { token, stats })).toEqual({ usdlRaw: 1000000000n, exact: false, source: 'pool-valuation' });
  });
  it('returns null when nothing real supports a number', () => {
    const tokenBounty = bounty({ assetAddress: tokenAddress });
    expect(bountiesContract.usdlValue(tokenBounty, { token: null, stats: null })).toBeNull();
    expect(bountiesContract.usdlValue(tokenBounty, { token: { ...token, address: pool }, stats })).toBeNull();
    expect(bountiesContract.usdlValue(tokenBounty, { token, stats: { ...stats, supplyValuationQuality: 'unavailable' } })).toBeNull();
    expect(bountiesContract.usdlValue(tokenBounty, { token, stats: { ...stats, totalSupplyValuationUsdlRaw: null } })).toBeNull();
    expect(bountiesContract.usdlValue(tokenBounty, { token: { ...token, totalSupplyRaw: null }, stats })).toBeNull();
  });
  it('ranks by value descending with unvalued rewards last in list order', () => {
    const rows = [
      { bounty: bounty({ bountyId: 'low' }), valuation: { usdlRaw: 1n, exact: true, source: 'usdl' as const } },
      { bounty: bounty({ bountyId: 'none-a' }), valuation: null },
      { bounty: bounty({ bountyId: 'high' }), valuation: { usdlRaw: 9n, exact: false, source: 'pool-valuation' as const } },
      { bounty: bounty({ bountyId: 'none-b' }), valuation: null },
    ];
    expect(bountiesContract.rankByValue(rows).map(row => row.bounty.bountyId)).toEqual(['high', 'low', 'none-a', 'none-b']);
  });
});
