// @vitest-environment jsdom

import BigNumber from 'bignumber.js';
import React from 'react';

import type { AddressesItem } from 'types/api/addresses';

import * as addressMock from 'mocks/address/address';
import { publicTag } from 'mocks/address/tag';
import { TableBody, TableRoot } from 'toolkit/chakra/table';
import { render } from 'ui/shared/layout/testWrapper';
import { describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import AddressesTableItem, { ADDRESS_VALUE_PLACEHOLDER, getAddressBalance } from './AddressesTableItem';

const tagged: AddressesItem = {
  ...addressMock.withName,
  public_tags: [ publicTag ],
  transactions_count: '1234',
  coin_balance: '1000000000000000000000',
};

const untagged: AddressesItem = {
  ...addressMock.withoutName,
  transactions_count: '7',
  coin_balance: null,
};

const renderItem = (item: AddressesItem, totalSupply = BigNumber('2000'), hasPercentage = true) => render(
  <TableRoot variant="scan">
    <TableBody>
      <AddressesTableItem item={ item } index={ 1 } totalSupply={ totalSupply } hasPercentage={ hasPercentage }/>
    </TableBody>
  </TableRoot>,
);

describe('getAddressBalance', () => {
  it('reads the balance in coins out of the balance in the smallest unit', () => {
    expect(getAddressBalance(tagged).toFixed()).toBe('1000');
  });

  it('reads an account with no balance as zero', () => {
    expect(getAddressBalance(untagged).toFixed()).toBe('0');
  });
});

describe('AddressesTableItem', () => {
  it('lays the account out over the six scan columns', () => {
    const { container } = renderItem(tagged);

    expect(container.querySelectorAll('[data-address-row] td')).toHaveLength(6);
    expect(container.querySelector('[data-address-rank]')?.textContent).toBe('1');
    expect(container.querySelector('[data-address-txn-count]')?.textContent).toBe('1,234');
  });

  it('carries the public tags of the account in their own cell', () => {
    const { container } = renderItem(tagged);

    const cell = container.querySelector('[data-address-name-tag="tags"]') as HTMLElement;

    expect(cell.textContent).toBe(publicTag.display_name);
  });

  it('dashes the name tag of an account that carries none', () => {
    const { container } = renderItem(untagged);

    expect(container.querySelector('[data-address-name-tag="none"]')?.textContent).toBe(ADDRESS_VALUE_PLACEHOLDER);
  });

  it('drops the percentage cell when no total supply is known', () => {
    const { container } = renderItem(tagged, BigNumber('0'), false);

    expect(container.querySelectorAll('[data-address-row] td')).toHaveLength(5);
  });
});
