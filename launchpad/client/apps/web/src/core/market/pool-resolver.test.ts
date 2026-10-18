import { afterAll, beforeAll, describe, it, expect } from 'vitest';
import { HttpError, Operations, SdkClient, type V2ContractTypes } from '@kindle/sdk';
import { recordedStats, serveRecorded, v2Reads, type Replay, type ReplayFallback } from '@/core/test-fixtures/market-production/v2-reads';
import { PoolNotFoundError, fetchPoolDetail, resolvePoolIdentity, resolvePoolSnapshot } from './pool-resolver';
import { toUnsupported } from './unsupported';

type MetadataGetResponse = V2ContractTypes['MetadataGetResponse'];
type SearchResponse = V2ContractTypes['SearchResponse'];
type ApiError = V2ContractTypes['Error'];

/**
 * IF_POOL_DETAIL and IF_STATS answer with the bytes core/api's production
 * handlers wrote (core-api-wire.json `v2Reads` / `stats`). IF_SEARCH and
 * IF_METADATA_GET (other services, unchanged here) answer typed contract bodies.
 */
const { real: pool, realToken: token, stateless, unknown } = v2Reads.pools;
const recorded = v2Reads.responses;
const requestId = '0f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01';
const generatedAt = '2026-10-13T07:12:36.000Z';
const position = { chainId: '125', height: '33963716', transactionIndex: '0', logIndex: '2', transactionHash: '0x' + '5'.repeat(64), blockHash: '0x' + '6'.repeat(64) } as const;

const metadata: MetadataGetResponse = { requestId, generatedAt, nextCursor: null, quality: 'complete', revision: '1', watermark: null, data: { description: 'A test market', displayName: 'Test Market', displaySymbol: 'TEST', socialLinks: [], revision: '1', tags: ['test'], token: {
  address: token, decimals: 6, decimalsQuality: 'verified', logoAssetId: null, metadataRevision: '1', name: 'Test Market', sourcePosition: position, supplyQuality: 'verified', symbol: 'TEST', totalSupplyRaw: '1000000000000000',
} } };
const searchToken: SearchResponse = { requestId, generatedAt, nextCursor: null, quality: 'partial', revision: '3', watermark: null, data: { items: [{ id: `125:${pool}`, kind: 'token', label: 'Test Market', poolAddress: pool, tokenAddress: token, wallet: null, score: '1' }] } };
const statelessToken = '0x' + '6'.repeat(40);
const searchStateless: SearchResponse = { ...searchToken, data: { items: [{ id: `125:${stateless}`, kind: 'pool', label: 'Stateless', poolAddress: stateless, tokenAddress: statelessToken, wallet: null, score: '1' }] } };
const searchEmpty: SearchResponse = { ...searchToken, data: { items: [] } };
const envelope = (code: string, retryable = false): ApiError => ({ code, message: code, requestId, retryable, retryAfterMs: null, fieldErrors: [], details: null });

let override: ReplayFallback | null = null;
const otherServices: ReplayFallback = (_method, url) => {
  if (url.pathname === `/v2/tokens/${token}/metadata`) return { status: 200, body: metadata };
  if (url.pathname === `/v2/pools/${stateless}/stats`) return { status: 503, body: envelope('DEPENDENCY_UNAVAILABLE', false) };
  if (url.pathname === '/v2/search') {
    const q = url.searchParams.get('q'), type = url.searchParams.get('type');
    return { status: 200, body: q === token && type === 'token' ? searchToken : q === stateless && type === 'pool' ? searchStateless : searchEmpty };
  }
  return undefined;
};
let replay: Replay;
beforeAll(async () => { replay = await serveRecorded([recorded.poolDetailReal, recorded.poolDetailNotFound, recorded.poolDetailUnavailable, recordedStats], otherServices, (method, url, body) => override?.(method, url, body)); });
afterAll(async () => { await replay.close(); });

