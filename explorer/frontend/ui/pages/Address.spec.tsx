// @vitest-environment jsdom

import React from 'react';

import * as addressMock from 'mocks/address/address';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_NAVIGATION_LAYOUT: 'horizontal',
    NEXT_PUBLIC_PAXEER_X_ENABLED: 'true',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// The address lists render fifty placeholder rows of real table items, which jsdom lays out well past the
// default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

import AddressPageContent from './Address';

const HASH = addressMock.hash;

const tabTitles = (container: HTMLElement) =>
  Array.from(container.querySelectorAll('[role="tab"]')).map((item) => item.textContent ?? '');

describe('AddressPageContent', () => {
  beforeEach(() => {
    routerState.pathname = '/address/[hash]';
    routerState.query = { hash: HASH };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({ items: [], next_page_params: null }), { headers: { 'Content-Type': 'application/json' } });
  });

  it('heads the page with the address label rather than a details heading', () => {
    const { container } = render(<AddressPageContent/>);

    const heading = container.querySelector('h1')?.textContent ?? '';

    expect([ 'Address', 'Contract' ]).toContain(heading);
    expect(heading.endsWith('details')).toBe(false);
  });

  it('puts the icon action row beneath the title', () => {
    const { container } = render(<AddressPageContent/>);

    expect(container.querySelector('[data-address-actions]')).toBeTruthy();
  });

  it('renders the three detail cards above the tab strip', () => {
    const { container } = render(<AddressPageContent/>);

    const details = container.querySelector('[data-address-details]') as Element;
    const tabList = container.querySelector('[role="tablist"]') as Element;

    expect(details).toBeTruthy();
    expect(tabList).toBeTruthy();
    expect(details.querySelectorAll('[data-address-card]')).toHaveLength(3);
    expect(details.compareDocumentPosition(tabList) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('opens the tab strip on the transaction lists and keeps no separate details tab', () => {
    const { container } = render(<AddressPageContent/>);

    const titles = tabTitles(container);

    expect(titles[0].startsWith('Transactions')).toBe(true);
    expect(titles[1].startsWith('Internal transactions')).toBe(true);
    expect(titles[2].startsWith('Token transfers')).toBe(true);
    expect(titles[3].startsWith('Tokens')).toBe(true);
    expect(titles.some((title) => title.startsWith('Details'))).toBe(false);
  });

  it('keeps the coin balance history and the advanced filter on the tab row', () => {
    const { container } = render(<AddressPageContent/>);

    expect(tabTitles(container).some((title) => title.startsWith('Coin balance history'))).toBe(true);
    expect(container.querySelector('a[href^="/advanced-filter"]')).toBeTruthy();
  });
});
