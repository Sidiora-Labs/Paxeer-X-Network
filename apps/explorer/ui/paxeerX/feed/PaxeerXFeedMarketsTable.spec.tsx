// @vitest-environment jsdom

import React from 'react';

import * as feedMock from 'mocks/paxeerX/feed';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import PaxeerXFeedMarketsTable from './PaxeerXFeedMarketsTable';

const renderTable = () => render(<PaxeerXFeedMarketsTable items={ feedMock.markets.items }/>);

describe('PaxeerXFeedMarketsTable', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('heads the table with the market columns in order', () => {
    const { container } = renderTable();

    expect(Array.from(container.querySelectorAll('thead th')).map((cell) => cell.textContent))
      .toEqual([ 'Market', 'Index price', 'Mark price', 'Source time', 'Last update', 'Block' ]);
  });

  it('renders one row per market with its prices at 18 decimals', () => {
    const { container } = renderTable();

    expect(Array.from(container.querySelectorAll('[data-market]')).map((row) => row.getAttribute('data-market')))
      .toEqual([ '0', '17' ]);

    const cells = container.querySelectorAll('[data-market="0"] td');
    expect(cells[0]?.textContent).toBe('BTC-USDBTCUSDT');
    expect(cells[1]?.textContent).toBe('67,012.5');
    expect(cells[2]?.textContent).toBe('67,020.25');
  });

  it('links the market to its history, the last update to its page and the block to the block page', () => {
    const { container } = renderTable();

    const row = container.querySelector('[data-market="0"]') as HTMLElement;

    expect(row.querySelector('a[href="/feed/market/0"]')?.textContent).toBe('BTC-USD');
    expect(row.querySelector(`a[href="/feed/update/${ feedMock.updateHash }"]`)).toBeTruthy();
    expect(row.querySelector('a[href="/block/34800123"]')?.textContent).toBe('34800123');
  });

  it('shows dashes for a market that has no update yet', () => {
    const { container } = renderTable();

    const cells = Array.from(container.querySelectorAll('[data-market="17"] td')).slice(1).map((cell) => cell.textContent);

    expect(cells).toEqual([ '—', '—', '—', '—', '—' ]);
  });
});
