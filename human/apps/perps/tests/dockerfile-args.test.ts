import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const DOCKERFILE = readFileSync(fileURLToPath(new URL('../Dockerfile', import.meta.url)), 'utf8');

function unsetLoop(): string {
  const build = DOCKERFILE.split('\n').filter((line) => /^RUN .*pnpm build$/.test(line));
  assert.equal(build.length, 1, 'Dockerfile needs exactly one RUN step ending in pnpm build');
  const match = /^RUN (for n in .*done); pnpm build$/.exec(build[0]);
  assert.ok(match, 'the pnpm build step does not first unset empty NEXT_PUBLIC_ variables');
  return match[1];
}

function envAfterLoop(vars: Record<string, string>): Map<string, string> {
  const out = execFileSync('sh', ['-c', `${unsetLoop()}; env`], {
    env: { PATH: process.env.PATH ?? '/usr/bin:/bin', ...vars },
    encoding: 'utf8',
  });
  return new Map(
    out
      .split('\n')
      .filter((line) => line.includes('='))
      .map((line) => [line.slice(0, line.indexOf('=')), line.slice(line.indexOf('=') + 1)] as [string, string]),
  );
}

test('emptyNextPublicArgIsUnset', () => {
  const env = envAfterLoop({
    NEXT_PUBLIC_PAXSCAN_BASE_URL: '',
    NEXT_PUBLIC_STATUS_PAGE_URL: '',
  });
  assert.equal(env.has('NEXT_PUBLIC_PAXSCAN_BASE_URL'), false);
  assert.equal(env.has('NEXT_PUBLIC_STATUS_PAGE_URL'), false);
});

test('setNextPublicArgSurvives', () => {
  const env = envAfterLoop({
    NEXT_PUBLIC_PAXSCAN_BASE_URL: 'https://paxscan.io',
    NEXT_PUBLIC_LOG_LEVEL: 'warn',
  });
  assert.equal(env.get('NEXT_PUBLIC_PAXSCAN_BASE_URL'), 'https://paxscan.io');
  assert.equal(env.get('NEXT_PUBLIC_LOG_LEVEL'), 'warn');
});

test('otherVariablesUntouched', () => {
  const env = envAfterLoop({
    NODE_ENV: 'production',
    MARKET_PROXY_ORIGIN: '',
    NEXT_PUBLIC_SENTRY_DSN: '',
  });
  assert.equal(env.get('NODE_ENV'), 'production');
  assert.equal(env.get('MARKET_PROXY_ORIGIN'), '');
  assert.equal(env.has('NEXT_PUBLIC_SENTRY_DSN'), false);
});

test('runnerStageCarriesNoPublicVariables', () => {
  const runner = DOCKERFILE.slice(DOCKERFILE.indexOf('AS runner'));
  assert.equal(/NEXT_PUBLIC_/.test(runner), false, 'runner stage sets a NEXT_PUBLIC_ variable');
});
