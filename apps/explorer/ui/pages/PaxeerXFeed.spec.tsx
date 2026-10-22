// @vitest-environment jsdom

import React from 'react';

import * as feedMock from 'mocks/paxeerX/feed';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import PaxeerXFeed from './PaxeerXFeed';

const mockMarkets = (payload: object) => {
  fetchMock.resetMocks();
  fetchMock.mockResponse(JSON.stringify(payload), { headers: { 'Content-Type': 'application/json' } });
};

describe('PaxeerXFeed', () => {
  beforeEach(() => {
    routerState.pathname = '/feed';
    routerState.query = {};
    mockMarkets(feedMock.markets);
  });

  it('heads the page with the price feed title', async() => {
    render(<PaxeerXFeed/>);

    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 1 }).textContent).toContain('Price feed');
    });
  });

  it('counts the markets and names the chain head in the card head', async() => {
    const { container } = render(<PaxeerXFeed/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent).toBe('A total of 2 markets found');
    });
    expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent)
      .toBe(`Latest submitted prices per market at chain head block ${ (34800135).toLocaleString() }`);
    expect(fetchMock.mock.calls.some(([ url ]) => String(url).includes('/api/v2/paxeer-x/feed/markets'))).toBe(true);
  });

  it('renders the markets the node returns', async() => {
    const { container } = render(<PaxeerXFeed/>);

    await waitFor(() => {
      expect(Array.from(container.querySelectorAll('[data-market]')).map((row) => row.getAttribute('data-market'))).toEqual([ '0', '17' ]);
    });
  });

  it('says so when the node returns no market', async() => {
    mockMarkets({ items: [], chain_head: 1 });

    render(<PaxeerXFeed/>);

    await waitFor(() => {
      expect(screen.getByText('There are no price feed markets.')).toBeTruthy();
    });
  });
});
