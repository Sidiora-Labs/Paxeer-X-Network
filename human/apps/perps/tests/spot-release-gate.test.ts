import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createElement, isValidElement, type ReactNode } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import type { NextRouter } from 'next/router';

process.env.NEXT_PUBLIC_PERPS_GATEWAY_URL ??= 'https://gateway.test';
delete process.env.NEXT_PUBLIC_RELEASE_SPOT_OPEN;

const CLOSED_HREFS = ['/', '/perps', '/markets', '/portfolio', '/analytics', '/leaderboard', '/referrals', '/explorer'];
const OPEN_HREFS = ['/', '/perps', '/spot', '/markets', '/portfolio', '/analytics', '/leaderboard', '/referrals', '/explorer'];
const SPOT_WIDGETS = [
  'SpotPairHeader',
  'SpotOrderbook',
  'SpotOrderForm',
  'SpotTradesTape',
  'SpotMarketWatchlist',
  'SpotPositionsPanel',
] as const;

function elements(node: ReactNode): { type: unknown; props: Record<string, unknown> }[] {
  if (Array.isArray(node)) return node.flatMap(elements);
  if (!isValidElement<{ children?: ReactNode }>(node)) return [];
  return [{ type: node.type, props: node.props as Record<string, unknown> }, ...elements(node.props.children)];
}

test('spot_route_follows_release_gate', async () => {
  const nav = await import('../src/core/nav');
  const tabs = await import('../src/widgets/markets/MarketsHeaderTabs');
  const page = await import('../src/pages/spot');
  const widgets = await import('../src/widgets/spot');

  assert.equal(nav.isSpotOpen(), false);
  for (const value of ['', '0', 'true', ' 1', '1 ']) assert.equal(nav.isSpotOpen(value), false);
  assert.deepEqual(await page.getServerSideProps({ query: {} }), { notFound: true });
  assert.deepEqual(await page.getServerSideProps({ query: { market: 'PAX' } }), { notFound: true });
  assert.deepEqual(nav.navItems().map((i) => i.href), CLOSED_HREFS);
  assert.deepEqual(nav.NAV_ITEMS.map((i) => i.href), CLOSED_HREFS);
  assert.deepEqual(tabs.marketsModes(), ['perps']);

  process.env.NEXT_PUBLIC_RELEASE_SPOT_OPEN = '1';
  try {
    assert.equal(nav.isSpotOpen(), true);
    assert.deepEqual(await page.getServerSideProps({ query: {} }), { props: { symbol: 'SID' } });
    assert.deepEqual(await page.getServerSideProps({ query: { market: 'pax' } }), { props: { symbol: 'PAX' } });
    assert.deepEqual(await page.getServerSideProps({ query: { market: 'NONE' } }), { props: { symbol: 'SID' } });
    assert.deepEqual(await page.getServerSideProps({ query: { market: ['PAX', 'SID'] } }), {
      props: { symbol: 'SID' },
    });
    assert.deepEqual(nav.navItems().map((i) => i.href), OPEN_HREFS);
    assert.deepEqual(nav.navItems().find((i) => i.href === '/spot'), {
      href: '/spot',
      label: 'Spot',
      surfaces: ['desktopPro', 'mobileDrawer'],
      group: 'trade',
    });
    assert.deepEqual(tabs.marketsModes(), ['perps', 'spot']);

    const rendered = elements(page.SpotPage({ symbol: 'PAX' }));
    for (const name of SPOT_WIDGETS) {
      assert.equal(rendered.filter((e) => e.type === widgets[name]).length, 1, `${name} is composed once`);
    }
    const props = (name: (typeof SPOT_WIDGETS)[number]) => rendered.find((e) => e.type === widgets[name])?.props;
    assert.equal(props('SpotPairHeader')?.selectedPair, 'PAX');
    assert.equal(props('SpotOrderbook')?.symbol, 'PAX');
    assert.equal(props('SpotTradesTape')?.symbol, 'PAX');
    assert.equal(props('SpotMarketWatchlist')?.selectedSymbol, 'PAX');
    assert.equal((props('SpotOrderForm')?.market as { symbol: string }).symbol, 'PAX');
  } finally {
    delete process.env.NEXT_PUBLIC_RELEASE_SPOT_OPEN;
  }
});

test('markets_mode_handler_switches_to_spot', async () => {
  const { RouterContext } = await import('next/dist/shared/lib/router-context.shared-runtime');
  const { default: MarketsPage } = await import('../src/page-modules/markets');
  const tabs = await import('../src/widgets/markets/MarketsHeaderTabs');
  const { ErrorBoundary } = await import('../src/app/shell/ErrorBoundary');

  process.env.NEXT_PUBLIC_RELEASE_SPOT_OPEN = '1';
  try {
    const passes: { mode: unknown; activeTab: unknown; boundaries: unknown[] }[] = [];
    function Harness() {
      const tree = MarketsPage();
      const all = elements(tree);
      const header = all.find((e) => e.type === tabs.MarketsHeaderTabs)?.props;
      assert.ok(header, 'markets page composes the header tabs');
      passes.push({
        mode: header.mode,
        activeTab: header.activeTab,
        boundaries: all.filter((e) => e.type === ErrorBoundary).map((e) => e.props.fallbackLabel),
      });
      if (passes.length === 1) (header.onChangeMode as (mode: string) => void)('spot');
      return tree;
    }

    const router = { query: {} } as unknown as NextRouter;
    const html = renderToStaticMarkup(createElement(RouterContext.Provider, { value: router }, createElement(Harness)));

    assert.equal(passes.length, 2);
    assert.deepEqual(passes[0], { mode: 'perps', activeTab: 'markets', boundaries: ['Markets overview'] });
    assert.deepEqual(passes[1], { mode: 'spot', activeTab: 'markets', boundaries: ['Spot Markets'] });
    assert.ok(html.includes('24h Volume'), 'spot markets panel is rendered');
  } finally {
    delete process.env.NEXT_PUBLIC_RELEASE_SPOT_OPEN;
  }
});
