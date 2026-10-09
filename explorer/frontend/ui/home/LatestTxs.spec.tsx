// @vitest-environment jsdom

import React from 'react';

import { SocketProvider } from 'lib/socket/context';
import { SOCKET_FLUSH_INTERVAL_MS } from 'lib/socket/useSocketBuffer';
import * as txMock from 'mocks/txs/tx';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { fireEvent, screen } from 'vitest/lib';
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

import LatestTxs from './LatestTxs';

const TOPIC = 'transactions:new_transaction';

const noticeText = (container: HTMLElement) => container.querySelector('[data-label="latest-txs-rows"]')
  ?.previousElementSibling?.textContent;

vi.setConfig({ testTimeout: 60_000 });

describe('LatestTxs', () => {
  beforeEach(() => {
    routerState.pathname = '/';
    routerState.query = {};
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => {
      const body = request.url.includes('/api/v2/main-page/transactions') ?
        [ txMock.base, txMock.base2, txMock.base3 ] :
        {};

      return { body: JSON.stringify(body), headers: { 'Content-Type': 'application/json' } };
    });
  });

  it('lists the transactions in the order the api returns them', async() => {
    const { container } = render(<LatestTxs/>);

    await screen.findByText('View all transactions');

    await vi.waitFor(() => {
      const rows = Array.from(container.querySelectorAll('[data-latest-tx]'))
        .map((item) => item.getAttribute('data-latest-tx'));

      expect(rows.slice(0, 3)).toEqual([ txMock.base.hash, txMock.base2.hash, txMock.base3.hash ]);
    }, { timeout: 30_000, interval: 100 });
  });

  it('keeps a mobile row and a desktop row for every transaction', async() => {
    const { container } = render(<LatestTxs/>);

    await screen.findByText('View all transactions');

    await vi.waitFor(() => {
      expect(container.querySelectorAll(`[data-latest-tx="${ txMock.base.hash }"]`)).toHaveLength(2);
    }, { timeout: 30_000, interval: 100 });
  });

  it('lets the desktop rows fit the half-width card with the parties and the value as columns', async() => {
    const { container } = render(<LatestTxs/>);

    await screen.findByText('View all transactions');

    await vi.waitFor(() => {
      expect(container.querySelectorAll(`[data-latest-tx="${ txMock.base.hash }"]`)).toHaveLength(2);
    }, { timeout: 30_000, interval: 100 });

    const desktopRow = container.querySelectorAll(`[data-latest-tx="${ txMock.base.hash }"]`)[1] as HTMLElement;
    const labels = Array.from(desktopRow.children).map((column) => column.getAttribute('data-label'));

    expect(window.getComputedStyle(desktopRow.parentElement as HTMLElement).minWidth).not.toBe('720px');
    expect(labels).toContain('tx-parties');
    expect(labels).toContain('tx-value');
    expect(labels).not.toContain('tx-tags');
  });

  it('closes the list with the link to every transaction', async() => {
    const { container } = render(<LatestTxs/>);

    const footer = await screen.findByText('View all transactions');

    expect(footer.getAttribute('href')).toBe('/txs');
    expect(container.querySelector('[data-label="view-all-txs"]')).not.toBeNull();
  });

  it('counts twenty transactions that arrive inside half a second in one notice', async() => {
    const socket = await createTestSocket();

    try {
      const { container } = render(
        <SocketProvider url={ socket.url }>
          <LatestTxs/>
        </SocketProvider>,
      );

      await screen.findByText('View all transactions');

      await vi.waitFor(() => {
        expect(noticeText(container)).toContain('scanning new transactions...');
      }, { timeout: 30_000, interval: 100 });

      await socket.join(TOPIC);

      // One message whose flush lands on a cadence tick, so the burst that follows sits inside one
      // cadence instead of straddling two of them.
      socket.send(TOPIC, 'transaction', { transaction: 1 });

      await vi.waitFor(() => {
        expect(noticeText(container)).toContain('1 more transaction has come in');
      }, { timeout: 30_000, interval: 50 });

      await new Promise((resolve) => {
        setTimeout(resolve, 300);
      });

      for (let count = 0; count < 20; count++) {
        socket.send(TOPIC, 'transaction', { transaction: 1 });
        await new Promise((resolve) => {
          setTimeout(resolve, 20);
        });
      }

      expect(noticeText(container)).toContain('1 more transaction has come in');

      await vi.waitFor(() => {
        expect(noticeText(container)).toContain('21 more transactions have come in');
      }, { timeout: 30_000, interval: 50 });
    } finally {
      await socket.close();
    }
  });

  it('holds the notice while the pointer rests on the list', async() => {
    const socket = await createTestSocket();

    try {
      const { container } = render(
        <SocketProvider url={ socket.url }>
          <LatestTxs/>
        </SocketProvider>,
      );

      await screen.findByText('View all transactions');

      await socket.join(TOPIC);

      const rows = container.querySelector('[data-label="latest-txs-rows"]') as HTMLElement;

      fireEvent.mouseOver(rows);

      socket.send(TOPIC, 'transaction', { transaction: 3 });

      await new Promise((resolve) => {
        setTimeout(resolve, SOCKET_FLUSH_INTERVAL_MS * 2);
      });

      expect(noticeText(container)).toContain('scanning new transactions...');

      fireEvent.mouseOut(rows, { relatedTarget: document.body });

      await vi.waitFor(() => {
        expect(noticeText(container)).toContain('3 more transactions have come in');
      }, { timeout: 30_000, interval: 50 });
    } finally {
      await socket.close();
    }
  });
});
