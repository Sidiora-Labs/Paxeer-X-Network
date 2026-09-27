// @vitest-environment jsdom

import React from 'react';

import * as blockMock from 'mocks/blocks/block';
import * as dailyTxsMock from 'mocks/stats/daily_txs';
import * as statsMock from 'mocks/stats/index';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { screen } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_STATS_API_HOST: '',
    NEXT_PUBLIC_IS_ACCOUNT_SUPPORTED: 'false',
    NEXT_PUBLIC_MARKETPLACE_CONFIG_URL: '',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import Stats from './Stats';

// jsdom implements <path> as SVGElement and leaves the geometry methods of SVGGeometryElement out,
// so the chart line animation of the toolkit has nothing to measure without this shim.
if (!('getTotalLength' in SVGElement.prototype)) {
  Object.defineProperty(SVGElement.prototype, 'getTotalLength', { configurable: true, value: () => 0 });
}

const mockHomeApi = () => {
  fetchMock.mockResponse((request) => {
    const body = (() => {
      if (request.url.includes('/api/v2/main-page/blocks')) {
        return [ blockMock.base, blockMock.base2 ];
      }

      if (request.url.includes('/api/v2/stats/charts/transactions')) {
        return dailyTxsMock.base;
      }

      if (request.url.includes('/api/v2/stats')) {
        return statsMock.base;
      }

      return {};
    })();

    return { body: JSON.stringify(body), headers: { 'Content-Type': 'application/json' } };
  });
};

vi.setConfig({ testTimeout: 60_000 });

describe('Stats', () => {
  beforeEach(() => {
    routerState.pathname = '/';
    routerState.query = {};
    fetchMock.resetMocks();
    mockHomeApi();
  });

  it('divides the card into the coin, the chain and the history columns', async() => {
    const { container } = render(<Stats/>);

    await screen.findByText('$0.001997');

    expect(Array.from(container.querySelectorAll('[data-label^="home-stats-"]')).map((item) => item.getAttribute('data-label')))
      .toEqual([ 'home-stats-coin', 'home-stats-chain', 'home-stats-history' ]);
  });

  it('puts the coin price with its change above the market cap', async() => {
    const { container } = render(<Stats/>);

    await screen.findByText('$0.001997');

    const column = container.querySelector('[data-label="home-stats-coin"]') as HTMLElement;

    expect(Array.from(column.querySelectorAll('[data-highlight]')).map((item) => item.getAttribute('data-highlight')))
      .toEqual([ 'coin_price', 'market_cap' ]);
    expect(column.querySelector('[data-highlight="coin_price"] [data-label]')?.textContent).toBe('ETH price');
    expect(column.querySelector('[data-highlight="coin_price"] [data-delta="down"]')?.textContent).toBe('(-7.42%)');
    expect(column.querySelector('[data-highlight="market_cap"] [data-value]')?.textContent).toBe('$330,809.96');
  });

  it('puts the transaction count with its rate beside the latest block with its block time', async() => {
    const { container } = render(<Stats/>);

    await screen.findByText('82.26M');

    const column = container.querySelector('[data-label="home-stats-chain"]') as HTMLElement;

    expect(column.querySelector('[data-highlight="total_txs"] [data-label]')?.textContent).toBe('Transactions');
    expect(column.querySelector('[data-highlight="total_txs"] [data-secondary]')?.textContent).toBe('(0.3 TPS)');
    expect(column.querySelector('[data-highlight="total_blocks"] [data-value]')?.textContent).toBe('30,146,364');
    expect(column.querySelector('[data-highlight="total_blocks"] [data-secondary]')?.textContent).toBe('(6.2s)');
  });

  it('titles the history column with the last fourteen days', async() => {
    const { container } = render(<Stats/>);

    await screen.findByText('$0.001997');

    const column = container.querySelector('[data-label="home-stats-history"]') as HTMLElement;

    expect(column.querySelector('[data-title]')?.textContent).toBe('Blockscout transaction history in 14 days');
  });
});
