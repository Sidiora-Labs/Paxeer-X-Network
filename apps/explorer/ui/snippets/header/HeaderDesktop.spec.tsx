// @vitest-environment jsdom

import React from 'react';

import { render } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, vi } from 'vitest';
import { screen } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_NAVIGATION_LAYOUT: 'horizontal',
    NEXT_PUBLIC_IS_ACCOUNT_SUPPORTED: 'false',
  };
});

import HeaderDesktop from './HeaderDesktop';

const renderPageSearchBar = () => <div>page search</div>;

describe('HeaderDesktop', () => {
  it('leaves the content column without a search row when the utility bar carries the search box', () => {
    const { container } = render(<HeaderDesktop/>);

    expect(container.querySelector('[data-label="content-search"]')).toBeNull();
    expect(container.querySelector('header')).toBeNull();
  });

  it('keeps the search row when the page brings its own search box', () => {
    const { container } = render(<HeaderDesktop renderSearchBar={ renderPageSearchBar }/>);

    const row = container.querySelector('[data-label="content-search"]');

    expect(row).toBeTruthy();
    expect(row?.tagName).toBe('HEADER');
    expect(screen.getByText('page search')).toBeTruthy();
  });
});
