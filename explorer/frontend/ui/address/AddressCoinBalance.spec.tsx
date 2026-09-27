// @vitest-environment jsdom

import React from 'react';

import * as addressMock from 'mocks/address/address';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// The address lists render fifty placeholder rows of real table items, which jsdom lays out well past the
// default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

import AddressCoinBalance from './AddressCoinBalance';

describe('AddressCoinBalance', () => {
  beforeEach(() => {
    routerState.pathname = '/address/[hash]';
    routerState.query = { hash: addressMock.hash, tab: 'coin_balance_history' };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({ items: [], next_page_params: null }), { headers: { 'Content-Type': 'application/json' } });
  });

  it('puts the chart in its own card above the history', () => {
    const { container } = render(<AddressCoinBalance/>);

    expect(container.querySelector('[data-coin-balance-chart-card]')).toBeTruthy();
  });

  it('holds the height of the chart with a placeholder until the chart itself arrives', async() => {
    const { container } = render(<AddressCoinBalance/>);

    const card = container.querySelector('[data-coin-balance-chart-card]') as HTMLElement;

    expect(card.querySelector('[data-coin-balance-chart-skeleton]')).toBeTruthy();
    expect(card.textContent).not.toContain('Balances');

    await waitFor(() => {
      expect(container.querySelector('[data-coin-balance-chart-card]')?.textContent).toContain('Balances');
    // The chart is fetched as its own module, and the first fetch of it in this environment is a transform
    // of the chart and its d3 dependencies rather than a cached chunk.
    }, { timeout: 30_000 });
  });

  it('heads the history card with the number of balance changes', () => {
    const { container } = render(<AddressCoinBalance/>);

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe('A total of 50 balance changes found');
  });

  it('renders nothing when the tab is not the one being shown', () => {
    const { container } = render(<AddressCoinBalance shouldRender={ false }/>);

    expect(container.querySelector('[data-scan-table-card]')).toBeNull();
  });
});
