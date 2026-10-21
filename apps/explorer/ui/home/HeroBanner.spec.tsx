// @vitest-environment jsdom

import React from 'react';

import { render, routerState } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, beforeEach, vi } from 'vitest';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_NAVIGATION_LAYOUT: 'horizontal',
    NEXT_PUBLIC_IS_ACCOUNT_SUPPORTED: 'false',
    NEXT_PUBLIC_MARKETPLACE_CONFIG_URL: '',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import HeroBanner from './HeroBanner';

describe('HeroBanner', () => {
  beforeEach(() => {
    routerState.pathname = '/';
    routerState.query = {};
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('names the network above the search field', () => {
    const { container } = render(<HeroBanner/>);

    const title = container.querySelector('[data-label="hero-title"]') as HTMLElement;
    const search = container.querySelector('[data-label="hero-search"]') as HTMLElement;

    expect(title.textContent).toBe('Blockscout explorer');
    expect(title.compareDocumentPosition(search) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('carries the search field and its submit button in the band', () => {
    const { container } = render(<HeroBanner/>);

    const search = container.querySelector('[data-label="hero-search"]') as HTMLElement;

    expect(search.querySelector('form')).not.toBeNull();
    expect(search.querySelector('input')).not.toBeNull();
    expect(container.querySelector('[data-label="hero-search-submit"]')).not.toBeNull();
  });

  it('puts the filter dropdown on the left of the search field, starting on all filters', () => {
    const { container } = render(<HeroBanner/>);

    const filter = container.querySelector('[data-label="hero-search-filter"]') as HTMLElement;
    const search = container.querySelector('[data-label="hero-search-desktop"]') as HTMLElement;
    const submit = container.querySelector('[data-label="hero-search-submit"]') as HTMLElement;

    expect(filter.textContent).toContain('All filters');
    expect(filter.compareDocumentPosition(search) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(search.compareDocumentPosition(submit) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('submits the search form from the submit button', () => {
    const { container } = render(<HeroBanner/>);

    const form = container.querySelector('[data-label="hero-search-desktop"] form') as HTMLFormElement;
    const submit = container.querySelector('[data-label="hero-search-submit"]') as HTMLElement;
    const onSubmit = vi.fn((event: Event) => event.preventDefault());

    form.addEventListener('submit', onSubmit);
    submit.click();

    expect(onSubmit).toHaveBeenCalledTimes(1);
  });
});
