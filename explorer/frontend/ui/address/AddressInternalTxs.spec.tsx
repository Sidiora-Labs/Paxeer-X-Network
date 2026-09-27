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

import AddressInternalTxs from './AddressInternalTxs';

const HASH = addressMock.hash;

describe('AddressInternalTxs', () => {
  beforeEach(() => {
    routerState.pathname = '/address/[hash]';
    routerState.query = { hash: HASH, tab: 'internal_txns' };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({ items: [], next_page_params: null }), { headers: { 'Content-Type': 'application/json' } });
  });

  it('heads the table card with the shown slice against the tab counter', () => {
    const { container } = render(<AddressInternalTxs internalTxsCount={ 13120 }/>);

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe('Latest 50 from a total of 13,120 internal transactions');
  });

  it('carries the filter and the page-data download in the card header', () => {
    const { container } = render(<AddressInternalTxs internalTxsCount={ 13120 }/>);

    expect((container.querySelector('[data-scan-table-card] [data-actions]') as HTMLElement).textContent)
      .toContain('Download Page Data');
  });

  it('marks the filtered direction in the card header', () => {
    routerState.query = { hash: HASH, tab: 'internal_txns', filter: 'to' };

    const { container } = render(<AddressInternalTxs internalTxsCount={ 13120 }/>);

    expect(container.querySelector('[data-direction]')?.getAttribute('data-direction')).toBe('in');
  });

  it('closes the card with the view-all row and keeps the CSV export beneath it', () => {
    const { container } = render(<AddressInternalTxs internalTxsCount={ 13120 }/>);

    const viewAll = container.querySelector('[data-scan-table-card] [data-view-all] a') as HTMLElement;

    expect(viewAll.getAttribute('href')).toBe('/internal-txs');
    expect(viewAll.textContent).toBe('View all internal transactions →');
    expect(Array.from(container.querySelectorAll('[data-csv-export-label]')).map((item) => item.textContent))
      .toEqual([ 'Download Page Data', 'CSV Export' ]);
  });
});
