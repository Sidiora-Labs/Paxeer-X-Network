import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { QueryClient } from '@tanstack/react-query';
import { Operations, SdkClient } from '@kindle/sdk';
import { serveRecorded, v2Reads, type Replay } from '@/core/test-fixtures/market-production/v2-reads';

const { real, realToken, example } = v2Reads.pools;
const recorded = v2Reads.responses;
const exampleToken = '0x' + '3'.repeat(40);
let replay: Replay;
let metadataBatchCalls = 0;

beforeAll(async () => {
  replay = await serveRecorded([recorded.rankingsTrending, recorded.statsBatch, recorded.poolDetailReal, recorded.poolDetailFirst], (method, url) => {
    // media/metadata legacy batch read (unchanged by this migration).
    if (method === 'GET' && url.pathname === '/metadata/batch') {
      metadataBatchCalls++;
      return { status: 200, body: { [realToken]: { token_address: realToken, name: 'Sidiora Fixture', symbol: 'SIDF', images: { logo: null } }, [exampleToken]: null } };
    }
    return undefined;
  });
  process.env.NEXT_PUBLIC_METADATA_API_URL = replay.origin;
});
afterAll(async () => { await replay.close(); });

const sources = () => ({
  // production:false only because the replay server listens on plain http.
  operations: new Operations(new SdkClient({ apiOrigin: replay.origin, identityOrigin: replay.origin, mediaOrigin: replay.origin, streamOrigin: 'ws://127.0.0.1:1', approvedStreamOrigins: ['ws://127.0.0.1:1'], production: false })),
  queryClient: new QueryClient(),
});

describe('meta-ag token universe over IF_RANKINGS + IF_STATS_BATCH', () => {
  it('lists the quote token, then the trending launchpad tokens in rank order from recorded core/api bytes', async () => {
    const { loadMetaAgTokens } = await import('./token-universe');
    const start = replay.requests.length;
    const tokens = await loadMetaAgTokens(undefined, sources());
    expect(tokens[0].kind).toBe('quote');
    expect(tokens.slice(1).map(token => [token.poolAddress, token.tokenAddress, token.symbol])).toEqual([[real, realToken, 'SIDF'], [example, exampleToken, '???']]);
    // The real pool's 2.2.0 projection values it at 10098226981 raw USDL (price x total supply); the frozen example at 1 raw USDL.
    expect(tokens[1].marketCap).toBe(10098.226981);
    expect(tokens[2].marketCap).toBe(0.000001);
    expect(tokens.every(token => token.price === 0)).toBe(true);
    const seen = replay.requests.slice(start);
    expect(seen).toEqual(expect.arrayContaining(['GET /v2/rankings/trending', 'POST /v2/pools/stats:batch', `GET /v2/pools/${real}`, `GET /v2/pools/${example}`]));
    expect(seen.some(path => path.startsWith('GET /rankings/') || path.startsWith('GET /stats/'))).toBe(false);
    expect(metadataBatchCalls).toBe(1);
  });

  it('keeps the quote tokens when the ranking projection is not served', async () => {
    const { loadMetaAgTokens } = await import('./token-universe');
    const empty = await serveRecorded([]);
    try {
      const operations = new Operations(new SdkClient({ apiOrigin: empty.origin, identityOrigin: empty.origin, mediaOrigin: empty.origin, streamOrigin: 'ws://127.0.0.1:1', approvedStreamOrigins: ['ws://127.0.0.1:1'], production: false }));
      const tokens = await loadMetaAgTokens(undefined, { operations, queryClient: new QueryClient() });
      expect(tokens.map(token => token.kind)).toEqual(['quote']);
      expect(empty.requests).toEqual(['GET /v2/rankings/trending']);
    } finally { await empty.close(); }
  });
});
