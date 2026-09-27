// @vitest-environment jsdom

import React from 'react';

import * as addressMock from 'mocks/address/address';
import * as txMock from 'mocks/txs/tx';
import { TableBody, TableRoot } from 'toolkit/chakra/table';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TxsTableItem from './TxsTableItem';

const renderItem = (tx = txMock.base) => render(
  <TableRoot variant="scan">
    <TableBody>
      <TxsTableItem tx={ tx } showBlockInfo/>
    </TableBody>
  </TableRoot>,
);

describe('TxsTableItem', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('lays the row out as preview, hash, action, block, age, from and to, amount and fee', () => {
    const { container } = renderItem();

    const cells = Array.from(container.querySelectorAll('td'));

    expect(cells).toHaveLength(8);
    expect(Boolean(cells[0].querySelector('[data-scan-preview]'))).toBe(true);
    expect(cells[1].querySelector('a[href^="/tx/"]')?.getAttribute('href')).toBe(`/tx/${ txMock.base.hash }`);
    expect(cells[2].querySelector('[data-scan-method]')?.getAttribute('data-scan-method')).toBe(txMock.base.method);
    expect(cells[3].querySelector('a[href^="/block/"]')?.getAttribute('href')).toBe(`/block/${ txMock.base.block_number }`);
    expect(cells[4].textContent).not.toBe('');
    expect(Array.from(cells[5].querySelectorAll('a[href^="/address/"]'))).toHaveLength(2);
    expect(cells[6].textContent).toContain('42');
    expect(cells[7].textContent).not.toBe('');
  });

  it('drops the block cell when the list does not show block information', () => {
    const { container } = render(
      <TableRoot variant="scan">
        <TableBody>
          <TxsTableItem tx={ txMock.base } showBlockInfo={ false }/>
        </TableBody>
      </TableRoot>,
    );

    expect(container.querySelectorAll('td')).toHaveLength(7);
    expect(container.querySelector('a[href^="/block/"]')).toBeNull();
  });

  it('marks a reverted transaction on its hash and keeps the method chip', () => {
    const { container } = renderItem(txMock.withDecodedRevertReason);

    const cells = Array.from(container.querySelectorAll('td'));

    expect(cells[1].querySelector('[data-status]')?.getAttribute('data-status')).toBe('error');
    expect(cells[2].querySelector('[data-scan-method]')?.getAttribute('data-scan-method')).toBe(txMock.base.method);
  });

  it('marks the counterparty cell outgoing when the row belongs to the sender', () => {
    const { container } = render(
      <TableRoot variant="scan">
        <TableBody>
          <TxsTableItem tx={ txMock.base } showBlockInfo currentAddress={ txMock.base.from.hash }/>
        </TableBody>
      </TableRoot>,
    );

    const cells = Array.from(container.querySelectorAll('td'));

    expect(cells[5].querySelector('[data-direction]')?.getAttribute('data-direction')).toBe('out');
  });

  it('marks the counterparty cell incoming when the row belongs to the recipient', () => {
    const { container } = render(
      <TableRoot variant="scan">
        <TableBody>
          <TxsTableItem tx={ txMock.base } showBlockInfo currentAddress={ addressMock.hash }/>
        </TableBody>
      </TableRoot>,
    );

    const cells = Array.from(container.querySelectorAll('td'));

    expect(cells[5].querySelector('[data-direction]')?.getAttribute('data-direction')).toBe('in');
  });

  it('carries no direction on a row of the chain-wide list', () => {
    const { container } = renderItem();

    expect(container.querySelector('[data-direction]')).toBeNull();
  });

  it('reads the raw selector as the action of a transaction the indexer has not decoded', () => {
    const { container } = renderItem(txMock.pending);

    const cells = Array.from(container.querySelectorAll('td'));

    expect(cells[2].querySelector('[data-scan-method]')?.getAttribute('data-scan-method')).toBe(txMock.base.raw_input.slice(0, 10));
  });
});
