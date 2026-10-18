import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { CapabilityUnavailableError, HttpError, Operations, SdkClient, type V2ContractTypes } from '@kindle/sdk';
import { toUnsupported } from '@/core/market/unsupported';
import { CORE_DEPLOYMENT } from '@/core/network/deployment';
import {
  chunkPools,
  classifyHolderRewardsFailure,
  feeAccumulatorAbi,
  feeAccumulatorAddress,
  fetchHolderRewardsPools,
  fetchHolderRewardsStats,
  holderRewardsOperations,
  holderRewardsPageSize,
  readAirdropBalance,
  validateHolderRewardsPools,
  validateHolderRewardsStats,
  type HolderRewardsPoolsResponse,
  type HolderRewardsStatsResponse,
} from './holder-rewards-contract';

type Watermark = V2ContractTypes['Watermark'];
type ApiError = V2ContractTypes['Error'];

const poolA = '0x' + 'a1'.repeat(20);
const poolB = '0x' + 'b2'.repeat(20);
const tokenA = '0x' + 'c3'.repeat(20);
const tokenB = '0x' + 'd4'.repeat(20);
const requestId = '0f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01';
const generatedAt = '2026-10-12T15:00:01.000Z';
const watermark: Watermark = { chainId: '125', height: '4200', blockHash: '0x' + '6'.repeat(64), projectionRevision: '7', integrity: 'healthy', observedAt: '2026-10-12T15:00:00.000Z' };

const statsResponse: HolderRewardsStatsResponse = {
  requestId, generatedAt, nextCursor: null, quality: 'complete', revision: '7', watermark,
  data: { cumulativeDistributedRaw: '18627546000000', walletsRewarded: 445691, generatedAt, watermark },
};
const poolsResponse: HolderRewardsPoolsResponse = {
  requestId, generatedAt, nextCursor: 'next-page', quality: 'complete', revision: '7', watermark,
  data: { items: [
    { poolAddress: poolA, tokenAddress: tokenA, distributedRaw: '504967000000', holdersPaid: 14648, strategyActive: true, createdAt: '2026-09-14T10:00:00.000Z' },
    { poolAddress: poolB, tokenAddress: tokenB, distributedRaw: '498048000000', holdersPaid: 26093, strategyActive: false, createdAt: '2026-09-01T10:00:00.000Z' },
  ] },
};

type Responder = (path: string) => { status: number; body?: unknown };

/** A real HTTP origin standing in for core/api so the SDK's own 404/503/500 handling runs end to end. */
function serve(responder: Responder): Promise<{ server: Server; origin: string; paths: string[] }> {
  const paths: string[] = [];
  const server = createServer((request, response) => {
    paths.push(request.url ?? '');
    const answer = responder(request.url ?? '');
    response.statusCode = answer.status;
    if (answer.body === undefined) { response.end(); return; }
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify(answer.body));
  });
  return new Promise(resolve => {
    server.listen(0, '127.0.0.1', () => {
      const { port } = server.address() as AddressInfo;
      resolve({ server, origin: `http://127.0.0.1:${port}`, paths });
    });
  });
}

function operationsFor(origin: string): Operations {
  const client = new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: 'ws://127.0.0.1:1', approvedStreamOrigins: ['ws://127.0.0.1:1'], production: false });
  return new Operations(client);
}

