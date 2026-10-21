// @vitest-environment jsdom

import React from 'react';

import type { BlockWithdrawalsResponse } from 'types/api/block';
import type { PaginationParams } from 'ui/shared/pagination/types';

import * as withdrawalsMock from 'mocks/withdrawals/withdrawals';
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

import BlockWithdrawals from './BlockWithdrawals';

// The block endpoint answers with the withdrawal fields of the block payload, so the spec narrows
// the repository's withdrawal mock to exactly those fields.
const payload: BlockWithdrawalsResponse = {
  items: withdrawalsMock.data.items.map((item) => ({
    amount: item.amount,
    index: item.index,
    receiver: item.receiver,
    validator_index: item.validator_index,
  })),
  next_page_params: { index: 11639, items_count: 50 },
};

const pagination = (overrides?: Partial<PaginationParams>): PaginationParams => ({
  page: 2,
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

const query = (overrides?: Partial<QueryWithPagesResult<'general:block_withdrawals'>>) => ({
  data: payload,
  isError: false,
  isPlaceholderData: false,
  pagination: pagination(),
  ...overrides,
}) as unknown as QueryWithPagesResult<'general:block_withdrawals'>;

describe('BlockWithdrawals', () => {
  it('puts the withdrawals in the shared table card under a count line', () => {
    const { container } = render(<BlockWithdrawals blockWithdrawalsQuery={ query() } itemsCount={ 12 }/>);

    const card = container.querySelector('[data-scan-table-card]');

    expect(card).not.toBeNull();
    expect(card?.querySelector('[data-title]')?.textContent).toBe('A total of 12 withdrawals found');
  });

  it('counts the rows it holds when the block carries no total', () => {
    const { container } = render(<BlockWithdrawals blockWithdrawalsQuery={ query() }/>);

    expect(container.querySelector('[data-title]')?.textContent).toBe('A total of 3 withdrawals found');
  });

  it('repeats the pagination in the card header and in its footer', () => {
    const { container } = render(<BlockWithdrawals blockWithdrawalsQuery={ query() }/>);

    const header = container.querySelector('[data-actions]');
    const footer = container.querySelector('[data-footer-pagination]');

    expect(header?.querySelector('[data-scan-pagination]')).not.toBeNull();
    expect(footer?.querySelector('[data-scan-pagination]')).not.toBeNull();
    expect(footer?.querySelector('[data-control="page"]')?.textContent).toBe('Page 2');
  });

  it('leaves the card without a footer when there is nothing to page through', () => {
    const { container } = render(
      <BlockWithdrawals blockWithdrawalsQuery={ query({ pagination: pagination({ isVisible: false }) }) }/>,
    );

    expect(container.querySelector('[data-footer]')).toBeNull();
  });

  it('renders each withdrawal of the payload in the card body', () => {
    const { container } = render(<BlockWithdrawals blockWithdrawalsQuery={ query() }/>);

    const body = container.querySelector('[data-body]') as HTMLElement;

    payload.items.forEach((item) => {
      expect(body.textContent).toContain(String(item.index));
      expect(body.textContent).toContain(String(item.validator_index));
    });
  });

  it('says so when the block holds no withdrawal', () => {
    const { container } = render(
      <BlockWithdrawals blockWithdrawalsQuery={ query({ data: { items: [], next_page_params: null } }) }/>,
    );

    expect(container.querySelector('[data-body]')?.textContent).toContain('There are no withdrawals for this block.');
  });
});
