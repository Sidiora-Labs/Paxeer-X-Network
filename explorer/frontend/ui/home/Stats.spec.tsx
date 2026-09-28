// @vitest-environment jsdom

import React from 'react';

import type { Block } from 'types/api/block';

import { SocketProvider } from 'lib/socket/context';
import * as blockMock from 'mocks/blocks/block';
import * as dailyTxsMock from 'mocks/stats/daily_txs';
import * as statsMock from 'mocks/stats/index';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { screen } from 'vitest/lib';
import { createTestSocket } from 'vitest/utils/socketServer';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_STATS_API_HOST: '',
    NEXT_PUBLIC_IS_ACCOUNT_SUPPORTED: 'false',
    NEXT_PUBLIC_MARKETPLACE_CONFIG_URL: '',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import { HomeRpcDataContextProvider } from './fallbacks/rpcDataContext';
import LatestBlocks from './LatestBlocks';
import Stats from './Stats';

const TOPIC = 'blocks:new_block';

const socketBlock = (offset: number): Block => ({
  ...blockMock.base,
  height: blockMock.base.height + offset,
  hash: `0x${ (1000 + offset).toString(16).padStart(64, '0') }`,
});

const latestBlockCounter = (container: HTMLElement) => container
  .querySelector('[data-highlight="total_blocks"] [data-value]')?.textContent;

const renderHomeStats = (socketUrl: string, onRender: React.ProfilerOnRenderCallback) => render(
  <SocketProvider url={ socketUrl }>
    <HomeRpcDataContextProvider>
      <React.Profiler id="home-stats" onRender={ onRender }>
        <Stats/>
      </React.Profiler>
      <LatestBlocks/>
    </HomeRpcDataContextProvider>
  </SocketProvider>,
);

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

  it('moves the latest block counter on the flush cadence and leaves the columns beside it alone', async() => {
    const socket = await createTestSocket();

    try {
      const commits: Array<string> = [];
      const { container } = renderHomeStats(socket.url, (id, phase) => {
        commits.push(phase);
      });

      await screen.findByText('$0.001997');

      await vi.waitFor(() => {
        expect(latestBlockCounter(container)).toBe(blockMock.base.height.toLocaleString());
      }, { timeout: 30_000, interval: 100 });

      await socket.join(TOPIC);

      // One message whose flush lands on a cadence tick, so the burst that follows sits inside one
      // cadence instead of straddling two of them.
      socket.send(TOPIC, 'new_block', { average_block_time: '1000', block: socketBlock(1) });

      await vi.waitFor(() => {
        expect(latestBlockCounter(container)).toBe((blockMock.base.height + 1).toLocaleString());
      }, { timeout: 30_000, interval: 50 });

      await new Promise((resolve) => {
        setTimeout(resolve, 300);
      });

      const untouched = [ 'home-stats-coin' ]
        .map((label) => container.querySelector(`[data-label="${ label }"]`) as HTMLElement)
        .concat(container.querySelector('[data-highlight="total_txs"]') as HTMLElement);
      const mutations: Array<MutationRecord> = [];
      const observer = new MutationObserver((records) => {
        mutations.push(...records);
      });
      untouched.forEach((element) => observer.observe(element, { childList: true, characterData: true, subtree: true }));

      const historyTitle = container.querySelector('[data-label="home-stats-history"] [data-title]')?.textContent;
      const commitsBeforeBurst = commits.length;

      for (let offset = 2; offset <= 21; offset++) {
        socket.send(TOPIC, 'new_block', { average_block_time: '1000', block: socketBlock(offset) });
        await new Promise((resolve) => {
          setTimeout(resolve, 20);
        });
      }

      expect(commits.length - commitsBeforeBurst).toBe(0);
      expect(latestBlockCounter(container)).toBe((blockMock.base.height + 1).toLocaleString());

      await vi.waitFor(() => {
        expect(latestBlockCounter(container)).toBe((blockMock.base.height + 21).toLocaleString());
      }, { timeout: 30_000, interval: 50 });

      expect(mutations).toHaveLength(0);
      expect(container.querySelector('[data-label="home-stats-history"] [data-title]')?.textContent).toBe(historyTitle);

      observer.disconnect();
    } finally {
      await socket.close();
    }
  });

  it('titles the history column with the last fourteen days', async() => {
    const { container } = render(<Stats/>);

    await screen.findByText('$0.001997');

    const column = container.querySelector('[data-label="home-stats-history"]') as HTMLElement;

    expect(column.querySelector('[data-title]')?.textContent).toBe('Blockscout transaction history in 14 days');
  });
});
