// @vitest-environment jsdom

import React from 'react';

import * as blockMock from 'mocks/blocks/block';
import { render } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, vi } from 'vitest';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_IS_ACCOUNT_SUPPORTED: 'false',
    NEXT_PUBLIC_MARKETPLACE_CONFIG_URL: '',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import LatestBlocksItem from './LatestBlocksItem';

vi.setConfig({ testTimeout: 60_000 });

describe('LatestBlocksItem', () => {
  it('carries the height, the hash and the transaction count of the block', () => {
    const { container } = render(<LatestBlocksItem block={ blockMock.base }/>);

    const row = container.querySelector(`[data-latest-block="${ blockMock.base.height }"]`) as HTMLElement;

    expect(row.textContent).toContain(String(blockMock.base.height));
    expect(row.textContent).toContain('Hash');
    expect(row.textContent).toContain(`${ blockMock.base.transactions_count } txns`);
    expect(row.querySelector(`a[href="/block/${ blockMock.base.hash }"]`)).not.toBeNull();
  });

  it('carries the reward of the block', () => {
    const { container } = render(<LatestBlocksItem block={ blockMock.base }/>);

    expect(container.querySelector('[data-label="block-reward"]')?.textContent).toContain('ETH');
  });

  it('links the height to the block page', () => {
    const { container } = render(<LatestBlocksItem block={ blockMock.base }/>);

    expect(container.querySelector(`a[href="/block/${ blockMock.base.height }"]`)).not.toBeNull();
  });
});
