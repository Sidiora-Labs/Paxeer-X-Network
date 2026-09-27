// @vitest-environment jsdom

import React from 'react';

import * as addressMock from 'mocks/address/address';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

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
