import { test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = fileURLToPath(new URL('..', import.meta.url));

const DECLARED = [
  'NEXT_PUBLIC_PERPS_GATEWAY_URL',
  'NEXT_PUBLIC_MARKET_PROXY_HOST',
  'NEXT_PUBLIC_PERPS_PRECOMPILE',
  'NEXT_PUBLIC_LAUNCHPAD_PRECOMPILE',
  'NEXT_PUBLIC_PECOR_MARKET_PRECOMPILE',
  'NEXT_PUBLIC_PECOR_ORDERS_PRECOMPILE',
  'NEXT_PUBLIC_PECOR_STOP_PRECOMPILE',
  'NEXT_PUBLIC_PECOR_QUOTER_PRECOMPILE',
  'NEXT_PUBLIC_PECOR_VAULT_PRECOMPILE',
] as const;

const REMOVED = [
  'DIAMOND_ADDRESS',
  'USID_ADDRESS',
  'USDC_ADDRESS',
  'USDT_ADDRESS',
  'NEXT_PUBLIC_INDEXER_URL',
  'NEXT_PUBLIC_INDEXER_WS_URL',
  'NEXT_PUBLIC_PERFDB_URL',
  'NEXT_PUBLIC_SPOT_',
] as const;

const USDL = '0x85FcD13735F4309833A503EE804ea32395851479';

function read(rel: string): string {
  return readFileSync(path.join(ROOT, rel), 'utf8');
}

function lines(rel: string): string[] {
  return read(rel).split('\n');
}

function filesUnder(rel: string): string[] {
  return readdirSync(path.join(ROOT, rel)).flatMap((name) => {
    const child = path.join(rel, name);
    return statSync(path.join(ROOT, child)).isDirectory() ? filesUnder(child) : [child];
  });
}

function dockerfileEnvBlock(): string[] {
  const all = lines('deploy/Dockerfile');
  const start = all.findIndex((line) => line.startsWith('ENV NEXT_PUBLIC_'));
  assert.notEqual(start, -1, 'deploy/Dockerfile has no NEXT_PUBLIC_ ENV list');
  const end = all.findIndex((line, i) => i >= start && !line.trimEnd().endsWith('\\'));
  return all.slice(start, end + 1).map((line) => line.replace(/^ENV\s+/, '').replace(/\\$/, '').trim());
}

test('deployFilesDeclareGateway', () => {
  const declarations: Record<string, (name: string) => boolean> = {
    'src/env/.env.template': (name) => lines('src/env/.env.template').some((l) => l.startsWith(`${name}=`)),
    'deploy/.env.production.example': (name) =>
      lines('deploy/.env.production.example').some((l) => l.startsWith(`${name}=`)),
    'deploy/docker-compose.yml': (name) =>
      lines('deploy/docker-compose.yml').some((l) => l.replace(/^#\s*/, '').trim() === `${name}: \${${name}:-}`),
    'deploy/Dockerfile ARG': (name) => lines('deploy/Dockerfile').some((l) => l === `ARG ${name}=`),
    'deploy/Dockerfile ENV': (name) => dockerfileEnvBlock().includes(`${name}=$${name}`),
    'deploy/README.md': (name) => read('deploy/README.md').includes(`\`${name}\``),
  };
  for (const [file, declares] of Object.entries(declarations)) {
    for (const name of DECLARED) {
      assert.equal(declares(name), true, `${file} does not declare ${name}`);
    }
  }

  const railway = JSON.parse(read('railway.json')) as Record<string, Record<string, unknown>>;
  assert.deepEqual(Object.keys(railway).sort(), ['$schema', 'build', 'deploy']);
  assert.deepEqual(Object.keys(railway.build).sort(), ['buildCommand', 'builder', 'nixpacksPlan']);
  assert.deepEqual(Object.keys(railway.build.nixpacksPlan as object), ['providers']);
  assert.equal(/^\[variables\]/m.test(read('nixpacks.toml')), false, 'nixpacks.toml carries a variable list');
  assert.equal(read('ecosystem.config.js').includes('NEXT_PUBLIC_'), false, 'ecosystem.config.js names a build variable');
});

test('removedVariablesAbsent', () => {
  const scanned = [...filesUnder('src/env'), ...filesUnder('deploy'), 'railway.json', 'nixpacks.toml', 'ecosystem.config.js'];
  assert.ok(scanned.includes(path.join('src/env', '.env.template')));
  assert.ok(scanned.includes(path.join('deploy', 'Dockerfile')));
  for (const file of scanned) {
    const text = read(file);
    for (const name of REMOVED) {
      assert.equal(text.includes(name), false, `${file} still names ${name}`);
    }
  }
  const usdl = lines('src/env/.env.template').filter((l) => l.startsWith('USDL_ADDRESS='));
  assert.deepEqual(usdl, [`USDL_ADDRESS=${USDL}`]);
});

test('deployedJsonRemoved', () => {
  assert.equal(existsSync(path.join(ROOT, 'src/env/deployed.json')), false);
});
