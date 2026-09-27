// @vitest-environment jsdom

import React from 'react';

import type { PaginationParams } from 'ui/shared/pagination/types';

import * as depositsMock from 'mocks/deposits/deposits';
import { render } from 'ui/shared/layout/testWrapper';
import type { QueryWithPagesResult } from 'ui/shared/pagination/useQueryWithPages';
import { describe, expect, it, vi } from 'vitest';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_HAS_BEACON_CHAIN: 'true',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import BlockDeposits from './BlockDeposits';

const pagination = (overrides?: Partial<PaginationParams>): PaginationParams => ({
  page: 1,
  onNextPageClick: vi.fn(),
  onPrevPageClick: vi.fn(),
  resetPage: vi.fn(),
  hasPages: true,
  hasNextPage: true,
  canGoBackwards: false,
  isLoading: false,
  isVisible: true,
  ...overrides,
});

const query = (overrides?: Partial<QueryWithPagesResult<'general:block_deposits'>>) => ({
  data: depositsMock.data,
  isError: false,
  isPlaceholderData: false,
  pagination: pagination(),
  ...overrides,
}) as unknown as QueryWithPagesResult<'general:block_deposits'>;

describe('BlockDeposits', () => {
  it('heads the shared table card with the count the block reports', () => {
    const { container } = render(<BlockDeposits blockDepositsQuery={ query() } itemsCount={ 9 }/>);

    const card = container.querySelector('[data-scan-table-card]');

    expect(card).not.toBeNull();
    expect(card?.querySelector('[data-title]')?.textContent).toBe('A total of 9 deposits found');
  });

  it('falls back to the number of rows it received', () => {
    const { container } = render(<BlockDeposits blockDepositsQuery={ query() }/>);

    expect(container.querySelector('[data-title]')?.textContent).toBe('A total of 3 deposits found');
  });

  it('drops the block and timestamp columns the block page already states', () => {
    const { container } = render(<BlockDeposits blockDepositsQuery={ query() }/>);

    const body = container.querySelector('[data-body]') as HTMLElement;
    const headers = Array.from(body.querySelectorAll('th')).map((cell) => cell.textContent);

    expect(headers).toContain('Transaction hash');
    expect(headers).toContain('PubKey');
    expect(headers).toContain('Status');
    expect(headers).not.toContain('Block');
    expect(headers.some((header) => header?.startsWith('Timestamp'))).toBe(false);
  });

  it('repeats the pagination in the card header and in its footer', () => {
    const { container } = render(<BlockDeposits blockDepositsQuery={ query() }/>);

    expect(container.querySelector('[data-actions] [data-scan-pagination]')).not.toBeNull();
    expect(container.querySelector('[data-footer-pagination] [data-control="page"]')?.textContent).toBe('Page 1');
  });

  it('leaves the card without a footer when there is nothing to page through', () => {
    const { container } = render(
      <BlockDeposits blockDepositsQuery={ query({ pagination: pagination({ isVisible: false }) }) }/>,
    );

    expect(container.querySelector('[data-footer]')).toBeNull();
  });

  it('says so when the block holds no deposit', () => {
    const { container } = render(
      <BlockDeposits blockDepositsQuery={ query({ data: { items: [], next_page_params: null } }) }/>,
    );

    expect(container.querySelector('[data-body]')?.textContent).toContain('There are no deposits for this block.');
  });
});
