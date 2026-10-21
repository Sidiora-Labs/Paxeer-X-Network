// @vitest-environment jsdom

import React from 'react';

import type { PaginationParams } from 'ui/shared/pagination/types';

import * as addressMock from 'mocks/address/address';
import * as statsMock from 'mocks/stats/index';
import * as txMock from 'mocks/txs/tx';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TxsContent from './TxsContent';

const items = [ txMock.base, { ...txMock.base2 }, { ...txMock.base3 } ];

const paginationParams = (overrides?: Partial<PaginationParams>): PaginationParams => ({
  page: 1,
  onNextPageClick: () => undefined,
  onPrevPageClick: () => undefined,
  resetPage: () => undefined,
  hasPages: false,
  hasNextPage: false,
  canGoBackwards: false,
  isLoading: false,
  isVisible: true,
  ...overrides,
});

describe('TxsContent', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => {
      if (request.url.includes('/api/v2/stats')) {
        return { body: JSON.stringify(statsMock.base), headers: { 'Content-Type': 'application/json' } };
      }

      return { body: JSON.stringify({}), headers: { 'Content-Type': 'application/json' } };
    });
  });

  it('heads the card with the chain-wide count line and the record note', async() => {
    const { container } = render(
      <TxsContent
        pagination={ paginationParams() }
        items={ items }
        isPlaceholderData={ false }
        isError={ false }
        sort="default"
        socketType="txs_validated"
        stickyHeader={ false }
      />,
    );

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
        .toBe('Latest 3 from a total of 82,258,122 transactions');
    });

    expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent)
      .toBe('Showing page 1 of the records the node returns, newest first');
  });

  it('carries the pagination in the card header and repeats it in the footer', () => {
    const { container } = render(
      <TxsContent
        pagination={ paginationParams({ hasPages: true }) }
        items={ items }
        isPlaceholderData={ false }
        isError={ false }
        sort="default"
        socketType="txs_validated"
        stickyHeader={ false }
      />,
    );

    expect(container.querySelector('[data-actions] [data-pagination]')).toBeTruthy();
    expect(container.querySelector('[data-footer-pagination] [data-pagination]')).toBeTruthy();
  });

  it('counts the records of an address list and offers the download beside the count', () => {
    const { container } = render(
      <TxsContent
        pagination={ paginationParams() }
        items={ items }
        isPlaceholderData={ false }
        isError={ false }
        sort="default"
        currentAddress={ addressMock.hash }
        stickyHeader={ false }
      />,
    );

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe('A total of 3 transactions found');
    expect(container.querySelector('[data-actions] a[href^="/csv-export"]')).toBeTruthy();
  });

  it('reads more than the records it has when another page follows', () => {
    const { container } = render(
      <TxsContent
        pagination={ paginationParams({ hasPages: true, hasNextPage: true }) }
        items={ items }
        isPlaceholderData={ false }
        isError={ false }
        sort="default"
        currentAddress={ addressMock.hash }
        stickyHeader={ false }
      />,
    );

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe('More than 3 transactions found');
  });

  it('renders the rows alone and leaves the count, the download and the pagination to a caller that owns the card', () => {
    const { container } = render(
      <TxsContent
        pagination={ paginationParams({ hasPages: true }) }
        items={ items }
        isPlaceholderData={ false }
        isError={ false }
        sort="default"
        currentAddress={ addressMock.hash }
        stickyHeader={ false }
        isInsideTableCard
      />,
    );

    const hashes = Array.from(container.querySelectorAll('tbody tr'))
      .map((row) => row.querySelector('a[href^="/tx/"]')?.getAttribute('href'))
      .filter(Boolean);

    expect(container.querySelector('[data-scan-table-card]')).toBeNull();
    expect(container.querySelector('[data-csv-export-label]')).toBeNull();
    expect(container.querySelector('[data-pagination]')).toBeNull();
    expect(hashes).toEqual(items.map((item) => `/tx/${ item.hash }`));
  });

  it('says there are no transactions when the list is empty', () => {
    const { container } = render(
      <TxsContent
        pagination={ paginationParams({ isVisible: false }) }
        items={ [] }
        isPlaceholderData={ false }
        isError={ false }
        sort="default"
        stickyHeader={ false }
      />,
    );

    expect(container.querySelector('[data-scan-table-card]')).toBeNull();
    expect(container.textContent).toContain('There are no transactions.');
  });
});
