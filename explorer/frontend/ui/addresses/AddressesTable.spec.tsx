// @vitest-environment jsdom

import BigNumber from 'bignumber.js';
import React from 'react';

import type { AddressesItem } from 'types/api/addresses';

import { currencyUnits } from 'lib/units';
import * as addressMock from 'mocks/address/address';
import { publicTag } from 'mocks/address/tag';
import { render } from 'ui/shared/layout/testWrapper';
import { describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import AddressesTable from './AddressesTable';
import { ADDRESS_VALUE_PLACEHOLDER } from './AddressesTableItem';

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

const TOTAL_SUPPLY = BigNumber('2000');

const renderTable = (totalSupply: BigNumber = TOTAL_SUPPLY, pageStartIndex = 1) => render(
  <AddressesTable items={ items } totalSupply={ totalSupply } pageStartIndex={ pageStartIndex }/>,
);

const rowCells = (container: HTMLElement, index: number) => {
  const row = container.querySelectorAll('[data-address-row]')[index];

  return Array.from(row.querySelectorAll('td')).map((cell) => cell.textContent);
};

describe('AddressesTable', () => {
  it('heads the table with the scan columns in their order', () => {
    const { container } = renderTable();

    expect(Array.from(container.querySelectorAll('thead th')).map((cell) => cell.textContent)).toEqual([
      '#',
      'Address',
      'Name tag',
      `Balance ${ currencyUnits.ether }`,
      'Percentage',
      'Txn count',
    ]);
  });

  it('drops the percentage column when no total supply is known', () => {
    const { container } = renderTable(BigNumber('0'));

    expect(Array.from(container.querySelectorAll('thead th')).map((cell) => cell.textContent)).toEqual([
      '#',
      'Address',
      'Name tag',
      `Balance ${ currencyUnits.ether }`,
      'Txn count',
    ]);
  });

  it('numbers the rows from the index the page starts at', () => {
    const { container } = renderTable(TOTAL_SUPPLY, 51);

    expect(Array.from(container.querySelectorAll('[data-address-rank]')).map((cell) => cell.textContent))
      .toEqual([ '51', '52' ]);
  });

  it('fills the row with the address, its name tag, its balance, its share and its transactions', () => {
    const { container } = renderTable();

    const cells = rowCells(container, 0);

    expect(cells[1]).toContain(addressMock.withName.name);
    expect(cells[2]).toBe(publicTag.display_name);
    expect(cells[3]).toBe('1,000');
    expect(cells[4]).toBe('50%');
    expect(cells[5]).toBe('1,234');
    expect(rowCells(container, 1)[2]).toBe(ADDRESS_VALUE_PLACEHOLDER);
  });
});
