// @vitest-environment jsdom

import React from 'react';

import type { PaxeerXReceiptsItem } from 'types/api/paxeerXLists';

import { PAXEER_X_RECEIPTS_ITEM } from 'stubs/paxeerXLists';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import PaxeerXReceiptsTable from './PaxeerXReceiptsTable';

const accountless: PaxeerXReceiptsItem = {
  ...PAXEER_X_RECEIPTS_ITEM,
  id: '0x11223344556677889900aabbccddeeff0f1e2d3c4b5a69788796a5b4c3d2e1f0',
  account: null,
  status: 'final',
  block_number: PAXEER_X_RECEIPTS_ITEM.block_number - 1,
};

const items = [ PAXEER_X_RECEIPTS_ITEM, accountless ];

const renderTable = () => render(<PaxeerXReceiptsTable items={ items }/>);

describe('PaxeerXReceiptsTable', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('heads the table with the receipt columns in order', () => {
    const { container } = renderTable();

    expect(Array.from(container.querySelectorAll('thead th')).map((cell) => cell.textContent))
      .toEqual([ 'Receipt ID', 'Account', 'Block', 'Settlement' ]);
  });

  it('renders one row per receipt, each marked by its id', () => {
    const { container } = renderTable();

    expect(container.querySelector('[data-label="paxeer-x-receipts"]')).toBeTruthy();
    expect(Array.from(container.querySelectorAll('[data-receipt]')).map((row) => row.getAttribute('data-receipt')))
      .toEqual(items.map((item) => item.id));
  });

  it('links a receipt id to its own page and the block to the block page', () => {
    const { container } = renderTable();

    const row = container.querySelector(`[data-receipt="${ PAXEER_X_RECEIPTS_ITEM.id }"]`) as HTMLElement;

    expect(row.querySelector(`a[href="/paxeer-x/receipts/${ PAXEER_X_RECEIPTS_ITEM.id }"]`)).toBeTruthy();
    expect(row.querySelector(`a[href="/block/${ PAXEER_X_RECEIPTS_ITEM.block_number }"]`)?.textContent)
      .toBe(String(PAXEER_X_RECEIPTS_ITEM.block_number));
  });

  it('marks a receipt the log carries without an account', () => {
    const { container } = renderTable();

    const row = container.querySelector(`[data-receipt="${ accountless.id }"]`) as HTMLElement;

    expect(row.querySelectorAll('td')[1]?.textContent).toBe('—');
  });

  it('puts every row on its own rung of the settlement ladder', () => {
    const { container } = renderTable();

    expect(Array.from(container.querySelectorAll('[data-receipt] [data-rung]')).map((badge) => badge.getAttribute('data-rung')))
      .toEqual(items.map((item) => item.status));
  });
});
