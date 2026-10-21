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

import { HomeRpcDataContextProvider } from './fallbacks/rpcDataContext';
import Transactions from './Transactions';

vi.setConfig({ testTimeout: 60_000 });

describe('Transactions', () => {
  beforeEach(() => {
    routerState.pathname = '/';
    routerState.query = {};
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => {
      const body = request.url.includes('/api/v2/main-page/transactions') ? [ txMock.base, txMock.base2 ] : {};

      return { body: JSON.stringify(body), headers: { 'Content-Type': 'application/json' } };
    });
  });

  it('heads one card with the latest transactions', async() => {
    const { container } = render(
      <HomeRpcDataContextProvider>
        <Transactions/>
      </HomeRpcDataContextProvider>,
    );

    await screen.findByText('View all transactions');

    expect(container.querySelectorAll('[data-scan-table-card]')).toHaveLength(1);
    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent).toBe('Latest transactions');
  });

  it('holds the transaction rows inside that card', async() => {
    const { container } = render(
      <HomeRpcDataContextProvider>
        <Transactions/>
      </HomeRpcDataContextProvider>,
    );

    await screen.findByText('View all transactions');

    const card = container.querySelector('[data-scan-table-card]') as HTMLElement;

    await vi.waitFor(() => {
      expect(card.querySelectorAll(`[data-latest-tx="${ txMock.base.hash }"]`).length).toBeGreaterThan(0);
    }, { timeout: 30_000, interval: 100 });
  });
});
