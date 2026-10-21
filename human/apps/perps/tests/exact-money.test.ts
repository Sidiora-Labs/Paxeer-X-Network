import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import type { DailyPnl } from '../src/hooks/perps/useDailyPnl';
import { weiToNumber } from '../src/ui/utils/format';

process.env.NEXT_PUBLIC_PERPS_GATEWAY_URL ??= 'https://perps-gateway.invalid';

const TENTH = '100000000000000000'; // 0.1 USD = 1e17 wei, above 2^53
const ODD = '9007199254740993'; // 2^53 + 1 wei
const day = (date: string, pnl: string, volume = '0', trades = 1): DailyPnl => ({ date, pnl, volume, trades });

function overviewProps(daily: { dailyPnl: DailyPnl[]; skippedDays: number; isLoading: boolean; error: Error | null }, page: typeof import('../src/page-modules/portfolio/index')) {
  return {
    isConnected: true, hasVault: true, address: '0x0000000000000000000000000000000000000001',
    totalVaultUsd: 10, unrealizedPnl: 0, openPositions: 0, realizedPnl: 0, totalTrades: 0, balances: [],
    ...page.overviewDailyPnlProps(daily),
    isLoading: false, privacy: false,
    onTogglePrivacy() {}, onRefresh() {}, onConnect() {}, onDeposit() {}, onWithdraw() {},
  };
}

test('portfolioPageShowsDailyPnlErrorAndSkippedDays', async () => {
  const page = await import('../src/page-modules/portfolio/index');
  const { PortfolioOverviewPanel } = await import('../src/widgets/portfolio/PortfolioOverviewPanel');
  const failed = renderToStaticMarkup(createElement(PortfolioOverviewPanel, overviewProps({ dailyPnl: [], skippedDays: 0, isLoading: false, error: new Error('both reads failed') }, page)));
  assert.match(failed, /Performance history is unavailable/);
  const partial = renderToStaticMarkup(createElement(PortfolioOverviewPanel, overviewProps({ dailyPnl: [day('2026-10-21', TENTH)], skippedDays: 3, isLoading: false, error: null }, page)));
  assert.match(partial, /3 days skipped/);
  assert.doesNotMatch(partial, /Performance history is unavailable/);
});

test('performanceCardsSumWeiExactly', async () => {
  const { sevenDayTotals } = await import('../src/widgets/portfolio/PerformanceCards');
  const rows = [day('2026-10-19', TENTH, ODD), day('2026-10-20', TENTH, ODD), day('2026-10-21', TENTH, ODD)];
  const t = sevenDayTotals(rows);
  assert.equal(t.pnlWei, 300_000_000_000_000_000n);
  assert.equal(t.volumeWei, 3n * 9007199254740993n);
  assert.equal(weiToNumber(t.pnlWei), 0.3);
  assert.notEqual(0.1 + 0.1 + 0.1, 0.3);
  assert.equal(t.prevPnlWei, 0n);
});

test('assetsChartWalksBackInExactWei', async () => {
  const { buildAssetSeries } = await import('../src/widgets/portfolio/PortfolioAssetsChart');
  const rows = [day('2026-10-18', TENTH), day('2026-10-19', TENTH), day('2026-10-20', TENTH), day('2026-10-21', TENTH)];
  const series = buildAssetSeries(rows, 1, 'ALL');
  assert.deepEqual(series.map((p) => p.value), [0.7, 0.8, 0.9, 1]);
  assert.notEqual(1 - 0.1 - 0.1 - 0.1, 0.7);
});

test('leaderboardSortsAndSumsWeiExactly', async () => {
  const lb = await import('../src/page-modules/leaderboard/index');
  assert.equal(lb.normalizeWeiStr('123456789012345678901e+5'), '12345678901234567890100000');
  assert.equal(lb.normalizeWeiStr('108310e+20'), '10831000000000000000000000');
  assert.equal(lb.normalizeWeiStr('bogus'), '0');
  assert.equal(lb.normalizeWeiStr(null), '0');
  assert.equal(lb.compareWei(ODD, '9007199254740992'), 1);
  assert.equal(Number(ODD) - Number('9007199254740992'), 0);
  const entries = [{ totalRealizedPnl: ODD }, { totalRealizedPnl: ODD }, { totalRealizedPnl: '-1' }];
  assert.equal(lb.totalPnlWei(entries), 18014398509481985n);
});

test('feesGroupInExactWei', async () => {
  const { groupFees } = await import('../src/widgets/portfolio/FeesBreakdownCard');
  const groups = groupFees([
    { feeType: 0, feeUsd: ODD },
    { feeType: 0, feeUsd: ODD },
    { feeType: 1, feeUsd: '18014398509481985' },
    { feeType: 2, feeUsd: null },
  ]);
  assert.deepEqual(groups.map((g) => [g.feeType, g.totalWei, g.count]), [[0, 18014398509481986n, 2], [1, 18014398509481985n, 1], [2, 0n, 1]]);
});

test('settlementMagnitudesAreExact', async () => {
  const { absUsd } = await import('../src/widgets/portfolio/TradeSettlementHistory');
  assert.equal(absUsd('-1.5e+20'), 150);
  assert.equal(absUsd('-123456789012345678901234567'), weiToNumber(123456789012345678901234567n));
  assert.equal(absUsd(null), 0);
});

test('portfolioLeverageWeightsInExactWei', async () => {
  const { portfolioFromAccount } = await import('../src/core/perps/portfolio');
  const E18 = 10n ** 18n;
  const totals = portfolioFromAccount({ collateral: 0n, equityUsd: 0n, withdrawable: 0n }, [
    { sizeUsd: ODD, leverage: (3n * E18).toString() },
    { sizeUsd: ODD, leverage: (5n * E18).toString() },
    { sizeUsd: '1e+21', leverage: '4e+18' },
  ]);
  assert.equal(totals.totalLeverage, 4);
});
