// @vitest-environment jsdom

import React from 'react';

import type { PaxeerXAnchorsItem } from 'types/api/paxeerXLists';

import { PAXEER_X_ANCHORS_ITEM } from 'stubs/paxeerXLists';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// The list renders fifty placeholder rows of real table items while the request is in flight, which
// jsdom lays out well past the default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

import PaxeerXAnchors from './PaxeerXAnchors';

const second: PaxeerXAnchorsItem = {
  ...PAXEER_X_ANCHORS_ITEM,
  batch_number: PAXEER_X_ANCHORS_ITEM.batch_number - 1,
  checkpoint_id: '0x4b3c2d1e0f9a8b7c6d5e4f30211223344556677889900aabbccddeeff00112233',
  block_number: PAXEER_X_ANCHORS_ITEM.block_number - 1,
};

const items = [ PAXEER_X_ANCHORS_ITEM, second ];

const mockItems = (payload: Array<PaxeerXAnchorsItem>) => {
  fetchMock.mockResponse(
    JSON.stringify({ items: payload, next_page_params: null }),
    { headers: { 'Content-Type': 'application/json' } },
  );
};

describe('PaxeerXAnchors', () => {
  beforeEach(() => {
    routerState.pathname = '/paxeer-x/anchors';
    routerState.query = {};
    fetchMock.resetMocks();
    mockItems(items);
  });

  it('heads the page with the anchor checkpoints title', async() => {
    render(<PaxeerXAnchors/>);

    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 1 }).textContent).toContain('Anchor checkpoints');
    });
  });

  it('counts the checkpoints in the head of the scan table card and notes the page', async() => {
    const { container } = render(<PaxeerXAnchors/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
        .toBe('A total of 2 anchor checkpoints found');
    });
    expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent)
      .toBe('Showing page 1 of the checkpoints the node returns, newest first');
  });

  it('renders the checkpoints the node returns as rows of the card', async() => {
    const { container } = render(<PaxeerXAnchors/>);

    await waitFor(() => {
      expect(Array.from(container.querySelectorAll('[data-scan-table-card] [data-anchor]')).map((row) => row.getAttribute('data-anchor')))
        .toEqual(items.map((item) => item.checkpoint_id));
    });
    expect(container.querySelector('[data-scan-table-card] [data-label="paxeer-x-anchors"]')).toBeTruthy();
  });

  it('says so when the node returns no checkpoint', async() => {
    fetchMock.resetMocks();
    mockItems([]);

    render(<PaxeerXAnchors/>);

    await waitFor(() => {
      expect(screen.getByText('There are no anchor checkpoints.')).toBeTruthy();
    });
    expect(screen.queryByText('A total of 0 anchor checkpoints found')).toBeNull();
  });
});
