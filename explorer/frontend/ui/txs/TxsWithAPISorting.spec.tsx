// @vitest-environment jsdom

import React from 'react';

import type { TransactionsSortingValue } from 'types/api/transaction';

import * as addressMock from 'mocks/address/address';
import * as txMock from 'mocks/txs/tx';
import { render } from 'ui/shared/layout/testWrapper';
import useQueryWithPages from 'ui/shared/pagination/useQueryWithPages';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TxsWithAPISorting from './TxsWithAPISorting';

const items = [ txMock.base, txMock.base2, txMock.base3 ];

const Harness = ({ isInsideTableCard }: { isInsideTableCard?: boolean }) => {
  const [ sort, setSort ] = React.useState<TransactionsSortingValue>('default');

  const query = useQueryWithPages({
    resourceName: 'general:address_txs',
    pathParams: { hash: addressMock.hash },
  });

  return (
    <TxsWithAPISorting
      query={ query }
      currentAddress={ addressMock.hash }
      socketType="address_txs"
      sorting={ sort }
      setSort={ setSort }
      showBlockInfo
      isInsideTableCard={ isInsideTableCard }
    />
  );
};

const renderedHashes = (container: HTMLElement) => Array.from(container.querySelectorAll('tbody tr'))
  .map((row) => row.querySelector('a[href^="/tx/"]')?.getAttribute('href'))
  .filter(Boolean);

describe('TxsWithAPISorting', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => {
      if (request.url.includes('/api/v2/addresses/')) {
        return { body: JSON.stringify({ items, next_page_params: null }), headers: { 'Content-Type': 'application/json' } };
      }

      return { body: JSON.stringify({}), headers: { 'Content-Type': 'application/json' } };
    });
  });

  it('opens the table card itself when no caller provides one', async() => {
    const { container } = render(<Harness/>);

    await waitFor(() => {
      expect(renderedHashes(container)).toEqual(items.map((item) => `/tx/${ item.hash }`));
    });

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe('A total of 3 transactions found');
  });

  it('renders the rows alone when it is told the caller already opens the card', async() => {
    const { container } = render(<Harness isInsideTableCard/>);

    await waitFor(() => {
      expect(renderedHashes(container)).toEqual(items.map((item) => `/tx/${ item.hash }`));
    });

    expect(container.querySelector('[data-scan-table-card]')).toBeNull();
    expect(container.querySelector('[data-csv-export-label]')).toBeNull();
  });
});
