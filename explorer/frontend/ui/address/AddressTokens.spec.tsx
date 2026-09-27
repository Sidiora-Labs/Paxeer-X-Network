// @vitest-environment jsdom

import React from 'react';

import * as addressMock from 'mocks/address/address';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// The address lists render fifty placeholder rows of real table items, which jsdom lays out well past the
// default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

import AddressTokens from './AddressTokens';

const HASH = addressMock.hash;

describe('AddressTokens', () => {
  beforeEach(() => {
    routerState.pathname = '/address/[hash]';
    routerState.query = { hash: HASH, tab: 'tokens_erc20' };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({ items: [], next_page_params: null }), { headers: { 'Content-Type': 'application/json' } });
  });

  it('heads the token balances card with the shown slice against the tab counter', () => {
    const { container } = render(<AddressTokens tokensCount={ 312 }/>);

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe('Latest 10 from a total of 312 token balances');
  });

  it('falls back to the total when no tab counter is available', () => {
    const { container } = render(<AddressTokens/>);

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe('A total of 10 token balances found');
  });

  it('closes the card with the view-all row', () => {
    const { container } = render(<AddressTokens tokensCount={ 312 }/>);

    const viewAll = container.querySelector('[data-scan-table-card] [data-view-all] a') as HTMLElement;

    expect(viewAll.getAttribute('href')).toBe('/tokens');
    expect(viewAll.textContent).toBe('View all tokens →');
  });

  it('keeps both token tabs on the pill strip', () => {
    const { container } = render(<AddressTokens tokensCount={ 312 }/>);

    const tabs = Array.from(container.querySelectorAll('[role="tab"]')).map((item) => item.textContent);

    expect(tabs.some((title) => title?.startsWith('NFTs'))).toBe(true);
    expect(tabs.length).toBeGreaterThanOrEqual(2);
  });
});
