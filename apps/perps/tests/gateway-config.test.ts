import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const GATEWAY = 'https://gateway.example.test';
const MODULE_PATH = fileURLToPath(new URL('../src/core/network/gateway.ts', import.meta.url));

async function loadGateway(value: string) {
  process.env.NEXT_PUBLIC_PERPS_GATEWAY_URL = value;
  return import('../src/core/network/gateway');
}

function loadInChild(value: string | undefined) {
  const env = { ...process.env };
  if (value === undefined) delete env.NEXT_PUBLIC_PERPS_GATEWAY_URL;
  else env.NEXT_PUBLIC_PERPS_GATEWAY_URL = value;
  return spawnSync(
    process.execPath,
    ['--import', 'tsx', '-e', `require(${JSON.stringify(MODULE_PATH)});`],
    { env, encoding: 'utf8' },
  );
}

test('restBaseIsGateway', async () => {
  const { gatewayUrls } = await loadGateway(GATEWAY);
  const urls = gatewayUrls();
  assert.equal(urls.origin, GATEWAY);
  assert.equal(urls.restApiUrl, `${GATEWAY}/api`);
  assert.equal(new URL(`${urls.restApiUrl}/orderbook/BTC?depth=20`).href, `${GATEWAY}/api/orderbook/BTC?depth=20`);
  assert.equal(urls.venueWsUrl, 'wss://gateway.example.test/ws');
  assert.ok(Object.isFrozen(urls));
});

test('spotUserRoutesOnGateway', async () => {
  const { gatewayUrls } = await loadGateway(GATEWAY);
  const urls = gatewayUrls();
  assert.equal(urls.spotApiBase, GATEWAY);
  const holdings = new URL(`${urls.spotApiBase}/v1/users/0xabc/holdings`);
  assert.equal(holdings.origin, GATEWAY);
  assert.equal(holdings.pathname, '/v1/users/0xabc/holdings');
  assert.equal(urls.spotUserWsUrl, 'wss://gateway.example.test/v1/ws');
});

test('perfdbOnGateway', async () => {
  const { gatewayUrls } = await loadGateway(GATEWAY);
  const urls = gatewayUrls();
  assert.equal(urls.perfdbOrigin, GATEWAY);
  assert.equal(new URL(`${urls.perfdbOrigin}/v2/auth/challenge`).pathname, '/v2/auth/challenge');
  assert.equal(new URL(`${urls.perfdbOrigin}/v2/user/0xabc/daily-pnl`).origin, GATEWAY);
});

test('indexerGraphqlOnGateway', async () => {
  const { gatewayUrls } = await loadGateway(GATEWAY);
  const { graphql } = gatewayUrls();
  assert.equal(graphql.url, `${GATEWAY}/graphql`);
  assert.equal(graphql.wsUrl, 'wss://gateway.example.test/graphql');
  assert.ok(Object.isFrozen(graphql));
});

test('httpGatewayUsesWsScheme', () => {
  const result = spawnSync(
    process.execPath,
    [
      '--import',
      'tsx',
      '-e',
      `process.stdout.write(JSON.stringify(require(${JSON.stringify(MODULE_PATH)}).gatewayUrls()));`,
    ],
    { env: { ...process.env, NEXT_PUBLIC_PERPS_GATEWAY_URL: 'http://gateway.local.test:4000/' }, encoding: 'utf8' },
  );
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(JSON.parse(result.stdout), {
    origin: 'http://gateway.local.test:4000',
    restApiUrl: 'http://gateway.local.test:4000/api',
    spotApiBase: 'http://gateway.local.test:4000',
    perfdbOrigin: 'http://gateway.local.test:4000',
    graphql: { url: 'http://gateway.local.test:4000/graphql', wsUrl: 'ws://gateway.local.test:4000/graphql' },
    venueWsUrl: 'ws://gateway.local.test:4000/ws',
    spotUserWsUrl: 'ws://gateway.local.test:4000/v1/ws',
  });
});

test('missingGatewayFails', () => {
  const cases: Array<[string | undefined, string]> = [
    [undefined, 'NEXT_PUBLIC_PERPS_GATEWAY_URL is not set'],
    ['', 'NEXT_PUBLIC_PERPS_GATEWAY_URL is not set'],
    ['not a url', 'NEXT_PUBLIC_PERPS_GATEWAY_URL is not a valid URL'],
    ['ftp://gateway.example.test', 'NEXT_PUBLIC_PERPS_GATEWAY_URL must use http or https'],
    ['wss://gateway.example.test', 'NEXT_PUBLIC_PERPS_GATEWAY_URL must use http or https'],
    ['https://gateway.example.test/api', 'NEXT_PUBLIC_PERPS_GATEWAY_URL must be a bare origin'],
    ['https://gateway.example.test/?x=1', 'NEXT_PUBLIC_PERPS_GATEWAY_URL must be a bare origin'],
    ['https://user:pw@gateway.example.test', 'NEXT_PUBLIC_PERPS_GATEWAY_URL must be a bare origin'],
  ];
  for (const [value, message] of cases) {
    const result = loadInChild(value);
    assert.notEqual(result.status, 0, `module loaded with ${String(value)}`);
    assert.ok(result.stderr.includes(message), `expected "${message}" for ${String(value)}, got: ${result.stderr}`);
  }
});