describe('holder rewards contract against a real HTTP origin', () => {
  let unserved: Awaited<ReturnType<typeof serve>>;
  let degraded: Awaited<ReturnType<typeof serve>>;
  let failing: Awaited<ReturnType<typeof serve>>;
  let served: Awaited<ReturnType<typeof serve>>;

  beforeAll(async () => {
    unserved = await serve(() => ({ status: 404 }));
    const envelope: ApiError = { code: 'DEPENDENCY_UNAVAILABLE', message: 'indexer read model offline', requestId, retryable: false, retryAfterMs: null, fieldErrors: [], details: null };
    degraded = await serve(() => ({ status: 503, body: envelope }));
    failing = await serve(() => ({ status: 500, body: { code: 'INTERNAL', message: 'x', requestId, retryable: false, retryAfterMs: null, fieldErrors: [], details: null } }));
    served = await serve(path => path.startsWith('/v2/holder-rewards/stats') ? { status: 200, body: statsResponse } : path.startsWith('/v2/holder-rewards/pools') ? { status: 200, body: poolsResponse } : { status: 404 });
  });
  afterAll(async () => {
    for (const { server } of [unserved, degraded, failing, served]) await new Promise<void>(resolve => server.close(() => resolve()));
  });

  it('turns the real 404 of an unserved core/api into CapabilityUnavailableError and the page-level unindexed state', async () => {
    const operations = operationsFor(unserved.origin);
    const failure = await fetchHolderRewardsPools(operations, { sort: 'rewards', cursor: null, limit: holderRewardsPageSize }).catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(CapabilityUnavailableError);
    expect((failure as CapabilityUnavailableError).status).toBe(404);
    expect(unserved.paths).toContain(`/v2/holder-rewards/pools?sort=rewards&limit=${holderRewardsPageSize}`);
    const classified = classifyHolderRewardsFailure(holderRewardsOperations.pools, failure);
    expect(classified).toEqual({ kind: 'unindexed', marker: { state: 'unsupported', operation: 'IF_HOLDER_REWARDS_POOLS', status: 404, message: 'Unavailable endpoint /v2/holder-rewards/pools' } });

    const stats = await fetchHolderRewardsStats(operations).catch((error: unknown) => error);
    expect(stats).toBeInstanceOf(CapabilityUnavailableError);
    expect(unserved.paths).toContain('/v2/holder-rewards/stats');
    expect(toUnsupported(holderRewardsOperations.stats, stats)).toMatchObject({ state: 'unsupported', operation: 'IF_HOLDER_REWARDS_STATS', status: 404 });
  });

  it('keeps a served route whose dependency is down as a retryable degraded state', async () => {
    const failure = await fetchHolderRewardsPools(operationsFor(degraded.origin), { sort: 'mcap', cursor: 'abc', limit: 10 }).catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(HttpError);
    expect((failure as HttpError).status).toBe(503);
    expect(degraded.paths).toContain('/v2/holder-rewards/pools?sort=mcap&cursor=abc&limit=10');
    expect(classifyHolderRewardsFailure(holderRewardsOperations.pools, failure)).toEqual({ kind: 'degraded', marker: { state: 'unsupported', operation: 'IF_HOLDER_REWARDS_POOLS', status: 503, message: 'DEPENDENCY_UNAVAILABLE' } });
  });

  it('reports every other failure as a genuine error, never as unsupported', async () => {
    const failure = await fetchHolderRewardsStats(operationsFor(failing.origin)).catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(HttpError);
    const classified = classifyHolderRewardsFailure(holderRewardsOperations.stats, failure);
    expect(classified.kind).toBe('failed');
    if (classified.kind === 'failed') expect(classified.error).toBe(failure);
    expect(classifyHolderRewardsFailure('IF_X', new TypeError('shape')).kind).toBe('failed');
  });

  it('decodes a served response through the SDK contract validators', async () => {
    const operations = operationsFor(served.origin);
    const pools = await fetchHolderRewardsPools(operations, { sort: 'holders', cursor: null, limit: 50 });
    expect(pools.data.items.map(item => item.poolAddress)).toEqual([poolA, poolB]);
    expect(pools.nextCursor).toBe('next-page');
    const stats = await fetchHolderRewardsStats(operations);
    expect(stats.data.walletsRewarded).toBe(445691);
  });

  it('refuses invalid sort and page bounds before any request', async () => {
    const operations = operationsFor(served.origin);
    await expect(fetchHolderRewardsPools(operations, { sort: 'volume' as never, cursor: null, limit: 10 })).rejects.toThrow(RangeError);
    await expect(fetchHolderRewardsPools(operations, { sort: 'rewards', cursor: null, limit: holderRewardsPageSize + 1 })).rejects.toThrow(RangeError);
  });
});

