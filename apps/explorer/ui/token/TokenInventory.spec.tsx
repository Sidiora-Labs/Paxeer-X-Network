// @vitest-environment jsdom

import type { UseQueryResult } from '@tanstack/react-query';
import React from 'react';

import type { TokenInfo } from 'types/api/token';
import type { PaginationParams } from 'ui/shared/pagination/types';

import type { ResourceError } from 'lib/api/resources';
import * as addressMock from 'mocks/address/address';
import { tokenInfoERC721a } from 'mocks/tokens/tokenInfo';
import * as tokenInstanceMock from 'mocks/tokens/tokenInstance';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import type { QueryWithPagesResult } from 'ui/shared/pagination/useQueryWithPages';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TokenInventory from './TokenInventory';

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

const items = [ tokenInstanceMock.base, { ...tokenInstanceMock.base, id: '2' } ];

const inventoryQuery = (overrides?: Partial<QueryWithPagesResult<'general:token_inventory'>>) => ({
  data: { items, next_page_params: null },
  isError: false,
  isPlaceholderData: false,
  pagination: pagination(),
  onFilterChange: vi.fn(),
  ...overrides,
}) as unknown as QueryWithPagesResult<'general:token_inventory'>;

const tokenQuery = () => ({
  data: tokenInfoERC721a,
  isError: false,
  isPlaceholderData: false,
}) as unknown as UseQueryResult<TokenInfo, ResourceError<unknown>>;

describe('TokenInventory', () => {
  beforeEach(() => {
    routerState.pathname = '/token/[hash]';
    routerState.query = { hash: tokenInfoERC721a.address_hash, tab: 'inventory' };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({ items: [], next_page_params: null }), { headers: { 'Content-Type': 'application/json' } });
  });

  it('puts the inventory grid in the shared table card', () => {
    const { container } = render(<TokenInventory inventoryQuery={ inventoryQuery() } tokenQuery={ tokenQuery() }/>);

    expect(container.querySelector('[data-scan-table-card] [data-inventory-grid]')).not.toBeNull();
  });

  it('renders one card per instance of the page', () => {
    const { container } = render(<TokenInventory inventoryQuery={ inventoryQuery() } tokenQuery={ tokenQuery() }/>);

    expect(container.querySelectorAll('[data-inventory-item]')).toHaveLength(2);
  });

  it('says only that there are more while a further page follows', () => {
    const { container } = render(<TokenInventory inventoryQuery={ inventoryQuery() } tokenQuery={ tokenQuery() }/>);

    expect(container.querySelector('[data-title]')?.textContent).toBe('More than 2 tokens found');
  });

  it('counts the collection when the page reports its size', () => {
    const { container } = render(
      <TokenInventory inventoryQuery={ inventoryQuery() } tokenQuery={ tokenQuery() } inventoryCount={ 105 }/>,
    );

    expect(container.querySelector('[data-title]')?.textContent).toBe('Latest 2 from a total of 105 tokens');
  });

  it('shows the active owner filter beside the count line', () => {
    const { container } = render(
      <TokenInventory inventoryQuery={ inventoryQuery() } tokenQuery={ tokenQuery() } ownerFilter={ addressMock.hash }/>,
    );

    const filter = container.querySelector('[data-actions] [data-inventory-owner-filter]') as HTMLElement;

    expect(filter).not.toBeNull();
    expect(filter.textContent).toContain('Filtered by owner');
  });

  it('offers the show-records selector in the card footer', () => {
    const { container } = render(<TokenInventory inventoryQuery={ inventoryQuery() } tokenQuery={ tokenQuery() }/>);

    expect(container.querySelector('[data-footer-rows] [data-scan-show-rows]')).not.toBeNull();
  });

  it('says so when the owner filter matches nothing', () => {
    const { container } = render(
      <TokenInventory
        inventoryQuery={ inventoryQuery({ data: { items: [], next_page_params: null } }) }
        tokenQuery={ tokenQuery() }
        ownerFilter={ addressMock.hash }
      />,
    );

    expect(container.querySelector('[data-body]')?.textContent).toContain('No tokens found for the selected owner.');
  });
});
