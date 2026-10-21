// @vitest-environment jsdom

import { noop } from 'es-toolkit';
import React from 'react';

import * as searchMock from 'mocks/search/index';
import { render } from 'ui/shared/layout/testWrapper';
import type { Category } from 'ui/shared/search/utils';
import { describe, it, expect, beforeEach, vi } from 'vitest';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_IS_ACCOUNT_SUPPORTED: 'false',
    NEXT_PUBLIC_MARKETPLACE_CONFIG_URL: '',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import useQuickSearchQuery from '../useQuickSearchQuery';
import SearchBarSuggest from './SearchBarSuggest';

const TERM = '0xb64a';
const RESULTS = [ searchMock.address1, searchMock.tx1, searchMock.block1 ];

interface HarnessProps {
  category?: Category;
}

const Harness = ({ category }: HarnessProps): React.JSX.Element => {
  const { debouncedSearchTerm, handleSearchTermChange, query, zetaChainCCTXQuery, externalSearchItem } = useQuickSearchQuery();

  React.useEffect(() => {
    handleSearchTermChange(TERM);
  }, [ handleSearchTermChange ]);

  return (
    <SearchBarSuggest
      query={ query }
      zetaChainCCTXQuery={ zetaChainCCTXQuery }
      externalSearchItem={ externalSearchItem }
      searchTerm={ debouncedSearchTerm }
      onItemClick={ noop }
      category={ category }
    />
  );
};

const shownCategories = (container: HTMLElement): Array<string | null> => Array.from(container.querySelectorAll('[data-scroll-target] [data-id]'))
  .map((item) => item.getAttribute('data-id'));

vi.setConfig({ testTimeout: 60_000 });

describe('SearchBarSuggest', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => {
      const body = request.url.includes('/api/v2/search/quick') ? RESULTS : {};

      return { body: JSON.stringify(body), headers: { 'Content-Type': 'application/json' } };
    });
  });

  it('groups the results under every category they fall in when no category is chosen', async() => {
    const { container } = render(<Harness/>);

    await vi.waitFor(() => {
      expect(shownCategories(container)).toEqual([ 'address', 'transaction', 'block' ]);
    }, { timeout: 30_000, interval: 100 });
  });

  it('narrows the results and drops the category tabs to the chosen category', async() => {
    const { container } = render(<Harness category="block"/>);

    await vi.waitFor(() => {
      expect(shownCategories(container)).toEqual([ 'block' ]);
    }, { timeout: 30_000, interval: 100 });
    expect(container.querySelector('[role="tablist"]')).toBeNull();
  });

  it('reports no results when the chosen category holds none of them', async() => {
    const { container } = render(<Harness category="token"/>);

    await vi.waitFor(() => {
      expect(container.textContent).toContain('No results found.');
    }, { timeout: 30_000, interval: 100 });
    expect(shownCategories(container)).toEqual([]);
  });
});
