// @vitest-environment jsdom

import React from 'react';

import type { PaginationParams } from 'ui/shared/pagination/types';

import * as internalTxsMock from 'mocks/txs/internalTxs';
import { render } from 'ui/shared/layout/testWrapper';
import type { QueryWithPagesResult } from 'ui/shared/pagination/useQueryWithPages';
import { describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import BlockInternalTxs from './BlockInternalTxs';

const pagination = (overrides?: Partial<PaginationParams>): PaginationParams => ({
  page: 3,
  onNextPageClick: vi.fn(),
  onPrevPageClick: vi.fn(),
  resetPage: vi.fn(),
  hasPages: true,
  hasNextPage: true,
  canGoBackwards: true,
  isLoading: false,
  isVisible: true,
  ...overrides,
});

const query = (overrides?: Partial<QueryWithPagesResult<'general:block_internal_txs'>>) => ({
  data: internalTxsMock.baseResponse,
  isError: false,
  isPlaceholderData: false,
  pagination: pagination(),
  ...overrides,
}) as unknown as QueryWithPagesResult<'general:block_internal_txs'>;

describe('BlockInternalTxs', () => {
  it('heads the shared table card with the count the block reports', () => {
    const { container } = render(<BlockInternalTxs query={ query() } itemsCount={ 47 }/>);

    const card = container.querySelector('[data-scan-table-card]');

    expect(card).not.toBeNull();
    expect(card?.querySelector('[data-title]')?.textContent).toBe('A total of 47 internal transactions found');
  });

  it('falls back to the number of rows it received', () => {
    const { container } = render(<BlockInternalTxs query={ query() }/>);

    expect(container.querySelector('[data-title]')?.textContent).toBe('A total of 3 internal transactions found');
  });

  it('drops the block column and keeps the parent transaction, type and value columns', () => {
    const { container } = render(<BlockInternalTxs query={ query() }/>);

    const body = container.querySelector('[data-body]') as HTMLElement;
    const headers = Array.from(body.querySelectorAll('th')).map((cell) => cell.textContent);

    expect(headers.some((header) => header?.startsWith('Parent txn hash'))).toBe(true);
    expect(headers).toContain('Type');
    expect(headers).not.toContain('Block');
    expect(body.textContent).toContain('Call');
    expect(body.textContent).toContain('Static call');
  });

  it('repeats the pagination in the card header and in its footer', () => {
    const { container } = render(<BlockInternalTxs query={ query() }/>);

    expect(container.querySelector('[data-actions] [data-scan-pagination]')).not.toBeNull();
    expect(container.querySelector('[data-footer-pagination] [data-control="page"]')?.textContent).toBe('Page 3');
  });

  it('leaves the card without a footer when there is nothing to page through', () => {
    const { container } = render(<BlockInternalTxs query={ query({ pagination: pagination({ isVisible: false }) }) }/>);

    expect(container.querySelector('[data-footer]')).toBeNull();
  });

  it('says so when the block holds no internal transaction', () => {
    const { container } = render(
      <BlockInternalTxs query={ query({ data: { items: [], next_page_params: null } }) }/>,
    );

    expect(container.querySelector('[data-body]')?.textContent).toContain('There are no internal transactions.');
  });
});
