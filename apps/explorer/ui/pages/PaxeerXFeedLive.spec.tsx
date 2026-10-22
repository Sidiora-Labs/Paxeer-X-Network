// @vitest-environment jsdom

import React from 'react';

import type { PriceUpdateItem } from 'types/api/paxeerXFeed';

import { SocketProvider } from 'lib/socket/context';
import * as feedMock from 'mocks/paxeerX/feed';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { waitFor } from 'vitest/lib';
import { createTestSocket } from 'vitest/utils/socketServer';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import PaxeerXFeed from './PaxeerXFeed';

const TOPIC = 'paxeer_x_feed:new_update';

const solIndexPrice = '151230000000000000000';
const solMarkPrice = '151250000000000000000';
const newBtcIndexPrice = '67500000000000000000000';

const submission = (blockNumber: number, hashByte: string, overrides?: Partial<PriceUpdateItem>): PriceUpdateItem => ({
  ...feedMock.priceUpdate,
  hash: `0x${ hashByte.repeat(32) }`,
  block_number: blockNumber,
  price_update: {
    ...feedMock.priceUpdate.price_update,
    observation_count: 2,
    observations: [
      { market_id: 0, symbol: 'BTC-USD', index_price: newBtcIndexPrice, mark_price: feedMock.btcMarkPrice, source_timestamp_ms: 1767323050000, sequence: 9100 },
      { market_id: 17, symbol: 'SOL-USD', index_price: solIndexPrice, mark_price: solMarkPrice, source_timestamp_ms: 1767323050000, sequence: 9101 },
    ],
  },
  ...overrides,
});

const row = (container: HTMLElement, marketId: number) => container.querySelector(`[data-market="${ marketId }"]`) as HTMLElement | null;
const indexPriceOf = (container: HTMLElement, marketId: number) => row(container, marketId)?.querySelector('[data-price]')?.getAttribute('data-price');
const note = (container: HTMLElement) => container.querySelector('[data-scan-table-card] [data-note]')?.textContent;

vi.setConfig({ testTimeout: 60_000 });

describe('PaxeerXFeed live updates', () => {
  beforeEach(() => {
    routerState.pathname = '/feed';
    routerState.query = {};
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify(feedMock.markets), { headers: { 'Content-Type': 'application/json' } });
  });

  it('moves the market rows in place when a submission arrives on the feed topic', async() => {
    const socket = await createTestSocket();

    try {
      const { container } = render(
        <SocketProvider url={ socket.url }>
          <PaxeerXFeed/>
        </SocketProvider>,
      );

      await waitFor(() => {
        expect(indexPriceOf(container, 0)).toBe(feedMock.btcIndexPrice);
      });
      await socket.join(TOPIC);

      const btcRowBefore = row(container, 0);
      expect(row(container, 17)?.querySelector('[data-price]')).toBeNull();

      socket.send(TOPIC, 'new_update', submission(34800140, 'ab'));
      socket.send(TOPIC, 'new_update', submission(34800141, 'cd', {
        status: 'error',
        result: 'failed',
      }));

      await waitFor(() => {
        expect(indexPriceOf(container, 0)).toBe(newBtcIndexPrice);
      }, { timeout: 10_000 });

      expect(indexPriceOf(container, 17)).toBe(solIndexPrice);
      expect(row(container, 0)?.querySelector(`a[href="/feed/update/0x${ 'ab'.repeat(32) }"]`)).not.toBeNull();
      expect(row(container, 0)?.querySelector('a[href="/block/34800140"]')).not.toBeNull();
      expect(note(container)).toBe(`Latest submitted prices per market at chain head block ${ (34800140).toLocaleString() }`);
      expect(Array.from(container.querySelectorAll('[data-market]')).map((item) => item.getAttribute('data-market'))).toEqual([ '0', '17' ]);
      // The row is mounted again for its new submission, which is what replays the list fade-in.
      expect(btcRowBefore?.isConnected).toBe(false);

      socket.send(TOPIC, 'new_update', submission(34800139, 'ef', {
        price_update: {
          ...feedMock.priceUpdate.price_update,
          observation_count: 1,
          observations: [
            { market_id: 0, symbol: 'BTC-USD', index_price: '1', mark_price: '1', source_timestamp_ms: 1767323049000, sequence: 9099 },
          ],
        },
      }));

      await new Promise((resolve) => {
        setTimeout(resolve, 3_000);
      });

      expect(indexPriceOf(container, 0)).toBe(newBtcIndexPrice);
    } finally {
      await socket.close();
    }
  });

  it('reads the markets endpoint again while no socket carries the feed', async() => {
    const { container } = render(<PaxeerXFeed/>);

    await waitFor(() => {
      expect(indexPriceOf(container, 0)).toBe(feedMock.btcIndexPrice);
    });

    const marketReads = () => fetchMock.mock.calls.filter(([ url ]) => String(url).includes('/api/v2/paxeer-x/feed/markets')).length;
    const readsAfterLoad = marketReads();

    await waitFor(() => {
      expect(marketReads()).toBeGreaterThan(readsAfterLoad);
    }, { timeout: 15_000, interval: 250 });
  });
});
