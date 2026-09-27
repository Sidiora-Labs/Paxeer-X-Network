// @vitest-environment jsdom

import React from 'react';

import type { PaginationParams } from 'ui/shared/pagination/types';

import { tokenHoldersERC20 } from 'mocks/tokens/tokenHolders';
import { tokenInfo } from 'mocks/tokens/tokenInfo';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import type { QueryWithPagesResult } from 'ui/shared/pagination/useQueryWithPages';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TokenHolders from './TokenHolders';

Object.defineProperty(document, 'fonts', {
  configurable: true,
  value: { load: () => Promise.resolve([ {} ]) },
});

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

const holdersQuery = (overrides?: Partial<QueryWithPagesResult<'general:token_holders'>>) => ({
  data: tokenHoldersERC20,
  isError: false,
  isPlaceholderData: false,
  pagination: pagination(),
  ...overrides,
}) as unknown as QueryWithPagesResult<'general:token_holders'>;

describe('TokenHolders', () => {
  beforeEach(() => {
    routerState.pathname = '/token/[hash]';
    routerState.query = { hash: tokenInfo.address_hash, tab: 'holders' };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({ items: [], next_page_params: null }), { headers: { 'Content-Type': 'application/json' } });
  });

  it('puts the holders table in the shared table card', () => {
    const { container } = render(<TokenHolders holdersQuery={ holdersQuery() } token={ tokenInfo }/>);

    const card = container.querySelector('[data-scan-table-card]') as HTMLElement;

    expect(card).not.toBeNull();
    expect(card.querySelector('[data-body] table')).not.toBeNull();
  });

  it('counts the holders the token reports', () => {
    const { container } = render(<TokenHolders holdersQuery={ holdersQuery() } token={ tokenInfo } holdersCount={ 8838883 }/>);

    expect(container.querySelector('[data-title]')?.textContent).toBe('Latest 2 from a total of 8,838,883 holders');
  });

  it('says only that there are more when no total was reported', () => {
    const { container } = render(<TokenHolders holdersQuery={ holdersQuery() } token={ tokenInfo }/>);

    expect(container.querySelector('[data-title]')?.textContent).toBe('More than 2 holders found');
  });

  it('offers the holders download beside the count line', () => {
    const { container } = render(<TokenHolders holdersQuery={ holdersQuery() } token={ tokenInfo }/>);

    const actions = container.querySelector('[data-actions]') as HTMLElement;

    expect(actions.textContent).toContain('Download Page Data');
  });

  it('offers the show-records selector in the card footer', () => {
    const { container } = render(<TokenHolders holdersQuery={ holdersQuery() } token={ tokenInfo }/>);

    expect(container.querySelector('[data-footer-rows] [data-scan-show-rows]')).not.toBeNull();
  });

  it('reports the fetch failure instead of an empty card', () => {
    const { container } = render(<TokenHolders holdersQuery={ holdersQuery({ isError: true }) } token={ tokenInfo }/>);

    expect(container.querySelector('[data-scan-table-card]')).toBeNull();
  });

  it('says so when nobody holds the token', () => {
    const { container } = render(
      <TokenHolders holdersQuery={ holdersQuery({ data: { items: [], next_page_params: null } }) } token={ tokenInfo }/>,
    );

    expect(container.querySelector('[data-body]')?.textContent).toContain('There are no holders for this token.');
  });
});
