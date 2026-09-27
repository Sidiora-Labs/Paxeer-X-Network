// @vitest-environment jsdom

import React from 'react';

import type { AddressesItem, AddressesResponse } from 'types/api/addresses';

import { currencyUnits } from 'lib/units';
import * as addressMock from 'mocks/address/address';
import { publicTag } from 'mocks/address/tag';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// The page renders fifty placeholder rows of real table items before the list answers, which jsdom
// lays out well past the default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

import Accounts from './Accounts';

const items: Array<AddressesItem> = [
  {
    ...addressMock.withName,
    public_tags: [ publicTag ],
    transactions_count: '1234',
    coin_balance: '1000000000000000000000',
  },
  {
    ...addressMock.withoutName,
    transactions_count: '7',
    coin_balance: '500000000000000000000',
  },
];

const response: AddressesResponse = {
  items,
  total_supply: '2000',
  next_page_params: null,
};

const responseInit = { headers: { 'Content-Type': 'application/json' } };

const renderPage = async(payload: AddressesResponse = response) => {
  fetchMock.resetMocks();
  fetchMock.mockResponse(JSON.stringify(payload), responseInit);

  const result = render(<Accounts/>);

  await waitFor(() => expect(result.container.querySelectorAll('[data-address-row]')).toHaveLength(items.length));

  return result;
};

describe('AccountsPageContent', () => {
  beforeEach(() => {
    routerState.pathname = '/accounts';
    routerState.query = {};
    fetchMock.resetMocks();
  });

  it('heads the page with the top accounts title', async() => {
    await renderPage();

    expect(screen.getByRole('heading', { level: 1 }).textContent).toContain('Top accounts');
  });

  it('counts the accounts found with the total balance beside them', async() => {
    const { container } = await renderPage();

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe(`A total of 2 accounts found (2,000 ${ currencyUnits.ether })`);
    expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent)
      .toBe('Showing 2 accounts ranked by balance');
  });

  it('counts past the page it is on when another page follows and shows its pagination', async() => {
    const { container } = await renderPage({
      ...response,
      next_page_params: { fetched_coin_balance: '42', hash: addressMock.hash, items_count: 2 },
    });

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe(`More than 2 accounts found (2,000 ${ currencyUnits.ether })`);
    expect(container.querySelector('[data-scan-table-card] [data-actions] [data-pagination]')).not.toBeNull();
  });

  it('puts the page-data download in the card header and the row selector in its footer', async() => {
    const { container } = await renderPage();

    expect(container.querySelector('[data-scan-table-card] [data-actions] [data-accounts-download]')?.textContent)
      .toContain('Download Page Data');
    expect(container.querySelector('[data-scan-table-card] [data-footer-rows] [data-scan-show-rows]')).not.toBeNull();
  });

  it('lists the scan columns of the account table', async() => {
    const { container } = await renderPage();

    expect(Array.from(container.querySelectorAll('thead th')).map((cell) => cell.textContent)).toEqual([
      '#',
      'Address',
      'Name tag',
      `Balance ${ currencyUnits.ether }`,
      'Percentage',
      'Txn count',
    ]);
  });
});
