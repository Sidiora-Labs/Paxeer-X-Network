// @vitest-environment jsdom

import BigNumber from 'bignumber.js';
import React from 'react';

import * as tokenMock from 'mocks/tokens/tokenInfo';
import { TableBody, TableRoot } from 'toolkit/chakra/table';
import { render } from 'ui/shared/layout/testWrapper';
import { describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TokensTableItem, { getNativePrice, getOnchainMarketCap, TOKEN_VALUE_PLACEHOLDER } from './TokensTableItem';

const renderItem = (token: typeof tokenMock.tokenInfo, coinPrice?: string) => render(
  <TableRoot variant="scan">
    <TableBody>
      <TokensTableItem token={ token } index={ 0 } page={ 1 } coinPrice={ coinPrice }/>
    </TableBody>
  </TableRoot>,
);

describe('getOnchainMarketCap', () => {
  it('takes the supply the contract reports at the price the list carries', () => {
    expect(getOnchainMarketCap(tokenMock.tokenInfoERC20b)?.toFixed())
      .toBe(BigNumber('900000000000000000000000000').div(BigNumber(10).pow(6)).multipliedBy('0.982').toFixed());
  });

  it('answers nothing when the list carries no price', () => {
    expect(getOnchainMarketCap(tokenMock.tokenInfoERC20a)).toBeUndefined();
  });
});

describe('getNativePrice', () => {
  it('divides the token price by the coin price', () => {
    expect(getNativePrice('2.0101', '1.5')?.toFixed()).toBe(BigNumber('2.0101').div(1.5).toFixed());
  });

  it('answers nothing without a coin price and nothing at a coin price of zero', () => {
    expect(getNativePrice('2.0101', null)).toBeUndefined();
    expect(getNativePrice('2.0101', '0')).toBeUndefined();
  });
});

describe('TokensTableItem', () => {
  it('lays the token out over the eight scan columns', () => {
    const { container } = renderItem(tokenMock.tokenInfo);

    expect(container.querySelectorAll('[data-token-row] td')).toHaveLength(8);
    expect(container.querySelector('[data-token-rank]')?.textContent).toBe('1');
    expect(container.querySelector('[data-token-holders]')?.textContent).toBe('46,554');
  });

  it('dashes the change and the volume the payload does not carry', () => {
    const { container } = renderItem(tokenMock.tokenInfo);

    const placeholders = Array.from(container.querySelectorAll('[data-token-value-placeholder]'));

    expect(placeholders).toHaveLength(2);
    expect(placeholders.map((item) => item.textContent)).toEqual([ TOKEN_VALUE_PLACEHOLDER, TOKEN_VALUE_PLACEHOLDER ]);
  });

  it('writes the coin-denominated price only when the coin price is known', () => {
    const { container: withoutPrice } = renderItem(tokenMock.tokenInfo);

    expect(withoutPrice.querySelector('[data-token-native-price]')).toBeNull();

    const { container: withPrice } = renderItem(tokenMock.tokenInfo, '1.5');

    expect(withPrice.querySelector('[data-token-native-price]')?.textContent).toContain('1.340067');
  });
});
