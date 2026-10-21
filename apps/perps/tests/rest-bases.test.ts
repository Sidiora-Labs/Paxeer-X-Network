import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = fileURLToPath(new URL('..', import.meta.url));
const ADDRESS = '0xAbCdEf0123456789aBcDeF0123456789AbCdEf01';
const LEGACY_BASES = ['INDEXER_BASE_URL', 'SUBGRAPH_PROXY_BASE', 'GRAPHQL_URL', 'SUBGRAPH_WS_URL', 'REST_API_URL', 'WS_URL'] as const;

interface Recorded {
  method: string;
  url: string;
  body: string;
}

let server: Server;
let origin = '';
let wsOrigin = '';
const requests: Recorded[] = [];
let modules: Awaited<ReturnType<typeof load>>;

async function load() {
  return {
    gateway: await import('../src/core/network/gateway'),
    config: await import('../src/core/perps/config'),
    chain: await import('../src/core/chain'),
    spot: await import('../src/core/spot/api'),
    perfdb: await import('../src/core/perfdb/client'),
    graphql: await import('../src/core/graphql/client'),
    net: await import('../src/core/net/fetch'),
  };
}

function read(rel: string): string {
  return readFileSync(path.join(ROOT, rel), 'utf8');
}

function sources(dir: string): string[] {
  return readdirSync(path.join(ROOT, dir)).flatMap((name) => {
    const rel = path.join(dir, name);
    if (statSync(path.join(ROOT, rel)).isDirectory()) return sources(rel);
    return /\.(ts|tsx)$/.test(name) ? [rel] : [];
  });
}

function take(): Recorded[] {
  return requests.splice(0);
}

