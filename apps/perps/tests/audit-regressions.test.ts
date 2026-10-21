import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
const read = (file: string) => readFileSync(new URL('../' + file, import.meta.url), 'utf8');

test('spot direct route returns 404 and navigation has no spot target', async () => {
  process.env.NEXT_PUBLIC_PERPS_GATEWAY_URL ??= 'https://gateway.test';
  delete process.env.NEXT_PUBLIC_RELEASE_SPOT_OPEN;
  const { getServerSideProps } = await import('../src/pages/spot');
  const { NAV_ITEMS, navItems } = await import('../src/core/nav');
  const { marketsModes } = await import('../src/widgets/markets/MarketsHeaderTabs');
  assert.deepEqual(await getServerSideProps(), { notFound: true });
  assert.equal(NAV_ITEMS.some((item) => item.href === '/spot'), false);
  assert.equal(navItems().some((item) => item.href === '/spot'), false);
  assert.deepEqual(marketsModes(), ['perps']);
});
test('realtime prices use the shared market subscription, not interval polling; portfolio totals come from getAccount, not realtime prices (spec 6.2.2)', () => {
  const source = read('src/hooks/perps/useRealtimePrices.ts');
  assert.match(source, /useMarketsSnapshot\(enabled\)/);
  assert.doesNotMatch(source, /setInterval|setTimeout/);
  const portfolio = read('src/hooks/perps/usePortfolioStats.ts');
  assert.match(portfolio, /buildGetAccount\(/);
  assert.match(portfolio, /portfolioFromAccount\(raw\.account, visiblePositions\)/);
  assert.doesNotMatch(portfolio, /useRealtimePrices/);
});
test('image decoding and bulk precache are disabled; only one worker registers', () => {
  const config = read('next.config.js');
  assert.match(config, /unoptimized: true/);
  assert.match(config, /publicExcludes: \['!\*\*\/\*'\]/);
  assert.doesNotMatch(read('public/scripts/register-sw.js'), /serviceWorker\.register\(/);
  assert.match(read('public/service-worker.js'), /registration\.unregister/);
});
