// @vitest-environment jsdom

import React from 'react';

import * as blockMock from 'mocks/blocks/block';
import * as statsMock from 'mocks/stats/index';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { screen } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_STATS_API_HOST: '',
    NEXT_PUBLIC_IS_ACCOUNT_SUPPORTED: 'false',
    NEXT_PUBLIC_MARKETPLACE_CONFIG_URL: '',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import { HomeRpcDataContextProvider } from './fallbacks/rpcDataContext';
import LatestBlocks from './LatestBlocks';

const renderCard = () => render(
  <HomeRpcDataContextProvider>
    <LatestBlocks/>
  </HomeRpcDataContextProvider>,
);

vi.setConfig({ testTimeout: 60_000 });

describe('LatestBlocks', () => {
  beforeEach(() => {
    routerState.pathname = '/';
    routerState.query = {};
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => {
      const body = request.url.includes('/api/v2/main-page/blocks') ?
        [ blockMock.base, blockMock.base2 ] :
        statsMock.base;

      return { body: JSON.stringify(body), headers: { 'Content-Type': 'application/json' } };
    });
  });

  it('heads the card with the latest blocks and the network utilization', async() => {
    const { container } = renderCard();

    await screen.findByText('Latest blocks');

    await vi.waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent).toContain('1.55%');
    }, { timeout: 30_000, interval: 100 });

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent).toBe('Latest blocks');
    expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent).toContain('Network utilization:');
  });

  it('lists the blocks newest first', async() => {
    const { container } = renderCard();

    await screen.findByText('Latest blocks');

    await vi.waitFor(() => {
      expect(Array.from(container.querySelectorAll('[data-latest-block]')).map((item) => item.getAttribute('data-latest-block')))
        .toEqual([ String(blockMock.base.height), String(blockMock.base2.height) ]);
    }, { timeout: 30_000, interval: 100 });
  });

  it('closes the card with the link to every block', async() => {
    const { container } = renderCard();

    await screen.findByText('Latest blocks');

    const footer = container.querySelector('[data-label="view-all-blocks"] a') as HTMLAnchorElement;

    expect(footer.textContent).toBe('View all blocks');
    expect(footer.getAttribute('href')).toBe('/blocks');
  });
});
