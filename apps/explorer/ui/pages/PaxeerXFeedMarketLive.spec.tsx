// @vitest-environment jsdom

import React from 'react';

import type { MarketUpdate } from 'types/api/paxeerXFeed';

import { SocketProvider } from 'lib/socket/context';
import * as feedMock from 'mocks/paxeerX/feed';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { waitFor } from 'vitest/lib';
import { createTestSocket } from 'vitest/utils/socketServer';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import PaxeerXFeedMarket from './PaxeerXFeedMarket';

const TOPIC = 'paxeer_x_feed:market:0';

const historyItem = (blockNumber: number, hashByte: string, sequence: number): MarketUpdate => ({
  update_hash: `0x${ hashByte.repeat(32) }`,
  block_number: blockNumber,
  timestamp: '2026-01-02T03:04:10.000000Z',
  index_price: '67500000000000000000000',
  mark_price: '67505000000000000000000',
  source_timestamp_ms: 1767323050000,
  sequence,
});

const updateHashes = (container: HTMLElement) => Array.from(container.querySelectorAll('[data-update]')).map((item) => item.getAttribute('data-update'));

vi.setConfig({ testTimeout: 60_000 });

describe('PaxeerXFeedMarket live updates', () => {
  beforeEach(() => {
    routerState.pathname = '/feed/market/[id]';
    routerState.query = { id: '0' };
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => Promise.resolve({
      body: JSON.stringify(request.url.includes('/updates') ? feedMock.marketUpdatesPage : feedMock.markets),
      headers: { 'Content-Type': 'application/json' },
    }));
  });

  it('puts the submissions of its market topic on top of the first page', async() => {
    const socket = await createTestSocket();

    try {
      const { container } = render(
        <SocketProvider url={ socket.url }>
          <PaxeerXFeedMarket/>
        </SocketProvider>,
      );

      const initial = feedMock.marketUpdates.map((item) => item.update_hash);

      await waitFor(() => {
        expect(updateHashes(container)).toEqual(initial);
      });
      await socket.join(TOPIC);

      const first = historyItem(34800124, 'ab', 9200);
      const second = historyItem(34800125, 'cd', 9201);

      socket.send(TOPIC, 'new_update', first);
      socket.send(TOPIC, 'new_update', second);
      socket.send(TOPIC, 'new_update', feedMock.marketUpdates[0]);

      await waitFor(() => {
        expect(updateHashes(container)).toEqual([ second.update_hash, first.update_hash, initial[0] ]);
      }, { timeout: 10_000 });

      expect(container.querySelector(`[data-update="${ second.update_hash }"] [data-price]`)?.getAttribute('data-price')).toBe(second.index_price);
    } finally {
      await socket.close();
    }
  });

  it('leaves a later page as the endpoint returned it', async() => {
    routerState.query = { id: '0', page: '2' };

    const socket = await createTestSocket();

    try {
      const { container } = render(
        <SocketProvider url={ socket.url }>
          <PaxeerXFeedMarket/>
        </SocketProvider>,
      );

      await waitFor(() => {
        expect(updateHashes(container)).toEqual(feedMock.marketUpdates.map((item) => item.update_hash));
      });

      await new Promise((resolve) => {
        setTimeout(resolve, 500);
      });

      expect(() => socket.send(TOPIC, 'new_update', historyItem(34800124, 'ab', 9200))).toThrow(`no client joined ${ TOPIC }`);
    } finally {
      await socket.close();
    }
  });
});
