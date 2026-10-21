// @vitest-environment jsdom

import React from 'react';

import { render, routerState } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, beforeEach, vi } from 'vitest';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_IS_ACCOUNT_SUPPORTED: 'false',
    NEXT_PUBLIC_MARKETPLACE_CONFIG_URL: '',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import LatestBlocksDegraded from './LatestBlocksDegraded';
import { HomeRpcDataContextProvider } from './rpcDataContext';

vi.setConfig({ testTimeout: 60_000 });

const renderList = (maxNum: number) => render(
  <HomeRpcDataContextProvider>
    <LatestBlocksDegraded maxNum={ maxNum }/>
  </HomeRpcDataContextProvider>,
);

describe('LatestBlocksDegraded', () => {
  beforeEach(() => {
    routerState.pathname = '/';
    routerState.query = {};
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({ jsonrpc: '2.0', id: 1, result: null }));
  });

  it('holds a row for every block the card asks for while the node answer is on its way', () => {
    const { container } = renderList(3);

    expect(container.querySelectorAll('[data-latest-block]')).toHaveLength(3);
  });

  it('closes the list with the link to every block', () => {
    const { container } = renderList(3);

    const link = container.querySelector('a[href="/blocks"]') as HTMLAnchorElement;

    expect(link.textContent).toBe('View all blocks');
  });
});
