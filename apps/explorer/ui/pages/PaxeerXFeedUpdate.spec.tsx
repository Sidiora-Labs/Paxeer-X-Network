// @vitest-environment jsdom

import React from 'react';

import * as feedMock from 'mocks/paxeerX/feed';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import PaxeerXFeedUpdate from './PaxeerXFeedUpdate';

describe('PaxeerXFeedUpdate', () => {
  beforeEach(() => {
    routerState.pathname = '/feed/update/[hash]';
    routerState.query = { hash: feedMock.updateHash };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify(feedMock.priceUpdate), { headers: { 'Content-Type': 'application/json' } });
  });

  it('requests the update by its hash and renders its details', async() => {
    const { container } = render(<PaxeerXFeedUpdate/>);

    await waitFor(() => {
      expect(container.querySelector(`[data-price-update="${ feedMock.updateHash }"]`)).toBeTruthy();
    });
    expect(screen.getByRole('heading', { level: 1 }).textContent).toContain('Price update');
    expect(fetchMock.mock.calls.some(([ url ]) => String(url).includes(`/api/v2/paxeer-x/feed/updates/${ feedMock.updateHash }`))).toBe(true);
    expect(container.querySelectorAll('[data-observation]')).toHaveLength(2);
  });
});
