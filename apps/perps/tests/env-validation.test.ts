import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseMarketProxyOrigin } from '../src/pages/api/candle/[...path]';
import { parseStatusPageUrl } from '../src/app/shell/Footer';
import { COLLATERAL_TOKENS, PERPS_MARKETS, coinLogoUrl } from '../src/core/perps/config';

const ROOT = fileURLToPath(new URL('..', import.meta.url));
const REPO = path.join(ROOT, '../..');

function filesUnder(rel: string): string[] {
  return readdirSync(path.join(ROOT, rel)).flatMap((name) => {
    const child = path.join(rel, name);
    return statSync(path.join(ROOT, child)).isDirectory() ? filesUnder(child) : [child];
  });
}

const MALFORMED = [
  'https://status.example.com/',
  'https://status.example.com/path',
  'https://status.example.com?x=1',
  'https://status.example.com#frag',
  'https://user:pw@status.example.com',
  'ftp://status.example.com',
  'not a url',
];

test('MARKET_PROXY_ORIGIN accepts a bare http(s) origin and refuses anything else naming the variable', () => {
  assert.equal(parseMarketProxyOrigin('https://perps.example.com'), 'https://perps.example.com');
  assert.equal(parseMarketProxyOrigin('http://localhost:3000'), 'http://localhost:3000');
  assert.throws(() => parseMarketProxyOrigin(undefined), /^Error: MARKET_PROXY_ORIGIN is not set$/);
  assert.throws(() => parseMarketProxyOrigin(''), /^Error: MARKET_PROXY_ORIGIN is not set$/);
  for (const raw of MALFORMED) assert.throws(() => parseMarketProxyOrigin(raw), /MARKET_PROXY_ORIGIN/, raw);
  assert.throws(() => parseMarketProxyOrigin('https://perps.example.com/'), /no trailing slash/);
});

test('NEXT_PUBLIC_STATUS_PAGE_URL is optional, https only, and refuses a malformed value naming the variable', () => {
  assert.equal(parseStatusPageUrl(undefined), null);
  assert.equal(parseStatusPageUrl(''), null);
  assert.equal(parseStatusPageUrl('https://status.example.com'), 'https://status.example.com');
  for (const raw of [...MALFORMED, 'http://status.example.com']) {
    assert.throws(() => parseStatusPageUrl(raw), /NEXT_PUBLIC_STATUS_PAGE_URL/, raw);
  }
});

test('a malformed value refuses module load', () => {
  const cases: [string, string, string][] = [
    ['src/pages/api/candle/[...path].ts', 'MARKET_PROXY_ORIGIN', 'https://perps.example.com/'],
    ['src/app/shell/Footer.tsx', 'NEXT_PUBLIC_STATUS_PAGE_URL', 'https://status.example.com/'],
  ];
  for (const [file, name, value] of cases) {
    const env: NodeJS.ProcessEnv = { ...process.env, [name]: value };
    delete env.NODE_TEST_CONTEXT;
    const child = spawnSync(process.execPath, ['--import', 'tsx', '-e', `import(${JSON.stringify(`./${file}`)})`], {
      cwd: ROOT, env, encoding: 'utf8',
    });
    assert.notEqual(child.status, 0, `${file} loaded with ${name}=${value}`);
    assert.match(`${child.stdout}${child.stderr}`, new RegExp(`${name} must be a bare origin`));
  }
});

test('MARKET_PROXY_ORIGIN is declared in the production example and the Railway web service variables', () => {
  const example = readFileSync(path.join(ROOT, 'deploy/.env.production.example'), 'utf8').split('\n');
  assert.ok(example.includes('MARKET_PROXY_ORIGIN='));
  const kvx = readFileSync(path.join(REPO, 'deploy/railway-perps-project.kvx'), 'utf8');
  const web = kvx.slice(kvx.indexOf('[vars.web]')).split(/\n\[/)[0];
  assert.match(web, /^MARKET_PROXY_ORIGIN = "https:\/\/\$\{\{RAILWAY_PUBLIC_DOMAIN\}\}"$/m);
});

test('env template names no retired or deny-listed host', () => {
  const lines = readFileSync(path.join(ROOT, 'src/env/.env.template'), 'utf8').split('\n');
  assert.ok(lines.includes('EXPLORER_URL=https://paxscan.io'));
  assert.ok(lines.includes('RPC_URL='));
  const text = lines.join('\n');
  assert.equal(text.includes('stats.paxscan.io'), false);
  assert.equal(text.includes('paxscan.paxeer.app'), false);
});

test('every configured market and collateral token has a local logo file', () => {
  const symbols = [...PERPS_MARKETS.map((m) => m.symbol), ...Object.values(COLLATERAL_TOKENS).map((t) => t.symbol)];
  for (const symbol of symbols) {
    const url = coinLogoUrl(symbol);
    assert.match(url, /^\/market-logos\/[a-z0-9]+\.(png|jpg|webp)$/, symbol);
    const file = path.join(ROOT, 'public', url);
    assert.ok(existsSync(file) && statSync(file).size > 0, `${symbol}: ${url} missing`);
  }
});

test('no src file references raw.githubusercontent.com and the dead streaming shim is gone', () => {
  for (const file of filesUnder('src')) {
    assert.equal(readFileSync(path.join(ROOT, file), 'utf8').includes('raw.githubusercontent.com'), false, file);
  }
  assert.equal(existsSync(path.join(ROOT, 'src/utils/streaming.js')), false);
});

function runRegister(env: Record<string, string>) {
  const childEnv: NodeJS.ProcessEnv = { ...process.env, ...env };
  delete childEnv.NODE_TEST_CONTEXT;
  return spawnSync(process.execPath, ['--import', 'tsx', '-e', `import('./src/instrumentation.ts').then((m) => (m.default ?? m).register())`], {
    cwd: ROOT, env: childEnv, encoding: 'utf8',
  });
}

test('instrumentation register() fails the boot on a malformed MARKET_PROXY_ORIGIN naming the variable', () => {
  const child = runRegister({ MARKET_PROXY_ORIGIN: 'https://perps.example.com/' });
  assert.notEqual(child.status, 0);
  assert.match(`${child.stdout}${child.stderr}`, /MARKET_PROXY_ORIGIN must be a bare origin/);
});

test('instrumentation register() returns against a valid env', () => {
  const child = runRegister({ MARKET_PROXY_ORIGIN: 'https://perps.example.com', NEXT_PUBLIC_STATUS_PAGE_URL: 'https://status.example.com' });
  assert.equal(child.status, 0, `${child.stdout}${child.stderr}`);
});
