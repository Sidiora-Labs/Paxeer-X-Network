// @vitest-environment jsdom

import React from 'react';

import * as searchMock from 'mocks/search/index';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { fireEvent } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_IS_ACCOUNT_SUPPORTED: 'false',
    NEXT_PUBLIC_MARKETPLACE_CONFIG_URL: '',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import SearchBarDesktop from './SearchBarDesktop';

const TERM = '0xb64a';
const RESULTS = [ searchMock.address1, searchMock.tx1, searchMock.block1 ];

const shownCategories = (): Array<string | null> => Array.from(document.body.querySelectorAll('[data-scroll-target] [data-id]'))
  .map((item) => item.getAttribute('data-id'));

function typeTerm(container: HTMLElement): void {
  const input = container.querySelector('input') as HTMLInputElement;

  fireEvent.focus(input);
  fireEvent.change(input, { target: { value: TERM } });
}

vi.setConfig({ testTimeout: 60_000 });

describe('SearchBarDesktop', () => {
  beforeEach(() => {
    routerState.pathname = '/';
    routerState.query = {};
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => {
      const body = request.url.includes('/api/v2/search/quick') ? RESULTS : {};

      return { body: JSON.stringify(body), headers: { 'Content-Type': 'application/json' } };
    });
  });

  it('suggests every category the results fall in when no category is chosen', async() => {
    const { container } = render(<SearchBarDesktop isHeroBanner/>);

    typeTerm(container);

    await vi.waitFor(() => {
      expect(shownCategories()).toEqual([ 'address', 'transaction', 'block' ]);
    }, { timeout: 30_000, interval: 100 });
  });

  it('passes the chosen category to the suggestions so only that category is listed', async() => {
    const { container } = render(<SearchBarDesktop isHeroBanner category="transaction"/>);

    typeTerm(container);

    await vi.waitFor(() => {
      expect(shownCategories()).toEqual([ 'transaction' ]);
    }, { timeout: 30_000, interval: 100 });
  });
});
