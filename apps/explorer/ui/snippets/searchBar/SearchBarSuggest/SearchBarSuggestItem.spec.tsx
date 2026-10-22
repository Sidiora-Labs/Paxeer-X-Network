// @vitest-environment jsdom

import { noop } from 'es-toolkit';
import React from 'react';

import * as searchMock from 'mocks/search/index';
import { render } from 'ui/shared/layout/testWrapper';
import { getItemCategory } from 'ui/shared/search/utils';
import { describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import SearchBarSuggestItem from './SearchBarSuggestItem';

describe('SearchBarSuggestItem', () => {
  it('files a price update under transactions', () => {
    expect(getItemCategory(searchMock.priceUpdate1)).toBe('transaction');
  });

  it('links a price update result to its feed details page and labels it', () => {
    const { container } = render(
      <SearchBarSuggestItem data={ searchMock.priceUpdate1 } isMobile={ false } searchTerm={ searchMock.priceUpdate1.hash } onClick={ noop }/>,
    );

    expect(container.querySelector('a')?.getAttribute('href')).toBe(`/feed/update/${ searchMock.priceUpdate1.hash }`);
    expect(container.textContent).toContain('Price update');
  });
});
