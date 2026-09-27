// @vitest-environment jsdom

import React from 'react';

import { PAXEER_X_RECEIPTS_ITEM } from 'stubs/paxeerXLists';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import PaxeerXReceiptsListItem from './PaxeerXReceiptsListItem';

const renderItem = (item = PAXEER_X_RECEIPTS_ITEM) => render(<PaxeerXReceiptsListItem item={ item }/>);

describe('PaxeerXReceiptsListItem', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('carries the same fields as the table row, one label per line', () => {
    renderItem();

    expect(screen.getByText('Receipt ID')).toBeTruthy();
    expect(screen.getByText('Account')).toBeTruthy();
    expect(screen.getByText('Block')).toBeTruthy();
    expect(screen.getByText('Settlement')).toBeTruthy();
  });

  it('links the receipt id to its page and the block to the block page', () => {
    const { container } = renderItem();

    expect(container.querySelector(`a[href="/paxeer-x/receipts/${ PAXEER_X_RECEIPTS_ITEM.id }"]`)).toBeTruthy();
    expect(container.querySelector(`a[href="/block/${ PAXEER_X_RECEIPTS_ITEM.block_number }"]`)?.textContent)
      .toBe(String(PAXEER_X_RECEIPTS_ITEM.block_number));
  });

  it('puts the entry on its rung of the settlement ladder', () => {
    const { container } = renderItem();

    expect(container.querySelector(`[data-rung="${ PAXEER_X_RECEIPTS_ITEM.status }"]`)).toBeTruthy();
  });

  it('marks an account the receipt log does not carry', () => {
    renderItem({ ...PAXEER_X_RECEIPTS_ITEM, account: null });

    expect(screen.getByText('—')).toBeTruthy();
    expect(screen.getAllByLabelText('copy')).toHaveLength(1);
  });
});
