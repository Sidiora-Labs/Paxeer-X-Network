// @vitest-environment jsdom

import React from 'react';

import * as statsMock from 'mocks/stats/index';
import * as txsStatsMock from 'mocks/txs/stats';
import * as txMock from 'mocks/txs/tx';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { waitFor } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_STATS_API_HOST: '',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import Transactions from './Transactions';

const PAGE_TIMEOUT = 120_000;
const WAIT_TIMEOUT = 60_000;

const txsResponse = {
  items: [ txMock.base, txMock.base2, txMock.base3 ],
  next_page_params: null,
};

describe('Transactions', () => {
  beforeEach(() => {
    routerState.pathname = '/txs';
    routerState.query = {};
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => {
      if (request.url.includes('/api/v2/transactions/stats')) {
        return { body: JSON.stringify(txsStatsMock.base), headers: { 'Content-Type': 'application/json' } };
      }

      if (request.url.includes('/api/v2/transactions')) {
        return { body: JSON.stringify(txsResponse), headers: { 'Content-Type': 'application/json' } };
      }

      if (request.url.includes('/api/v2/stats')) {
        return { body: JSON.stringify(statsMock.base), headers: { 'Content-Type': 'application/json' } };
      }

      return { body: JSON.stringify({}), headers: { 'Content-Type': 'application/json' } };
    });
  });

  it('opens with the page title and the entry to the API documentation', () => {
    const { container } = render(<Transactions/>);

    expect(container.querySelector('h1')?.textContent).toBe('Transactions');
    expect(container.querySelector('[data-page-api-entry]')?.getAttribute('href')).toBe('/api-docs');
  }, PAGE_TIMEOUT);

  it('puts the stat card row above the table card', async() => {
    const { container } = render(<Transactions/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card]')).toBeTruthy();
    }, { timeout: WAIT_TIMEOUT });

    const row = container.querySelector('[data-scan-stat-row]') as HTMLElement;
    const card = container.querySelector('[data-scan-table-card]') as HTMLElement;

    expect(row).toBeTruthy();
    expect(row.compareDocumentPosition(card) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  }, PAGE_TIMEOUT);

  it('counts the transactions of the chain in the card header', async() => {
    const { container } = render(<Transactions/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
        .toBe('Latest 3 from a total of 82,258,122 transactions');
    }, { timeout: WAIT_TIMEOUT });
  }, PAGE_TIMEOUT);
});
