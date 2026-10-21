// @vitest-environment jsdom

import React from 'react';

import * as blockMock from 'mocks/blocks/block';
import { TableBody, TableCell, TableRoot, TableRow } from 'toolkit/chakra/table';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import BlocksTable from './BlocksTable';

const data = [ blockMock.base, blockMock.base2 ];

const renderTable = () => render(<BlocksTable data={ data } top={ 0 } page={ 1 }/>);

describe('BlocksTable', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('keeps the block columns in the order the list already reads', () => {
    const { container } = renderTable();

    expect(Array.from(container.querySelectorAll('thead th')).map((cell) => cell.textContent))
      .toEqual([ 'Block', 'Size, bytes', 'Validator', 'Txn', 'Gas used', 'Reward ETH', 'Burnt fees ETH', 'Base fee' ]);
  });

  it('takes the scan table styling from the table recipe', () => {
    const { container } = renderTable();
    const { container: plain } = render(
      <TableRoot>
        <TableBody>
          <TableRow>
            <TableCell>cell</TableCell>
          </TableRow>
        </TableBody>
      </TableRoot>,
    );

    expect(container.querySelector('table')?.className)
      .not.toBe(plain.querySelector('table')?.className);
  });

  it('renders one row per block, each linking its height and its validator', () => {
    const { container } = renderTable();

    const rows = Array.from(container.querySelectorAll('tbody tr'));

    expect(rows).toHaveLength(data.length);
    expect(rows.map((row) => row.querySelector('a[href^="/block/"]')?.getAttribute('href')))
      .toEqual(data.map((block) => `/block/${ block.height }`));
    expect(rows.map((row) => Boolean(row.querySelector('a[href^="/address/"]')))).toEqual([ true, true ]);
  });

  it('reads the size and the transaction count of a block', () => {
    const { container } = renderTable();

    const cells = Array.from(container.querySelectorAll('tbody tr')[0].querySelectorAll('td'));

    expect(cells[1].textContent).toBe(blockMock.base.size?.toLocaleString());
    expect(cells[3].textContent).toBe(String(blockMock.base.transactions_count));
  });
});
