// @vitest-environment jsdom

import React from 'react';

import * as txMock from 'mocks/txs/tx';
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

import LatestTxsItemMobile from './LatestTxsItemMobile';

vi.setConfig({ testTimeout: 60_000 });

describe('LatestTxsItemMobile', () => {
  it('stacks the hash, the parties and the value of the transaction', () => {
    const { container } = render(<LatestTxsItemMobile tx={ txMock.base }/>);

    const row = container.querySelector(`[data-latest-tx="${ txMock.base.hash }"]`) as HTMLElement;
    const parts = Array.from(row.querySelectorAll('[data-label]'))
      .map((item) => item.getAttribute('data-label'))
      .filter((label) => label !== null);

    expect(row.querySelector(`a[href="/tx/${ txMock.base.hash }"]`)).not.toBeNull();
    expect(parts).toEqual([ 'tx-parties', 'tx-tags', 'tx-value' ]);
  });

  it('names the sender and the recipient of the transaction', () => {
    const { container } = render(<LatestTxsItemMobile tx={ txMock.base }/>);

    const parties = container.querySelector('[data-label="tx-parties"]') as HTMLElement;

    expect(parties.querySelector(`a[href="/address/${ txMock.base.from.hash }"]`)).not.toBeNull();
    expect(parties.querySelector(`a[href="/address/${ txMock.base.to?.hash }"]`)).not.toBeNull();
  });

  it('carries the value and the fee of the transaction', () => {
    const { container } = render(<LatestTxsItemMobile tx={ txMock.base }/>);

    const value = container.querySelector('[data-label="tx-value"]') as HTMLElement;

    expect(value.textContent).toContain('ETH');
    expect(value.textContent).toContain('Fee');
  });
});
