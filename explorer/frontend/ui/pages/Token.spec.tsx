// @vitest-environment jsdom

import React from 'react';

import * as addressMock from 'mocks/address/address';
import { tokenCounters, tokenInfo } from 'mocks/tokens/tokenInfo';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { waitFor } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_NAVIGATION_LAYOUT: 'horizontal',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// The token lists render fifty placeholder rows of real table items, which jsdom lays out well past the
// default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

import TokenPageContent from './Token';

Object.defineProperty(document, 'fonts', {
  configurable: true,
  value: { load: () => Promise.resolve([ {} ]) },
});

const HASH = tokenInfo.address_hash;

const json = (payload: unknown) => ({
  body: JSON.stringify(payload),
  headers: { 'Content-Type': 'application/json' },
});

const tabTitles = (container: HTMLElement) =>
  Array.from(container.querySelectorAll('[role="tab"]')).map((tab) => tab.textContent ?? '');

describe('TokenPageContent', () => {
  beforeEach(() => {
    routerState.pathname = '/token/[hash]';
    routerState.query = { hash: HASH };
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => {
      const { pathname } = new URL(request.url);

      if (pathname === `/api/v2/tokens/${ HASH }`) {
        return Promise.resolve(json(tokenInfo));
      }

      if (pathname === `/api/v2/tokens/${ HASH }/counters`) {
        return Promise.resolve(json(tokenCounters));
      }

      if (pathname === `/api/v2/addresses/${ HASH }`) {
        return Promise.resolve(json(addressMock.contract));
      }

      return Promise.resolve(json({ items: [], next_page_params: null }));
    });
  });

  it('heads the page with the word Token', () => {
    const { container } = render(<TokenPageContent/>);

    expect(container.querySelector('h1')?.textContent).toContain('Token');
  });

  it('puts the detail cards above the tab strip', () => {
    const { container } = render(<TokenPageContent/>);

    const details = container.querySelector('[data-token-details]') as HTMLElement;
    const tabList = container.querySelector('[role="tablist"]') as HTMLElement;

    expect(details).not.toBeNull();
    expect(tabList).not.toBeNull();
    expect(details.querySelectorAll('[data-token-card]').length).toBeGreaterThanOrEqual(2);
    expect(details.compareDocumentPosition(tabList) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('opens the tab strip on the transfers and the holders', () => {
    const { container } = render(<TokenPageContent/>);

    const titles = tabTitles(container);

    expect(titles[0]).toBe('Token transfers');
    expect(titles[1]).toBe('Holders');
  });

  it('adds the contract tab once the address answered as a contract', async() => {
    const { container } = render(<TokenPageContent/>);

    await waitFor(() => {
      expect(tabTitles(container).some((title) => title.startsWith('Contract'))).toBe(true);
    });
  });

  it('keeps the pagination inside the table cards rather than on the tab row', () => {
    const { container } = render(<TokenPageContent/>);

    const tabList = container.querySelector('[role="tablist"]') as HTMLElement;

    expect(tabList.querySelector('[data-scan-pagination]')).toBeNull();
  });

  it('keeps the chip row of the title above the cards', () => {
    const { container } = render(<TokenPageContent/>);

    const chipRow = container.querySelector('[data-token-chip-row]') as HTMLElement;
    const details = container.querySelector('[data-token-details]') as HTMLElement;

    expect(chipRow).not.toBeNull();
    expect(chipRow.compareDocumentPosition(details) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});