before(async () => {
  server = createServer((req, res) => {
    let body = '';
    req.on('data', (chunk) => {
      body += chunk;
    });
    req.on('end', () => {
      requests.push({ method: req.method ?? '', url: req.url ?? '', body });
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify(req.url === '/graphql' ? { data: { markets: [] } } : { address: ADDRESS.toLowerCase() }));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address() as AddressInfo;
  origin = `http://127.0.0.1:${port}`;
  wsOrigin = `ws://127.0.0.1:${port}`;
  process.env.NEXT_PUBLIC_PERPS_GATEWAY_URL = origin;
  modules = await load();
});

after(async () => {
  server.closeAllConnections();
  await new Promise((resolve) => server.close(resolve));
});

test('legacyBasesRemoved', () => {
  const { config, gateway } = modules;
  const exported = Object.keys(config);
  for (const name of LEGACY_BASES) {
    assert.equal(exported.includes(name), false, `${name} is still exported`);
    assert.equal(new RegExp(`\\b${name}\\b`).test(read('src/core/perps/config.ts')), false, `${name} is still named`);
  }
  assert.equal(exported.includes('DIAMOND_ADDRESS'), false);
  assert.ok(read('src/core/net/venueStream.ts').includes('`${gatewayUrls().restApiUrl}/orderbook/${topic.symbol}?depth=${RESYNC_DEPTH}`'));
  const resync = new URL(`${gateway.gatewayUrls().restApiUrl}/orderbook/BTC`);
  assert.equal(resync.href, `${origin}/api/orderbook/BTC`);
});

test('spotUserRoutesOnGateway', async () => {
  const { spot } = modules;
  assert.equal(spot.spotApi.base, origin);
  take();
  const holdings = await spot.spotApi.getHoldings(ADDRESS);
  assert.equal(holdings.address, ADDRESS.toLowerCase());
  await spot.spotApi.getPositions(ADDRESS);
  await spot.spotApi.getOrders(ADDRESS, { status: 'open', limit: 25 });
  await spot.spotApi.getTrades(ADDRESS, { limit: 10, afterBlock: '120', afterIndex: 3 });
  await spot.spotApi.getTradesStats(ADDRESS);
  await spot.spotApi.getClosedLegs(ADDRESS, { limit: 5, afterId: 9 });
  await spot.spotApi.getPnlSummary(ADDRESS);
  await spot.spotApi.getPnl(ADDRESS);
  await spot.spotApi.getIdentity(ADDRESS);
  await spot.spotApi.getLeaderboard('win-rate');
  const user = `/v1/users/${ADDRESS.toLowerCase()}`;
  assert.deepEqual(
    take().map((r) => `${r.method} ${r.url}`),
    [
      `GET ${user}/holdings`,
      `GET ${user}/positions`,
      `GET ${user}/orders?status=open&limit=25`,
      `GET ${user}/trades?limit=10&afterBlock=120&afterIndex=3`,
      `GET ${user}/trades/stats`,
      `GET ${user}/closed-legs?limit=5&afterId=9`,
      `GET ${user}/pnl/summary`,
      `GET ${user}/pnl`,
      `GET ${user}/identity`,
      'GET /v1/leaderboards/win-rate',
    ],
  );
});

test('perfdbOnGateway', () => {
  const { perfdb, gateway } = modules;
  assert.equal(perfdb.PERFDB_URL, origin);
  assert.equal(perfdb.PERFDB_URL, gateway.gatewayUrls().perfdbOrigin);
  assert.equal(perfdb.PERFDB_GRAPHQL.url, `${origin}/graphql`);
  assert.equal(perfdb.PERFDB_GRAPHQL.wsUrl, `${wsOrigin}/graphql`);
  assert.equal(perfdb.PERFDB_GRAPHQL.name, 'perfdb');
  assert.equal(perfdb.PERFDB_GRAPHQL.authHeaders, perfdb.getAuthHeaders);
});

test('perfdbRoutesResolveOnGateway', async () => {
  const { perfdb, net } = modules;
  const callers: Array<[string, string]> = [
    ['src/hooks/perps/usePerfdbAuth.ts', '"/v2/auth/challenge"'],
    ['src/hooks/perps/usePerfdbAuth.ts', '"/v2/auth/verify"'],
    ['src/hooks/perps/usePerfdbAuth.ts', '`${PERFDB_URL}/v2/auth/refresh`'],
    ['src/hooks/perps/usePerfdbAlerts.ts', '"/v2/alerts"'],
    ['src/hooks/perps/usePerfdbAlerts.ts', '`/v2/alerts/${encodeURIComponent(id)}`'],
    ['src/hooks/perps/usePerfdbReferrals.ts', '"/v2/referrals/generate"'],
    ['src/hooks/perps/usePerfdbReferrals.ts', '"/v2/referrals/apply"'],
    ['src/hooks/perps/usePerfdbCompetitions.ts', '`/v2/competitions/${encodeURIComponent(compId)}/join`'],
    ['src/hooks/perps/useDailyPnl.ts', '`/v2/user/${addr}/daily-pnl`'],
    ['src/page-modules/leaderboard/index.tsx', '"/v2/leaderboard?metric=pnl&period=all&limit=200"'],
  ];
  for (const [file, route] of callers) assert.ok(read(file).includes(route), `${file} no longer requests ${route}`);

  take();
  const lower = ADDRESS.toLowerCase();
  await perfdb.perfdbRest('/v2/auth/challenge', { method: 'POST', body: JSON.stringify({ address: lower }) });
  await perfdb.perfdbRest('/v2/auth/verify', { method: 'POST', body: JSON.stringify({ address: lower, signature: '0x01', message: 'm' }) });
  await net.fetchWithPolicy(`${perfdb.PERFDB_URL}/v2/auth/refresh`, { method: 'POST' });
  await perfdb.perfdbRestAuth('/v2/alerts', { method: 'POST', body: JSON.stringify({ alertType: 'price', marketId: 0, threshold: 1 }) });
  await perfdb.perfdbRestAuth(`/v2/alerts/${encodeURIComponent('a/1')}`, { method: 'DELETE' });
  await perfdb.perfdbRestAuth('/v2/referrals/generate', { method: 'POST' });
  await perfdb.perfdbRestAuth('/v2/referrals/apply', { method: 'POST', body: JSON.stringify({ code: 'PAX' }) });
  await perfdb.perfdbRestAuth(`/v2/competitions/${encodeURIComponent('c1')}/join`, { method: 'POST' });
  await perfdb.perfdbRest(`/v2/user/${lower}/daily-pnl`);
  await perfdb.perfdbRest('/v2/leaderboard?metric=pnl&period=all&limit=200');
  assert.deepEqual(
    take().map((r) => `${r.method} ${r.url}`),
    [
      'POST /v2/auth/challenge',
      'POST /v2/auth/verify',
      'POST /v2/auth/refresh',
      'POST /v2/alerts',
      'DELETE /v2/alerts/a%2F1',
      'POST /v2/referrals/generate',
      'POST /v2/referrals/apply',
      'POST /v2/competitions/c1/join',
      `GET /v2/user/${lower}/daily-pnl`,
      'GET /v2/leaderboard?metric=pnl&period=all&limit=200',
    ],
  );
});

test('indexerGraphqlOnGateway', async () => {
  const { config, gateway, graphql } = modules;
  assert.equal(config.INDEXER_GRAPHQL.url, `${origin}/graphql`);
  assert.equal(config.INDEXER_GRAPHQL.wsUrl, `${wsOrigin}/graphql`);
  assert.deepEqual(config.INDEXER_GRAPHQL, { url: gateway.gatewayUrls().graphql.url, wsUrl: gateway.gatewayUrls().graphql.wsUrl, name: 'indexer' });
  take();
  const query = 'query GetMarkets { markets { marketId } }';
  assert.deepEqual(await graphql.gqlQuery(config.INDEXER_GRAPHQL, query), { markets: [] });
  const sent = take();
  assert.equal(sent.length, 1);
  assert.equal(`${sent[0]!.method} ${sent[0]!.url}`, 'POST /graphql');
  assert.deepEqual(JSON.parse(sent[0]!.body), { query });
  assert.deepEqual(Object.keys(config).filter((name) => name.startsWith('SUBGRAPH_')), []);
  assert.equal(/\bSUBGRAPH_\w*/.test(read('src/core/perps/config.ts')), false);
});

test('explorerUrlFromChain', () => {
  const { config, chain } = modules;
  assert.equal(config.EXPLORER_URL, chain.paxeerChain.blockExplorers.default.url);
  assert.equal(read('src/core/perps/config.ts').includes('EXPLORER_URL = paxeerChain.blockExplorers.default.url;'), true);
  const presence = read('src/hooks/presence/usePresence.ts');
  assert.equal(presence.match(/fetch\("\/api\/presence"/g)?.length, 2);
});

test('onlyOrderbookVenueRoute', () => {
  const routes: string[] = [];
  const originApi: string[] = [];
  for (const file of sources('src')) {
    const text = read(file);
    for (const match of text.matchAll(/restApiUrl\}([^`]*)`/g)) routes.push(`${file} ${match[1]}`);
    for (const match of text.matchAll(/(?:\.origin|perfdbOrigin|spotApiBase|PERFDB_URL|SPOT_API_BASE)\}(\/api\/[^`]*)`/g)) originApi.push(`${file} ${match[1]}`);
  }
  assert.deepEqual(routes, [
    'src/core/net/venueStream.ts /orderbook/${topic.symbol}?depth=${RESYNC_DEPTH}',
    'src/hooks/perps/useSidPaxMarketStream.ts /orderbook/${symbol}?depth=${RESYNC_DEPTH}',
  ]);
  assert.deepEqual(originApi, []);
});