function harness(route: ReplayFallback | null = null) {
  override = route;
  const start = replay.requests.length;
  // production:false only because the replay server listens on plain http.
  const client = new SdkClient({ apiOrigin: replay.origin, identityOrigin: replay.origin, mediaOrigin: replay.origin, streamOrigin: 'ws://127.0.0.1:1', approvedStreamOrigins: ['ws://127.0.0.1:1'], production: false });
  return { deps: { operations: new Operations(client) }, calls: () => replay.requests.slice(start) };
}

describe('pool resolver over served v2 routes', () => {
  it('reads IF_POOL_DETAIL and marks NOT_FOUND / a not-served pool unavailable instead of throwing', async () => {
    const { deps, calls } = harness();
    expect((await fetchPoolDetail(pool, deps)).value?.data.pool.tokenAddress).toBe(token);
    expect(await fetchPoolDetail(unknown, deps)).toEqual({ value: null, unavailable: { state: 'unsupported', operation: 'IF_POOL_DETAIL', status: 404, message: 'NOT_FOUND' } });
    expect(await fetchPoolDetail(stateless, deps)).toEqual({ value: null, unavailable: { state: 'unsupported', operation: 'IF_POOL_DETAIL', status: 503, message: 'DEPENDENCY_UNAVAILABLE' } });
    expect(calls().filter(call => call === `GET /v2/pools/${pool}` || call === `GET /v2/pools/${unknown}`)).toEqual([`GET /v2/pools/${pool}`, `GET /v2/pools/${unknown}`]);
  });

  it('resolves a pool address through IF_POOL_DETAIL with stats, registry facts and metadata', async () => {
    const { deps, calls } = harness();
    const snapshot = await resolvePoolSnapshot(pool.toUpperCase().replace('0X', '0x'), deps);
    const detail = recorded.poolDetailReal.body as V2ContractTypes['PoolDetailResponse'];
    expect(snapshot.resolvedBy).toBe('detail');
    expect(snapshot.pool).toEqual({ chainId: '125', poolAddress: pool, tokenAddress: token });
    expect(snapshot.stats).toEqual(detail.data.stats);
    expect(snapshot.stats?.volume24hRaw).toBe('147278912');
    expect(snapshot.token?.symbol).toBe('TEST');
    expect(snapshot.metadata?.description).toBe('A test market');
    expect(snapshot.creator).toBe(detail.data.pool.creator);
    expect(snapshot.createdAt).toBe('2025-10-09T08:53:20.000Z');
    expect(snapshot.generatedAt).toBe(detail.generatedAt);
    expect(snapshot.unavailable).toMatchObject({ stats: null, metadata: null, registry: null });
    expect(calls()).toEqual(expect.arrayContaining([`GET /v2/pools/${pool}`, `GET /v2/tokens/${token}/metadata`]));
    expect(calls().some(call => call.startsWith('GET /v2/search') || call.startsWith('GET /bff/') || call.endsWith('/stats'))).toBe(false);
  });

  it('resolves a token address through /v2/search when IF_POOL_DETAIL has no pool at that address', async () => {
    const { deps, calls } = harness();
    const resolved = await resolvePoolIdentity(token, deps);
    expect(resolved.resolvedBy).toBe('search');
    expect(resolved.identity).toEqual({ chainId: '125', poolAddress: pool, tokenAddress: token });
    expect(resolved.detail?.data.pool.poolAddress).toBe(pool);
    expect(calls()).toEqual([`GET /v2/pools/${token}`, `GET /v2/search?q=${token}&type=token&limit=10`, `GET /v2/pools/${pool}`]);
  });

  it('fails with a 404-classified PoolNotFoundError when neither IF_POOL_DETAIL nor search knows the address', async () => {
    const { deps, calls } = harness();
    const failure = await resolvePoolIdentity(unknown, deps).catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(PoolNotFoundError);
    expect(toUnsupported('POOL_RESOLVE', failure)).toEqual({ state: 'unsupported', operation: 'POOL_RESOLVE', status: 404, message: 'POOL_NOT_FOUND' });
    expect(calls()).toEqual([`GET /v2/pools/${unknown}`, `GET /v2/search?q=${unknown}&type=token&limit=10`, `GET /v2/search?q=${unknown}&type=pool&limit=10`]);
  });

  it('resolves a pool whose detail answers 503 through /v2/search and keeps the real status', async () => {
    const { deps, calls } = harness();
    const snapshot = await resolvePoolSnapshot(stateless, deps);
    expect(snapshot.resolvedBy).toBe('search');
    expect(snapshot.pool).toEqual({ chainId: '125', poolAddress: stateless, tokenAddress: statelessToken });
    expect(snapshot.stats).toBeNull();
    expect(snapshot.unavailable.stats).toEqual({ state: 'unsupported', operation: 'IF_STATS', status: 503, message: 'DEPENDENCY_UNAVAILABLE' });
    expect(snapshot.unavailable.registry).toEqual({ state: 'unsupported', operation: 'IF_POOL_DETAIL', status: 503, message: 'DEPENDENCY_UNAVAILABLE' });
    expect(snapshot.creator).toBeNull();
    expect(snapshot.createdAt).toBeNull();
    expect(calls()).toEqual(expect.arrayContaining([`GET /v2/pools/${stateless}`, `GET /v2/search?q=${stateless}&type=pool&limit=10`, `GET /v2/pools/${stateless}/stats`]));
  });

  it('falls back to the detail token when metadata is not served', async () => {
    const { deps } = harness((_method, url) => url.pathname === `/v2/tokens/${token}/metadata` ? { status: 404 } : undefined);
    const snapshot = await resolvePoolSnapshot(pool, deps);
    expect(snapshot.metadata).toBeNull();
    expect(snapshot.token).toMatchObject({ address: token, symbol: 'SIDF' });
    expect(snapshot.unavailable.metadata).toMatchObject({ state: 'unsupported', operation: 'IF_METADATA_GET', status: 404 });
    expect(snapshot.unavailable.stats).toBeNull();
  });

  it('reads IF_STATS from recorded bytes when the pool resolves only through search', async () => {
    const { deps } = harness((_method, url) => url.pathname === `/v2/pools/${pool}` ? { status: 503, body: envelope('DEPENDENCY_UNAVAILABLE', false) } : url.pathname === '/v2/search' && url.searchParams.get('q') === pool && url.searchParams.get('type') === 'pool' ? { status: 200, body: { ...searchToken, data: { items: [{ ...searchToken.data.items[0], kind: 'pool' }] } } } : undefined);
    const snapshot = await resolvePoolSnapshot(pool, deps);
    expect(snapshot.stats).toEqual((recordedStats.body as V2ContractTypes['StatsResponse']).data.stats);
    expect(snapshot.generatedAt).toBe(generatedAt);
  });

  it('rejects a pool detail that belongs to another pool', async () => {
    const other = structuredClone(recorded.poolDetailReal.body) as V2ContractTypes['PoolDetailResponse'];
    other.data.stats.poolAddress = unknown;
    const { deps } = harness((_method, url) => url.pathname === `/v2/pools/${pool}` ? { status: 200, body: other } : undefined);
    await expect(resolvePoolSnapshot(pool, deps)).rejects.toThrow('Pool identity disagreement');
  });

  it('surfaces an IF_POOL_DETAIL server failure as HttpError instead of swallowing it', async () => {
    const { deps } = harness((_method, url) => url.pathname === `/v2/pools/${pool}` ? { status: 500, body: envelope('INTERNAL', false) } : undefined);
    const failure = await resolvePoolIdentity(pool, deps).catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(HttpError);
    expect((failure as HttpError).status).toBe(500);
    expect(toUnsupported('IF_POOL_DETAIL', failure)).toBeNull();
  });
});
