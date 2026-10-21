// @vitest-environment jsdom

import React from 'react';

import * as addressMock from 'mocks/address/address';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { waitFor } from 'vitest/lib';
import flushPromises from 'vitest/utils/flushPromises';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_RE_CAPTCHA_APP_SITE_KEY: 'test-site-key',
    NEXT_PUBLIC_CROSS_CHAIN_TXS_ENABLED: 'true',
    NEXT_PUBLIC_INTERCHAIN_INDEXER_API_HOST: 'http://localhost:8051',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// The address lists render fifty placeholder rows of real table items, which jsdom lays out well past the
// default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

import AddressTxs from './AddressTxs';

const HASH = addressMock.hash;
const LOCAL_PATH = `/api/v2/addresses/${ HASH }/transactions`;
const CROSS_CHAIN_PATH = `/api/v1/interchain/messages:byAddress/${ HASH }`;

const requestedPaths = () => fetchMock.mock.calls.map((call) => decodeURIComponent(new URL(String(call[0]), 'http://localhost').pathname));

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

  it('leaves the direction to the rows instead of the card header', () => {
    routerState.query = { hash: HASH, tab: 'txs_local', filter: 'from' };

    const { container } = render(<AddressTxs txsCount={ 10 }/>);

    expect(container.querySelector('[data-scan-table-card] [data-actions] [data-direction]')).toBeNull();
    expect((container.querySelector('[data-scan-table-card] [data-actions]') as HTMLElement).textContent)
      .toContain('Download Page Data');
  });

  it('keeps the embedded transactions list inside the single card the tab opens', () => {
    const { container } = render(<AddressTxs txsCount={ 741895 }/>);

    expect(container.querySelectorAll('[data-scan-table-card]')).toHaveLength(1);
    expect(container.querySelectorAll('[data-scan-table-card] [data-title]')).toHaveLength(1);
    expect(container.querySelectorAll('[data-scan-table-card] [data-body] table').length).toBeGreaterThan(0);
  });

  it('closes the card with a centred view-all row and the CSV export beneath it', () => {
    const { container } = render(<AddressTxs txsCount={ 741895 }/>);

    const viewAll = container.querySelector('[data-scan-table-card] [data-view-all] a') as HTMLElement;

    expect(viewAll.getAttribute('href')).toBe('/txs');
    expect(viewAll.textContent).toBe('View all transactions →');
    expect(Array.from(container.querySelectorAll('[data-csv-export-label]')).map((item) => item.textContent))
      .toEqual([ 'Download Page Data', 'CSV Export' ]);
  });

  it('asks for the local list when the address page lands without a tab', async() => {
    routerState.query = { hash: HASH };

    render(<AddressTxs/>);

    await waitFor(() => {
      expect(requestedPaths()).toContain(LOCAL_PATH);
    });
    expect(requestedPaths()).not.toContain(CROSS_CHAIN_PATH);
  });

  it('asks for the cross-chain list only once its sub-tab is picked', async() => {
    routerState.query = { hash: HASH, tab: 'txs_cross_chain' };

    render(<AddressTxs/>);

    await waitFor(() => {
      expect(requestedPaths()).toContain(CROSS_CHAIN_PATH);
    });
    await flushPromises();
    expect(requestedPaths()).not.toContain(LOCAL_PATH);
  });
});
