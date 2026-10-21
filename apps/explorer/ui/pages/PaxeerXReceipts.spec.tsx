// @vitest-environment jsdom

import React from 'react';

import type { PaxeerXReceiptsItem } from 'types/api/paxeerXLists';

import { PAXEER_X_RECEIPTS_ITEM } from 'stubs/paxeerXLists';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// The list renders fifty placeholder rows of real table items while the request is in flight, which
// jsdom lays out well past the default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

import PaxeerXReceipts from './PaxeerXReceipts';

const second: PaxeerXReceiptsItem = {
  ...PAXEER_X_RECEIPTS_ITEM,
  id: '0x11223344556677889900aabbccddeeff0f1e2d3c4b5a69788796a5b4c3d2e1f0',
  status: 'final',
  block_number: PAXEER_X_RECEIPTS_ITEM.block_number - 1,
};

const items = [ PAXEER_X_RECEIPTS_ITEM, second ];

const mockItems = (payload: Array<PaxeerXReceiptsItem>) => {
  fetchMock.mockResponse(
    JSON.stringify({ items: payload, next_page_params: null }),
    { headers: { 'Content-Type': 'application/json' } },
  );
};

describe('PaxeerXReceipts', () => {
  beforeEach(() => {
    routerState.pathname = '/paxeer-x/receipts';
    routerState.query = {};
    fetchMock.resetMocks();
    mockItems(items);
  });

  it('heads the page with the kernel receipts title', async() => {
    render(<PaxeerXReceipts/>);

    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 1 }).textContent).toContain('Kernel receipts');
    });
  });

  it('counts the receipts in the head of the scan table card and notes the page', async() => {
    const { container } = render(<PaxeerXReceipts/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
        .toBe('A total of 2 kernel receipts found');
    });
    expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent)
      .toBe('Showing page 1 of the receipts the node returns, newest first');
  });

  it('renders the receipts the node returns as rows of the card', async() => {
    const { container } = render(<PaxeerXReceipts/>);

    await waitFor(() => {
      expect(Array.from(container.querySelectorAll('[data-scan-table-card] [data-receipt]')).map((row) => row.getAttribute('data-receipt')))
        .toEqual(items.map((item) => item.id));
    });
    expect(container.querySelector('[data-scan-table-card] [data-label="paxeer-x-receipts"]')).toBeTruthy();
  });

  it('says so when the node returns no receipt', async() => {
    fetchMock.resetMocks();
    mockItems([]);

    render(<PaxeerXReceipts/>);

    await waitFor(() => {
      expect(screen.getByText('There are no kernel receipts.')).toBeTruthy();
    });
    expect(screen.queryByText('A total of 0 kernel receipts found')).toBeNull();
  });
});
