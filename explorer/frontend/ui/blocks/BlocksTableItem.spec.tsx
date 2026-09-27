// @vitest-environment jsdom

import React from 'react';

import * as blockMock from 'mocks/blocks/block';
import { TableBody, TableRoot } from 'toolkit/chakra/table';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import BlocksTableItem from './BlocksTableItem';

const renderItem = (data = blockMock.base) => render(
  <TableRoot variant="scan">
    <TableBody>
      <BlocksTableItem data={ data }/>
    </TableBody>
  </TableRoot>,
);

describe('BlocksTableItem', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('lays the row out as block, size, validator, transactions, gas, reward, burnt fees and base fee', () => {
    const { container } = renderItem();

    const cells = Array.from(container.querySelectorAll('td'));

    expect(cells).toHaveLength(8);
    expect(cells[0].querySelector('a[href^="/block/"]')?.getAttribute('href')).toBe(`/block/${ blockMock.base.height }`);
    expect(cells[1].textContent).toBe(blockMock.base.size?.toLocaleString());
    expect(cells[2].querySelector('a[href^="/address/"]')?.getAttribute('href')).toBe(`/address/${ blockMock.base.miner.hash }`);
    expect(cells[3].querySelector('a')?.getAttribute('href')).toBe(`/block/${ blockMock.base.height }?tab=txs`);
    expect(cells[4].textContent).toContain('544,920');
    expect(cells[5].textContent).not.toBe('');
    expect(cells[6].textContent).not.toBe('');
    expect(cells[7].textContent).not.toBe('');
  });

  it('carries the burnt-fee flame beside the burnt fees', () => {
    const { container } = renderItem();

    const cells = Array.from(container.querySelectorAll('td'));

    expect(cells[6].querySelector('svg')).toBeTruthy();
  });

  it('reads the height of a block that is still being re-synced', () => {
    const { container } = renderItem(blockMock.base2);

    expect(container.querySelector('a[href^="/block/"]')?.getAttribute('href')).toBe(`/block/${ blockMock.base2.height }`);
  });
});
