// @vitest-environment jsdom

import React from 'react';

import * as feedMock from 'mocks/paxeerX/feed';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import PaxeerXFeedMarket from './PaxeerXFeedMarket';

const mockApi = (updates: object) => {
  fetchMock.resetMocks();
  fetchMock.mockResponse((request) => Promise.resolve({
    body: JSON.stringify(request.url.includes('/updates') ? updates : feedMock.markets),
    headers: { 'Content-Type': 'application/json' },
  }));
};

describe('PaxeerXFeedMarket', () => {
  beforeEach(() => {
    routerState.pathname = '/feed/market/[id]';
    routerState.query = { id: '0' };
    mockApi(feedMock.marketUpdatesPage);
  });

  it('titles the page with the market symbol and lists its updates', async() => {
    const { container } = render(<PaxeerXFeedMarket/>);

    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 1 }).textContent).toContain('BTC-USD price history');
    });
    await waitFor(() => {
      expect(Array.from(container.querySelectorAll('[data-update]')).map((row) => row.getAttribute('data-update')))
        .toEqual(feedMock.marketUpdates.map((item) => item.update_hash));
    });
    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent).toBe('BTC-USD · BTCUSDT');
    expect(fetchMock.mock.calls.some(([ url ]) => String(url).includes('/api/v2/paxeer-x/feed/markets/0/updates'))).toBe(true);
  });

  it('says so when the market has no update', async() => {
    mockApi({ items: [], next_page_params: null });

    render(<PaxeerXFeedMarket/>);

    await waitFor(() => {
      expect(screen.getByText('There are no price updates for this market.')).toBeTruthy();
    });
  });
});
