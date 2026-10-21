// @vitest-environment jsdom

import React from 'react';

import type { TokenInfo } from 'types/api/token';

import * as tokenMock from 'mocks/tokens/tokenInfo';
import { render } from 'ui/shared/layout/testWrapper';
import useQueryWithPages from 'ui/shared/pagination/useQueryWithPages';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// The list renders the real table and the real list item for every token, which jsdom lays out well
// past the default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

import Tokens from './Tokens';

const scamToken: TokenInfo = { ...tokenMock.tokenInfoERC20c, reputation: 'scam' };

const items = [ tokenMock.tokenInfo, tokenMock.tokenInfoERC20b, scamToken ];

const responseInit = { headers: { 'Content-Type': 'application/json' } };

interface HarnessProps {
  rowsCount?: number;
}

const Harness = ({ rowsCount }: HarnessProps) => {
  const query = useQueryWithPages({ resourceName: 'general:tokens' });

  return (
    <Tokens
      query={ query }
      hasActiveFilters={ false }
      rowsCount={ rowsCount }
      actions={ <div data-test-actions>actions</div> }
      pagination={ <div data-test-pagination>pagination</div> }
      showRows={ <div data-test-show-rows>show rows</div> }
    />
  );
};

const renderList = async(props?: HarnessProps) => {
  const result = render(<Harness rowsCount={ props?.rowsCount }/>);

  await waitFor(() => expect(result.container.querySelectorAll('[data-token-row]').length).toBeGreaterThan(0));

  return result;
};

describe('Tokens', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({ items, next_page_params: null }), responseInit);
  });

  it('counts the token contracts found in the card header', async() => {
    const { container } = await renderList();

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe('A total of 3 token contracts found');
  });

  it('notes how many of them the reputation keeps', async() => {
    const { container } = await renderList();

    expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent)
      .toBe('Showing 2 tokens with an ok or a neutral reputation');
  });

  it('counts past the page it is on when another page follows', async() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(
      JSON.stringify({ items, next_page_params: { holders_count: 1, items_count: 3, name: '', market_cap: null } }),
      responseInit,
    );

    const { container } = await renderList();

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe('More than 3 token contracts found');
  });

  it('puts the actions and the pagination in the header and the row selector in the footer', async() => {
    const { container } = await renderList();

    expect(container.querySelector('[data-scan-table-card] [data-actions] [data-test-actions]')).not.toBeNull();
    expect(container.querySelector('[data-scan-table-card] [data-actions] [data-test-pagination]')).not.toBeNull();
    expect(container.querySelector('[data-scan-table-card] [data-footer-rows] [data-test-show-rows]')).not.toBeNull();
    expect(container.querySelector('[data-scan-table-card] [data-footer-pagination] [data-test-pagination]')).not.toBeNull();
  });

  it('shows only as many rows as the row selector asks for', async() => {
    const { container } = await renderList({ rowsCount: 2 });

    expect(container.querySelectorAll('[data-token-row]')).toHaveLength(2);
  });
});
