// @vitest-environment jsdom

import React from 'react';

import * as paxeerXMock from 'mocks/paxeerX/unifiedAccount';
import { TableBody, TableRoot } from 'toolkit/chakra/table';
import { describe, expect, it } from 'vitest';
import { screen } from 'vitest/lib';

import ActivityListItem from './ActivityListItem';
import { render } from './testWrapper';

const transfer = paxeerXMock.unifiedAccount.activity[1];

const renderItem = (item: typeof transfer) => render(
  <TableRoot variant="scan">
    <TableBody>
      <ActivityListItem item={ item }/>
    </TableBody>
  </TableRoot>,
);

describe('ActivityListItem', () => {
  it('names the action on a method chip and the side of the network under it', () => {
    const { container } = renderItem(transfer);

    const row = container.querySelector(`[data-activity="${ transfer.hash }"]`) as HTMLElement;

    expect(row).toBeTruthy();
    expect(row.querySelector('[data-scan-method]')?.getAttribute('data-scan-method')).toBe('Token transfer');
    expect(row.querySelector('td:first-child')?.textContent).toBe('Token transferChain');
  });

  it('links the transaction and the block it sits in', () => {
    const { container } = renderItem(transfer);

    expect(container.querySelector(`a[href="/tx/${ transfer.hash }"]`)).toBeTruthy();
    expect(container.querySelector(`a[href="/block/${ transfer.block_number }"]`)?.textContent).toBe(String(transfer.block_number));
  });

  it('puts the row on its rung of the settlement ladder', () => {
    const { container } = renderItem(transfer);

    expect(container.querySelector(`[data-rung="${ transfer.status }"]`)).toBeTruthy();
  });

  it('scales the amount by the asset and marks an entry that carries none', () => {
    renderItem(transfer);

    expect(screen.getByText('2.5 USDX')).toBeTruthy();

    const { container } = renderItem({ ...transfer, amount: null, asset: null });

    expect(container.querySelectorAll('[data-activity] td')[3]?.textContent).toBe('—');
  });
});
