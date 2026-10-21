// @vitest-environment jsdom

import React from 'react';

import * as addressMock from 'mocks/address/address';
import { tokenInfoERC721a } from 'mocks/tokens/tokenInfo';
import * as tokenInstanceMock from 'mocks/tokens/tokenInstance';
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

// The transfer list renders fifty placeholder rows of real table items, which jsdom lays out well past the
// default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

import TokenInstanceContent from './TokenInstance';

Object.defineProperty(document, 'fonts', {
  configurable: true,
  value: { load: () => Promise.resolve([ {} ]) },
});

const HASH = tokenInfoERC721a.address_hash;
const ID = tokenInstanceMock.base.id;

const json = (payload: unknown) => ({
  body: JSON.stringify(payload),
  headers: { 'Content-Type': 'application/json' },
});

const tabTitles = (container: HTMLElement) =>
  Array.from(container.querySelectorAll('[role="tab"]')).map((tab) => tab.textContent ?? '');

describe('TokenInstanceContent', () => {
  beforeEach(() => {
    routerState.pathname = '/token/[hash]/instance/[id]';
    routerState.query = { hash: HASH, id: ID };
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => {
      const { pathname } = new URL(request.url);

      if (pathname === `/api/v2/tokens/${ HASH }`) {
        return Promise.resolve(json(tokenInfoERC721a));
      }

      if (pathname === `/api/v2/tokens/${ HASH }/instances/${ ID }`) {
        return Promise.resolve(json(tokenInstanceMock.base));
      }

      if (pathname.endsWith('/transfers-count')) {
        return Promise.resolve(json({ transfers_count: 42 }));
      }

      if (pathname === `/api/v2/addresses/${ HASH }`) {
        return Promise.resolve(json(addressMock.contract));
      }

      return Promise.resolve(json({ items: [], next_page_params: null }));
    });
  });

  it('heads the page the way the token page does', () => {
    const { container } = render(<TokenInstanceContent/>);

    expect(container.querySelector('h1')?.textContent).toContain('Token instance');
  });

  it('puts the instance cards above the tab strip once the instance answered', async() => {
    const { container } = render(<TokenInstanceContent/>);

    await waitFor(() => {
      const details = container.querySelector('[data-token-instance-details]') as HTMLElement;

      expect(details).not.toBeNull();
      expect(details.querySelectorAll('[data-token-card]').length).toBe(4);
    });

    const details = container.querySelector('[data-token-instance-details]') as HTMLElement;
    const tabList = container.querySelector('[role="tablist"]') as HTMLElement;

    expect(details.compareDocumentPosition(tabList) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('opens the tab strip on the transfers and keeps the metadata tab', () => {
    const { container } = render(<TokenInstanceContent/>);

    const titles = tabTitles(container);

    expect(titles[0]).toBe('Token transfers');
    expect(titles).toContain('Metadata');
  });

  it('adds the holders tab for an instance that is not unique', async() => {
    const { container } = render(<TokenInstanceContent/>);

    await waitFor(() => {
      expect(tabTitles(container)).toContain('Holders');
    });
  });

  it('keeps the pagination inside the table cards rather than on the tab row', () => {
    const { container } = render(<TokenInstanceContent/>);

    const tabList = container.querySelector('[role="tablist"]') as HTMLElement;

    expect(tabList.querySelector('[data-scan-pagination]')).toBeNull();
  });
});
