// @vitest-environment jsdom

import React from 'react';

import { currencyUnits } from 'lib/units';
import * as tokenMock from 'mocks/tokens/tokenInfo';
import { render } from 'ui/shared/layout/testWrapper';
import { describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TokensListItem from './TokensListItem';
import { TOKEN_VALUE_PLACEHOLDER } from './TokensTableItem';

const FIELDS = [
  'Price',
  'Change (%)',
  'Volume (24H)',
  'Circulating market cap',
  'Onchain market cap',
  'Holders',
];

describe('TokensListItem', () => {
  it('carries the same fields as the desktop columns in the same order', () => {
    const { container } = render(<TokensListItem token={ tokenMock.tokenInfo } index={ 0 } page={ 1 }/>);

    expect(Array.from(container.querySelectorAll('[data-token-field]')).map((row) => row.getAttribute('data-token-field')))
      .toEqual(FIELDS);
  });

  it('heads the item with the rank and the token', () => {
    const { container } = render(<TokensListItem token={ tokenMock.tokenInfo } index={ 1 } page={ 2 }/>);

    expect(container.querySelector('[data-token-rank]')?.textContent).toBe('52');
    expect(container.textContent).toContain('ARIANEE (ARIA)');
  });

  it('writes the figures the payload carries and dashes the ones it does not', () => {
    const { container } = render(<TokensListItem token={ tokenMock.tokenInfo } index={ 0 } page={ 1 } coinPrice="1.5"/>);

    expect(container.querySelector('[data-token-field="Price"]')?.textContent)
      .toBe(`Price$2.01011.340067 ${ currencyUnits.ether }`);
    expect(container.querySelector('[data-token-field="Change (%)"]')?.textContent)
      .toBe(`Change (%)${ TOKEN_VALUE_PLACEHOLDER }`);
    expect(container.querySelector('[data-token-field="Volume (24H)"]')?.textContent)
      .toBe(`Volume (24H)${ TOKEN_VALUE_PLACEHOLDER }`);
    expect(container.querySelector('[data-token-field="Circulating market cap"]')?.textContent)
      .toBe('Circulating market cap$117,629,601.62');
    expect(container.querySelector('[data-token-field="Holders"]')?.textContent).toBe('Holders46,554');
  });
});
