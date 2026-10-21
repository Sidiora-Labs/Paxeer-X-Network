// @vitest-environment jsdom

import React from 'react';

import * as paxeerXMock from 'mocks/paxeerX/unifiedAccount';
import { TableBody, TableRoot } from 'toolkit/chakra/table';
import { describe, expect, it } from 'vitest';
import { fireEvent, screen } from 'vitest/lib';

import AssetListItem from './AssetListItem';
import { render } from './testWrapper';

const tokenBalance = paxeerXMock.unifiedAccount.balances[1];
const custodyBalance = paxeerXMock.unifiedAccount.balances[0];

const renderItem = (item: typeof tokenBalance) => render(
  <TableRoot variant="scan">
    <TableBody>
      <AssetListItem item={ item }/>
    </TableBody>
  </TableRoot>,
);

describe('AssetListItem', () => {
  it('names the asset by its symbol and repeats the kernel asset id under it', () => {
    const { container } = renderItem(tokenBalance);

    const row = container.querySelector(`[data-asset="${ paxeerXMock.tokenAsset.id }"]`) as HTMLElement;

    expect(row).toBeTruthy();
    expect(row.querySelector('td:first-child')?.textContent).toContain(paxeerXMock.tokenAsset.symbol);
    expect(row.querySelector('[data-label="asset-id"]')?.textContent).toBe(paxeerXMock.tokenAsset.id);
  });

  it('leaves out the second line for an asset known by its id alone', () => {
    const { container } = renderItem(custodyBalance);

    expect(container.querySelector('[data-label="asset-id"]')).toBeNull();
    expect(container.querySelector('[data-label="total"]')?.textContent).toBe('1,200');
  });

  it('scales the total by the decimals of the asset', () => {
    const { container } = renderItem(tokenBalance);

    expect(container.querySelector('[data-label="total"]')?.textContent).toBe('2.5');
  });

  it('opens the breakdown of the total onto one row of labelled parts', () => {
    const { container } = renderItem(tokenBalance);

    expect(container.querySelector('[data-parts-of]')).toBeNull();

    fireEvent.click(screen.getByLabelText(`Show ${ paxeerXMock.tokenAsset.symbol } breakdown`));

    const parts = container.querySelector(`[data-parts-of="${ paxeerXMock.tokenAsset.id }"]`) as HTMLElement;

    expect(parts.querySelectorAll('[data-part]')).toHaveLength(3);
    expect(screen.getByText('On chain')).toBeTruthy();
    expect(screen.getByText('In custody')).toBeTruthy();
    expect(screen.getByText('In kernel')).toBeTruthy();
    expect(parts.querySelector('[data-part="chain"]')?.textContent).toBe('2.5');
  });
});
