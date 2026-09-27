// @vitest-environment jsdom

import React from 'react';

import * as txMock from 'mocks/txs/tx';
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

import LatestTxs from './LatestTxs';

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

  it('closes the list with the link to every transaction', async() => {
    const { container } = render(<LatestTxs/>);

    const footer = await screen.findByText('View all transactions');

    expect(footer.getAttribute('href')).toBe('/txs');
    expect(container.querySelector('[data-label="view-all-txs"]')).not.toBeNull();
  });
});
