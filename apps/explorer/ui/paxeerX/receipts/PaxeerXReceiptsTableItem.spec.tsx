// @vitest-environment jsdom

import React from 'react';

import { PAXEER_X_RECEIPTS_ITEM } from 'stubs/paxeerXLists';
import { TableBody, TableRoot } from 'toolkit/chakra/table';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import PaxeerXReceiptsTableItem from './PaxeerXReceiptsTableItem';

const renderItem = (item = PAXEER_X_RECEIPTS_ITEM) => render(
  <TableRoot variant="scan">
    <TableBody>
      <PaxeerXReceiptsTableItem item={ item }/>
    </TableBody>
  </TableRoot>,
);

describe('PaxeerXReceiptsTableItem', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('marks the row with the receipt it carries and links it to its page', () => {
    const { container } = renderItem();

    const row = container.querySelector(`[data-receipt="${ PAXEER_X_RECEIPTS_ITEM.id }"]`) as HTMLElement;

    expect(row).toBeTruthy();
    expect(row.querySelector(`a[href="/paxeer-x/receipts/${ PAXEER_X_RECEIPTS_ITEM.id }"]`)).toBeTruthy();
  });

  it('offers the receipt and the account with a copy control each', () => {
    renderItem();

    expect(screen.getAllByLabelText('copy')).toHaveLength(2);
  });

  it('orders the cells as receipt, account, block and settlement', () => {
    const { container } = renderItem();

    const cells = container.querySelectorAll('td');

    expect(cells).toHaveLength(4);
    expect(cells[2]?.textContent).toBe(String(PAXEER_X_RECEIPTS_ITEM.block_number));
    expect(cells[3]?.querySelector(`[data-rung="${ PAXEER_X_RECEIPTS_ITEM.status }"]`)).toBeTruthy();
  });

  it('marks an account the receipt log does not carry', () => {
    const { container } = renderItem({ ...PAXEER_X_RECEIPTS_ITEM, account: null });

    expect(container.querySelectorAll('td')[1]?.textContent).toBe('—');
    expect(screen.getAllByLabelText('copy')).toHaveLength(1);
  });
});
