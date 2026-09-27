// @vitest-environment jsdom

import type { UseQueryResult } from '@tanstack/react-query';
import React from 'react';

import type { TokenInfo } from 'types/api/token';
import type { PaginationParams } from 'ui/shared/pagination/types';

import type { ResourceError } from 'lib/api/resources';
import { tokenInfoERC20a } from 'mocks/tokens/tokenInfo';
import * as tokenTransferMock from 'mocks/tokens/tokenTransfer';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import type { QueryWithPagesResult } from 'ui/shared/pagination/useQueryWithPages';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TokenTransfer from './TokenTransfer';

Object.defineProperty(document, 'fonts', {
  configurable: true,
  value: { load: () => Promise.resolve([ {} ]) },
});

const HASH = tokenInfoERC20a.address_hash;

const pagination = (overrides?: Partial<PaginationParams>): PaginationParams => ({
  // the socket channel only listens on the first page, and jsdom carries no socket, so the spec pages forward
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

const items = [ tokenTransferMock.erc20, tokenTransferMock.erc20, tokenTransferMock.erc20 ];

const transfersQuery = (overrides?: Partial<QueryWithPagesResult<'general:token_transfers'>>) => ({
  data: { items, next_page_params: null },
  isError: false,
  isPlaceholderData: false,
  pagination: pagination(),
  ...overrides,
}) as unknown as QueryWithPagesResult<'general:token_transfers'>;

const tokenQuery = (data: TokenInfo = tokenInfoERC20a) => ({
  data,
  isError: false,
  isPlaceholderData: false,
}) as unknown as UseQueryResult<TokenInfo, ResourceError<unknown>>;

describe('TokenTransfer', () => {
  beforeEach(() => {
    routerState.pathname = '/token/[hash]';
    routerState.query = { hash: HASH };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({ items: [], next_page_params: null }), { headers: { 'Content-Type': 'application/json' } });
  });

  it('puts the transfers in the shared table card', () => {
    const { container } = render(<TokenTransfer transfersQuery={ transfersQuery() } tokenQuery={ tokenQuery() }/>);

    expect(container.querySelector('[data-scan-table-card] [data-token-transfer-table]')).not.toBeNull();
  });

  it('counts the transfers the token reports in the card header', () => {
    const { container } = render(
      <TokenTransfer transfersQuery={ transfersQuery() } tokenQuery={ tokenQuery() } transfersCount={ 42 }/>,
    );

    expect(container.querySelector('[data-title]')?.textContent).toBe('Latest 3 from a total of 42 token transfers');
  });

  it('says only that there are more when the token reports no total and a page follows', () => {
    const { container } = render(<TokenTransfer transfersQuery={ transfersQuery() } tokenQuery={ tokenQuery() }/>);

    expect(container.querySelector('[data-title]')?.textContent).toBe('More than 3 token transfers found');
  });

  it('counts the rows it holds when there is no further page', () => {
    const { container } = render(
      <TokenTransfer
        transfersQuery={ transfersQuery({ pagination: pagination({ hasNextPage: false }) }) }
        tokenQuery={ tokenQuery() }
      />,
    );

    expect(container.querySelector('[data-title]')?.textContent).toBe('A total of 3 token transfers found');
  });

  it('offers the show-records selector and the pagination in the card footer', () => {
    const { container } = render(<TokenTransfer transfersQuery={ transfersQuery() } tokenQuery={ tokenQuery() }/>);

    const footer = container.querySelector('[data-footer]') as HTMLElement;
    const showRows = footer.querySelector('[data-scan-show-rows]') as HTMLElement;

    expect(showRows.textContent).toContain('Show');
    expect(showRows.textContent).toContain('Records');
    expect(footer.querySelector('[data-footer-pagination] [data-scan-pagination]')).not.toBeNull();
  });

  it('renders nothing before the page asks for it', () => {
    const { container } = render(
      <TokenTransfer transfersQuery={ transfersQuery() } tokenQuery={ tokenQuery() } shouldRender={ false }/>,
    );

    expect(container.querySelector('[data-scan-table-card]')).toBeNull();
    expect(container.querySelector('[data-token-transfer-table]')).toBeNull();
  });

  it('says so when the token has no transfer', () => {
    const { container } = render(
      <TokenTransfer
        transfersQuery={ transfersQuery({ data: { items: [], next_page_params: null } }) }
        tokenQuery={ tokenQuery() }
      />,
    );

    expect(container.querySelector('[data-body]')?.textContent).toContain('There are no token transfers.');
  });
});
