// @vitest-environment jsdom

import React from 'react';

import * as addressMock from 'mocks/address/address';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_RE_CAPTCHA_APP_SITE_KEY: 'test-site-key',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// The address lists render fifty placeholder rows of real table items, which jsdom lays out well past the
// default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

import AddressTxs from './AddressTxs';

const HASH = addressMock.hash;

describe('AddressTxs', () => {
  beforeEach(() => {
    routerState.pathname = '/address/[hash]';
    routerState.query = { hash: HASH, tab: 'txs_local' };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({ items: [], next_page_params: null }), { headers: { 'Content-Type': 'application/json' } });
  });

  it('heads the table card with the shown slice against the tab counter', () => {
    const { container } = render(<AddressTxs txsCount={ 741895 }/>);

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe('Latest 50 from a total of 741,895 transactions');
  });

  it('falls back to the total when no tab counter is available', () => {
    const { container } = render(<AddressTxs/>);

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe('A total of 50 transactions found');
  });

  it('puts the page-data download in the card header', () => {
    const { container } = render(<AddressTxs txsCount={ 741895 }/>);

    const actions = container.querySelector('[data-scan-table-card] [data-actions]') as HTMLElement;

    expect(actions.textContent).toContain('Download Page Data');
  });

  it('shows the direction badge for the filtered direction only', () => {
    routerState.query = { hash: HASH, tab: 'txs_local' };
    const { container: unfiltered } = render(<AddressTxs txsCount={ 10 }/>);

    expect(unfiltered.querySelector('[data-direction]')).toBeNull();

    routerState.query = { hash: HASH, tab: 'txs_local', filter: 'from' };
    const { container: filtered } = render(<AddressTxs txsCount={ 10 }/>);

    expect(filtered.querySelector('[data-direction]')?.getAttribute('data-direction')).toBe('out');
  });

  it('closes the card with a centred view-all row and the CSV export beneath it', () => {
    const { container } = render(<AddressTxs txsCount={ 741895 }/>);

    const viewAll = container.querySelector('[data-scan-table-card] [data-view-all] a') as HTMLElement;

    expect(viewAll.getAttribute('href')).toBe('/txs');
    expect(viewAll.textContent).toBe('View all transactions →');
    expect(Array.from(container.querySelectorAll('[data-csv-export-label]')).map((item) => item.textContent))
      .toEqual([ 'Download Page Data', 'CSV Export' ]);
  });
});
