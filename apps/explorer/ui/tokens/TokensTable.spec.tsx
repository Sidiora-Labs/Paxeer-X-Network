// @vitest-environment jsdom

import React from 'react';

import { currencyUnits } from 'lib/units';
import * as tokenMock from 'mocks/tokens/tokenInfo';
import { render } from 'ui/shared/layout/testWrapper';
import { describe, expect, it, vi } from 'vitest';
import { fireEvent } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TokensTable from './TokensTable';
import { TOKEN_VALUE_PLACEHOLDER } from './TokensTableItem';

const items = [ tokenMock.tokenInfo, tokenMock.tokenInfoERC20b ];

const COLUMNS = [
  '#',
  'Token',
  'Price',
  'Change (%)',
  'Volume (24H)',
  'Circulating market cap',
  'Onchain market cap',
  'Holders',
];

const rowCells = (container: HTMLElement, index: number) => {
  const row = container.querySelectorAll('[data-token-row]')[index];

  return Array.from(row.querySelectorAll('td')).map((cell) => cell.textContent);
};

describe('TokensTable', () => {
  it('heads the table with the scan columns in their order', () => {
    const { container } = render(<TokensTable items={ items } page={ 1 }/>);

    expect(Array.from(container.querySelectorAll('thead th')).map((cell) => cell.textContent)).toEqual(COLUMNS);
  });

  it('numbers the rows from the page they belong to', () => {
    const { container } = render(<TokensTable items={ items } page={ 3 }/>);

    expect(Array.from(container.querySelectorAll('[data-token-rank]')).map((cell) => cell.textContent))
      .toEqual([ '101', '102' ]);
  });

  it('fills the row with the token, the price, the capitalisations and the holders the payload carries', () => {
    const { container } = render(<TokensTable items={ items } page={ 1 }/>);

    const cells = rowCells(container, 0);

    expect(cells[1]).toContain('ARIANEE (ARIA)');
    expect(cells[1]).toContain('ERC-20');
    expect(cells[2]).toContain('$2.0101');
    expect(cells[5]).toBe('$117,629,601.62');
    expect(cells[7]).toBe('46,554');
    expect(rowCells(container, 1)[6]).toBe('$883,800,000,000,000,000,000');
  });

  it('dashes the columns the list endpoint answers with no figure', () => {
    const { container } = render(<TokensTable items={ items } page={ 1 }/>);

    const cells = rowCells(container, 0);

    expect(cells[3]).toBe(TOKEN_VALUE_PLACEHOLDER);
    expect(cells[4]).toBe(TOKEN_VALUE_PLACEHOLDER);
  });

  it('writes the coin-denominated price beneath the fiat price', () => {
    const { container } = render(<TokensTable items={ items } page={ 1 } coinPrice="1.5"/>);

    expect(container.querySelector('[data-token-row] [data-token-native-price]')?.textContent)
      .toBe(`1.340067 ${ currencyUnits.ether }`);
  });

  it('asks for a sort from the price, the circulating capitalisation and the holders and from no other column', () => {
    const setSorting = vi.fn();
    const { container } = render(
      <TokensTable items={ items } page={ 1 } sorting="default" setSorting={ setSorting }/>,
    );

    const headers = Array.from(container.querySelectorAll('thead th')) as Array<HTMLElement>;

    const asked = headers.map((header) => {
      setSorting.mockClear();
      fireEvent.click((header.firstElementChild as HTMLElement | null) ?? header);

      return setSorting.mock.calls.map(([ value ]) => value)[0];
    });

    expect(asked).toEqual([
      undefined,
      undefined,
      'fiat_value-desc',
      undefined,
      undefined,
      'circulating_market_cap-desc',
      undefined,
      'holders_count-desc',
    ]);
  });
});
