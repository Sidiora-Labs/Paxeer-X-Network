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

import AddressesListItem from './AddressesListItem';
import { ADDRESS_VALUE_PLACEHOLDER } from './AddressesTableItem';

const item: AddressesItem = {
  ...addressMock.withName,
  public_tags: [ publicTag ],
  transactions_count: '1234',
  coin_balance: '1000000000000000000000',
};

const untagged: AddressesItem = {
  ...addressMock.withoutName,
  transactions_count: '7',
  coin_balance: '500000000000000000000',
};

const renderItem = (listItem: AddressesItem, totalSupply = BigNumber('2000')) => render(
  <AddressesListItem item={ listItem } index={ 1 } totalSupply={ totalSupply }/>,
);

const fieldsOf = (container: HTMLElement) =>
  Array.from(container.querySelectorAll('[data-address-field]')).map((row) => row.getAttribute('data-address-field'));

describe('AddressesListItem', () => {
  it('carries the same fields as the desktop columns in the same order', () => {
    const { container } = renderItem(item);

    expect(fieldsOf(container)).toEqual([ 'Name tag', `Balance ${ currencyUnits.ether }`, 'Percentage', 'Txn count' ]);
  });

  it('drops the percentage field when no total supply is known', () => {
    const { container } = renderItem(item, BigNumber('0'));

    expect(fieldsOf(container)).toEqual([ 'Name tag', `Balance ${ currencyUnits.ether }`, 'Txn count' ]);
  });

  it('heads the item with the rank and the address', () => {
    const { container } = renderItem(item);

    expect(container.querySelector('[data-address-rank]')?.textContent).toBe('1');
    expect(container.textContent).toContain(addressMock.withName.name);
  });

  it('writes the tag, the balance, the share and the transactions the account carries', () => {
    const { container } = renderItem(item);

    expect(container.querySelector('[data-address-field="Name tag"]')?.textContent)
      .toBe(`Name tag${ publicTag.display_name }`);
    expect(container.querySelector(`[data-address-field="Balance ${ currencyUnits.ether }"]`)?.textContent)
      .toBe(`Balance ${ currencyUnits.ether }1,000`);
    expect(container.querySelector('[data-address-field="Percentage"]')?.textContent).toBe('Percentage50%');
    expect(container.querySelector('[data-address-field="Txn count"]')?.textContent).toBe('Txn count1,234');
  });

  it('dashes the name tag of an account that carries none', () => {
    const { container } = renderItem(untagged);

    expect(container.querySelector('[data-address-field="Name tag"]')?.textContent)
      .toBe(`Name tag${ ADDRESS_VALUE_PLACEHOLDER }`);
  });
});
