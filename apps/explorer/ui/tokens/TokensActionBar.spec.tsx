// @vitest-environment jsdom

import React from 'react';

import { render } from 'ui/shared/layout/testWrapper';
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TokensActionBar from './TokensActionBar';

const renderBar = (onSearchChange = vi.fn(), searchTerm = '') => {
  const result = render(
    <TokensActionBar
      searchTerm={ searchTerm }
      onSearchChange={ onSearchChange }
      sort="default"
      onSortChange={ vi.fn() }
      filter={ <div data-test-filter>filter</div> }
    />,
  );

  return { ...result, onSearchChange };
};

describe('TokensActionBar', () => {
  it('gathers the filter, the sort control and the search box into one controls cluster', () => {
    const { container } = renderBar();

    const controls = container.querySelector('[data-tokens-controls]') as HTMLElement;

    expect(controls.querySelector('[data-test-filter]')).not.toBeNull();
    expect(controls.querySelector('input[placeholder="Token name or symbol"]')).not.toBeNull();
  });

  it('opens with the search term the page is already filtered by', () => {
    renderBar(vi.fn(), 'aria');

    expect((screen.getByPlaceholderText('Token name or symbol') as HTMLInputElement).value).toBe('aria');
  });

  it('reports what is typed into the search box', () => {
    const { onSearchChange } = renderBar();

    fireEvent.change(screen.getByPlaceholderText('Token name or symbol'), { target: { value: 'usdc' } });

    expect(onSearchChange).toHaveBeenCalledWith('usdc');
  });
});
