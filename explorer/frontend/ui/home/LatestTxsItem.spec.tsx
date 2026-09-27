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

import LatestTxsItem from './LatestTxsItem';

vi.setConfig({ testTimeout: 60_000 });

describe('LatestTxsItem', () => {
  it('carries the hash of the transaction and links it', () => {
    const { container } = render(<LatestTxsItem tx={ txMock.base }/>);

    const row = container.querySelector(`[data-latest-tx="${ txMock.base.hash }"]`) as HTMLElement;

    expect(row).not.toBeNull();
    expect(row.querySelector(`a[href="/tx/${ txMock.base.hash }"]`)).not.toBeNull();
  });

  it('names the sender and the recipient of the transaction', () => {
    const { container } = render(<LatestTxsItem tx={ txMock.base }/>);

    const parties = container.querySelector('[data-label="tx-parties"]') as HTMLElement;

    expect(parties.textContent).toContain('From');
    expect(parties.textContent).toContain('To');
    expect(parties.querySelector(`a[href="/address/${ txMock.base.from.hash }"]`)).not.toBeNull();
    expect(parties.querySelector(`a[href="/address/${ txMock.base.to?.hash }"]`)).not.toBeNull();
  });

  it('carries the value and the fee of the transaction', () => {
    const { container } = render(<LatestTxsItem tx={ txMock.base }/>);

    const value = container.querySelector('[data-label="tx-value"]') as HTMLElement;

    expect(value.textContent).toContain('ETH');
    expect(value.textContent).toContain('Fee');
  });
});
