import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import type { DailyPnl } from '../src/hooks/perps/useDailyPnl';

process.env.NEXT_PUBLIC_PERPS_GATEWAY_URL ??= 'https://perps-gateway.invalid';

const day = (date: string, pnl = '1000000000000000000', volume = '2000000000000000000', trades = 1): DailyPnl => ({ date, pnl, volume, trades });
const NOW = new Date(Date.UTC(2026, 9, 21, 23, 59));

test('windowsAreCalendarDaysOnUtcBoundaries', async () => {
  const grid = await import('../src/widgets/portfolio/PortfolioPerformanceGrid');
  assert.equal(grid.windowStartKey('7D', NOW), '2026-10-15');
  assert.equal(grid.windowStartKey('7D', new Date(Date.UTC(2026, 9, 22, 0, 0))), '2026-10-16');
  assert.equal(grid.windowStartKey('30D', NOW), '2026-09-22');
  assert.equal(grid.windowStartKey('90D', NOW), '2026-07-24');
  const rows = [day('2026-06-01'), day('2026-09-20'), day('2026-09-21'), day('2026-10-13'), day('2026-10-14T00:00:00Z'), day('2026-10-21')];
  assert.deepEqual(grid.sliceByRange(rows, '7D', NOW).map(r => r.date), ['2026-10-21']);
  assert.deepEqual(grid.sliceByRange(rows, '30D', NOW).map(r => r.date), ['2026-10-13', '2026-10-14T00:00:00Z', '2026-10-21']);
  assert.equal(grid.sliceByRange(rows, '90D', NOW).length, 5);
  assert.equal(grid.sliceByRange(rows, 'ALL', NOW).length, 6);
});

// Boundary: an N-day window is the last N calendar UTC days including today.
// Clock fixed at 2026-10-21T23:59Z, so 7D is 2026-10-15..2026-10-21 (seven
// day keys) and 2026-10-14 is outside; one minute later, at 2026-10-22T00:00Z,
// today rolls over and 7D becomes 2026-10-16..2026-10-22.
test('sevenDayWindowIsSevenUtcDaysIncludingToday', async () => {
  const grid = await import('../src/widgets/portfolio/PortfolioPerformanceGrid');
  const keys = Array.from({ length: 14 }, (_, i) => new Date(Date.UTC(2026, 9, 9 + i)).toISOString().slice(0, 10));
  assert.equal(keys[0], '2026-10-09');
  assert.equal(keys[13], '2026-10-22');
  const rows = keys.map(k => day(k));
  const week = grid.sliceByRange(rows.slice(0, 13), '7D', NOW).map(r => r.date);
  assert.deepEqual(week, ['2026-10-15', '2026-10-16', '2026-10-17', '2026-10-18', '2026-10-19', '2026-10-20', '2026-10-21']);
  const rolled = grid.sliceByRange(rows, '7D', new Date(Date.UTC(2026, 9, 22, 0, 0))).map(r => r.date);
  assert.deepEqual(rolled, ['2026-10-16', '2026-10-17', '2026-10-18', '2026-10-19', '2026-10-20', '2026-10-21', '2026-10-22']);
  assert.equal(grid.windowTotals(grid.sliceByRange(rows.slice(0, 13), '7D', NOW))!.trades, 7);
});

test('emptyWindowIsNoDataNotZero', async () => {
  const grid = await import('../src/widgets/portfolio/PortfolioPerformanceGrid');
  const stale = [day('2026-01-01'), day('2026-01-02')];
  assert.deepEqual(grid.sliceByRange(stale, '7D', NOW), []);
  assert.equal(grid.windowTotals(grid.sliceByRange(stale, '7D', NOW)), null);
  assert.deepEqual(grid.buildSeries(stale, '7D', NOW), []);
  const html = renderToStaticMarkup(createElement(grid.PortfolioPerformanceGrid, { dailyPnl: [] }));
  assert.equal((html.match(/No data yet/g) ?? []).length, 4);
  assert.doesNotMatch(html, /\$0\.00/);
});

test('bigintSumsDoNotDriftAbove2pow53Wei', async () => {
  const grid = await import('../src/widgets/portfolio/PortfolioPerformanceGrid');
  const big = '9007199254740993';
  const rows = [day('2026-10-20', big, big, 2), day('2026-10-21', big, '1000e+15', 3)];
  const totals = grid.windowTotals(rows)!;
  assert.equal(totals.pnlWei, 18014398509481986n);
  assert.equal(totals.volumeWei, 9007199254740993n + 1_000_000_000_000_000_000n);
  assert.equal(totals.trades, 5);
  assert.notEqual(BigInt(Number(big) + Number(big)), totals.pnlWei);
  const roi = grid.windowTotals([day('2026-10-21', '1000000000000000000', '3000000000000000000')])!;
  assert.equal(roi.roiHundredthsPct, 3333n);
  assert.equal(grid.windowTotals([day('2026-10-21', '5', '0')])!.roiHundredthsPct, null);
});

test('failureAndSkippedDaysAreShown', async () => {
  const grid = await import('../src/widgets/portfolio/PortfolioPerformanceGrid');
  const failed = renderToStaticMarkup(createElement(grid.PortfolioPerformanceGrid, { dailyPnl: [], error: new Error('down') }));
  assert.match(failed, /Performance history is unavailable/);
  assert.equal((failed.match(/Could not load data/g) ?? []).length, 4);
  assert.doesNotMatch(failed, /No data yet/);
  const skipped = renderToStaticMarkup(createElement(grid.PortfolioPerformanceGrid, { dailyPnl: [], skippedDays: 2 }));
  assert.match(skipped, /2 days skipped/);
});