describe('holder rewards response guards', () => {
  it('passes a healthy page and rejects quarantine, duplicates, bounds and shapes', () => {
    expect(validateHolderRewardsPools(poolsResponse, 50)).toBe(poolsResponse);
    expect(() => validateHolderRewardsPools({ ...poolsResponse, quality: 'quarantined' }, 50)).toThrow('quarantined');
    expect(() => validateHolderRewardsPools(poolsResponse, 1)).toThrow(RangeError);
    const duplicate = { ...poolsResponse, data: { items: [poolsResponse.data.items[0], { ...poolsResponse.data.items[1], poolAddress: poolA.toUpperCase().replace('0X', '0x') }] } };
    expect(() => validateHolderRewardsPools(duplicate, 50)).toThrow('Duplicate');
    expect(() => validateHolderRewardsPools({ ...poolsResponse, data: { items: [{ ...poolsResponse.data.items[0], distributedRaw: '-1' }] } }, 50)).toThrow(TypeError);
    expect(() => validateHolderRewardsPools({ ...poolsResponse, data: { items: [{ ...poolsResponse.data.items[0], holdersPaid: 1.5 }] } }, 50)).toThrow(TypeError);
    expect(() => validateHolderRewardsPools({ ...poolsResponse, data: { items: [{ ...poolsResponse.data.items[0], createdAt: 'yesterday' }] } }, 50)).toThrow(TypeError);
    expect(() => validateHolderRewardsPools({ ...poolsResponse, data: { items: [{ ...poolsResponse.data.items[0], poolAddress: '0x12' }] } }, 50)).toThrow(TypeError);
  });

  it('guards the stats envelope the same way', () => {
    expect(validateHolderRewardsStats(statsResponse)).toBe(statsResponse);
    expect(() => validateHolderRewardsStats({ ...statsResponse, data: { ...statsResponse.data, watermark: { ...watermark, integrity: 'quarantined' } } })).toThrow('quarantined');
    expect(() => validateHolderRewardsStats({ ...statsResponse, data: { ...statsResponse.data, cumulativeDistributedRaw: '1.5' } })).toThrow(TypeError);
    expect(() => validateHolderRewardsStats({ ...statsResponse, data: { ...statsResponse.data, walletsRewarded: -1 } })).toThrow(TypeError);
  });

  it('chunks pools into stats batches of at most 50 unique lowercased addresses', () => {
    const pools = Array.from({ length: 120 }, (_, index) => '0x' + index.toString(16).padStart(40, '0'));
    const chunks = chunkPools([...pools, pools[0].toUpperCase().replace('0X', '0x')]);
    expect(chunks.map(chunk => chunk.length)).toEqual([50, 50, 20]);
    expect(chunks.flat()).toHaveLength(120);
    expect(chunks[0]).toEqual([...chunks[0]].sort());
  });
});

describe('on-chain airdrop balance read', () => {
  it('targets the reviewed chain 125 FeeAccumulator with the getAirdropBalance view', () => {
    expect(feeAccumulatorAddress.toLowerCase()).toBe(CORE_DEPLOYMENT.feeAccumulator.toLowerCase());
    expect(feeAccumulatorAbi[0]).toMatchObject({ name: 'getAirdropBalance', stateMutability: 'view' });
  });

  it('issues exactly one read for the hovered pool and rejects a non-integer answer', async () => {
    const calls: unknown[] = [];
    const reader = { readContract: async (parameters: unknown) => { calls.push(parameters); return 1250000n; } } as unknown as Parameters<typeof readAirdropBalance>[0];
    await expect(readAirdropBalance(reader, poolA)).resolves.toBe(1250000n);
    expect(calls).toHaveLength(1);
    expect(calls[0]).toMatchObject({ address: feeAccumulatorAddress, functionName: 'getAirdropBalance' });
    const broken = { readContract: async () => 'not-a-bigint' } as unknown as Parameters<typeof readAirdropBalance>[0];
    await expect(readAirdropBalance(broken, poolA)).rejects.toThrow(TypeError);
    await expect(readAirdropBalance(reader, '0x12')).rejects.toThrow(TypeError);
  });
});
