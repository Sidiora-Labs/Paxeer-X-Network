import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { QueryClient } from '@tanstack/react-query';
import { CapabilityUnavailableError, HttpError, Operations, SdkClient, decodeV2Contract } from '@kindle/sdk';
import { serveRecorded, v2Reads, type Replay } from '@/core/test-fixtures/market-production/v2-reads';
import { fetchPoolDetailListing, fetchPoolListings, poolListing, poolSpotPrice, type Pool } from './pool-listings';
import { fetchPlatformMetricsV2, PLATFORM_WINDOW } from './platform-metrics';
import { toUnsupported } from './unsupported';

const { real, realToken, example, stateless, unknown } = v2Reads.pools;
const recorded = v2Reads.responses;
let replay: Replay;

beforeAll(async () => { replay = await serveRecorded(Object.values(recorded)); });
afterAll(async () => { await replay.close(); });

function operations(): Operations {
  // production:false only because the replay server listens on plain http.
  return new Operations(new SdkClient({ apiOrigin: replay.origin, identityOrigin: replay.origin, mediaOrigin: replay.origin, streamOrigin: 'ws://127.0.0.1:1', approvedStreamOrigins: ['ws://127.0.0.1:1'], production: false }));
}
const since = () => { const start = replay.requests.length; return () => replay.requests.slice(start); };

describe('recorded core/api v2 read bytes', () => {
  it('validate against the shared v2 contract', () => {
    expect(decodeV2Contract('RankingsResponse', recorded.rankingsTrending.body).data.snapshot.items.map(item => item.poolAddress)).toEqual([real, example]);
    expect(decodeV2Contract('RankingsResponse', recorded.rankingsNew.body).data.snapshot.category).toBe('new');
    expect(decodeV2Contract('StatsBatchResponse', recorded.statsBatch.body).data.items.map(item => item.poolAddress)).toEqual([example, real]);
    expect(decodeV2Contract('PoolDetailResponse', recorded.poolDetailReal.body).data.pool.tokenAddress).toBe(realToken);
    expect(decodeV2Contract('PoolListResponse', recorded.poolListNew.body).data.items).toHaveLength(2);
    expect(decodeV2Contract('PlatformResponse', recorded.platform24h.body).quality).toBe('partial');
    for (const failure of [recorded.statsBatchUnavailable, recorded.poolDetailNotFound, recorded.poolDetailUnavailable, recorded.platform7d]) decodeV2Contract('Error', failure.body);
  });
});

describe('pool listings over IF_POOL_LIST / IF_POOL_DETAIL', () => {
  it('derives the listing and the exact spot price from the served pool', () => {
    const detail = decodeV2Contract('PoolDetailResponse', recorded.poolDetailReal.body).data;
    const pool = detail.pool as Pool;
    expect(poolListing(pool)).toEqual({ poolAddress: real, tokenAddress: realToken, creator: detail.pool.creator, createdAtMs: Date.parse('2025-10-09T08:53:20.000Z') });
    // 1250 USDL effective reserve over 8e8 whole tokens (18 decimals).
    expect(poolSpotPrice(pool, 18)).toBe('0.0000015625');
    expect(poolSpotPrice(pool, null)).toBeNull();
    expect(poolSpotPrice({ ...pool, tokenReserveRaw: '0' }, 18)).toBeNull();
  });

  it('reads one pool detail with its spot price', async () => {
    const seen = since();
    const listing = await fetchPoolDetailListing(operations(), real.toUpperCase().replace('0X', '0x'));
    expect(listing).toMatchObject({ poolAddress: real, tokenAddress: realToken, spotPrice: '0.0000015625' });
    expect(seen()).toEqual([`GET /v2/pools/${real}`]);
  });

  it('seeds the session cache from one IF_POOL_LIST page and reads no detail', async () => {
    const qc = new QueryClient();
    const seen = since();
    const listings = await fetchPoolListings(qc, operations(), replay.origin, [example, real], undefined, 'new');
    expect(Object.keys(listings).sort()).toEqual([example, real]);
    expect(listings[real].tokenAddress).toBe(realToken);
    expect(seen()).toEqual(['GET /v2/pools?sort=new&limit=100']);
    // A second read is answered from the session cache.
    await fetchPoolListings(qc, operations(), replay.origin, [real], undefined, 'new');
    expect(seen()).toEqual(['GET /v2/pools?sort=new&limit=100']);
  });

  it('falls back to IF_POOL_DETAIL per pool and leaves an unserved pool out', async () => {
    const qc = new QueryClient();
    const seen = since();
    // core/api answers the volume list 503 here: its ranking references a pool without reserves.
    const listings = await fetchPoolListings(qc, operations(), replay.origin, [real, stateless], undefined, 'volume');
    expect(Object.keys(listings)).toEqual([real]);
    expect(seen()).toEqual(expect.arrayContaining(['GET /v2/pools?sort=volume&limit=100', `GET /v2/pools/${real}`, `GET /v2/pools/${stateless}`]));
  });

  it('throws the real failure when no pool resolves', async () => {
    const failure = await fetchPoolListings(new QueryClient(), operations(), replay.origin, [unknown]).catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(HttpError);
    expect((failure as HttpError).status).toBe(404);
    expect((failure as HttpError).code).toBe('NOT_FOUND');
  });
});

describe('IF_PLATFORM platform metrics', () => {
  it('reads the 24h envelope the footer shows', async () => {
    const seen = since();
    const metrics = await fetchPlatformMetricsV2(operations());
    expect(metrics).toEqual({ window: PLATFORM_WINDOW, volumeUsdlRaw: '147278912', pools: '2', activeWallets: '1', totalMarketCapUsdlRaw: '10098226981', trades: '2', newPools: '1', generatedAt: '2026-10-13T07:12:36.000Z' });
    expect(seen()).toEqual(['GET /v2/platform/metrics?window=24h']);
  });

  it('classifies an unserved window as unsupported', async () => {
    const failure = await fetchPlatformMetricsV2(operations(), undefined, '7d').catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(CapabilityUnavailableError);
    expect(toUnsupported('IF_PLATFORM', failure)).toMatchObject({ state: 'unsupported', operation: 'IF_PLATFORM', status: 501 });
  });
});
