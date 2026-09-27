// @vitest-environment jsdom

import React from 'react';

import * as statsMock from 'mocks/stats/index';
import * as tokenMock from 'mocks/tokens/tokenInfo';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// The page renders fifty placeholder rows of real table items before the list answers, which jsdom
// lays out well past the default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

import Tokens from './Tokens';

const items = [ tokenMock.tokenInfo, tokenMock.tokenInfoERC20b, tokenMock.tokenInfoERC20c ];

const responseInit = { headers: { 'Content-Type': 'application/json' } };

const renderPage = async() => {
  const result = render(<Tokens/>);

  await waitFor(() => expect(result.container.querySelectorAll('[data-token-row]')).toHaveLength(items.length));

  return result;
};

describe('TokensPageContent', () => {
  beforeEach(() => {
    routerState.pathname = '/tokens';
    routerState.query = {};
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => Promise.resolve({
      body: JSON.stringify(
        request.url.includes('/stats') ? statsMock.base : { items, next_page_params: null },
      ),
      ...responseInit,
    }));
  });

  it('heads the page with the token tracker title', async() => {
    await renderPage();

    expect(screen.getByRole('heading', { level: 1 }).textContent).toContain('Token tracker');
  });

  it('counts the token contracts found in the card header', async() => {
    const { container } = await renderPage();

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe('A total of 3 token contracts found');
  });

  it('puts the search and the filter controls in the card header', async() => {
    const { container } = await renderPage();

    const actions = container.querySelector('[data-scan-table-card] [data-actions]') as HTMLElement;

    expect(actions.querySelector('[data-tokens-controls]')).not.toBeNull();
    expect(actions.querySelector('input[placeholder="Token name or symbol"]')).not.toBeNull();
  });

  it('closes the card with the row selector and the pagination', async() => {
    const { container } = await renderPage();

    expect(container.querySelector('[data-scan-table-card] [data-footer-rows] [data-scan-show-rows]')).not.toBeNull();
    expect(container.querySelector('[data-scan-table-card] [data-footer-pagination]')).not.toBeNull();
  });

  it('lists the scan columns and prices the tokens in the coin the page reads from the statistics', async() => {
    const { container } = await renderPage();

    expect(Array.from(container.querySelectorAll('thead th')).map((cell) => cell.textContent)).toEqual([
      '#',
      'Token',
      'Price',
      'Change (%)',
      'Volume (24H)',
      'Circulating market cap',
      'Onchain market cap',
      'Holders',
    ]);
    await waitFor(() => {
      expect(container.querySelector('[data-token-row] [data-token-native-price]')?.textContent).toContain('1,006.67074');
    });
  });
});
