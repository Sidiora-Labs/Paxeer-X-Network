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

  it('stacks the time and the tags under the hash instead of giving the tags a column', () => {
    const { container } = render(<LatestTxsItem tx={ txMock.base }/>);

    const row = container.querySelector(`[data-latest-tx="${ txMock.base.hash }"]`) as HTMLElement;
    const columns = Array.from(row.children);
    const hashColumn = columns.find((column) => column.querySelector(`a[href="/tx/${ txMock.base.hash }"]`)) as HTMLElement;
    const tags = hashColumn.querySelector('[data-label="tx-tags"]') as HTMLElement;

    expect(tags).not.toBeNull();
    expect(tags.parentElement?.children.length).toBeGreaterThan(1);
    expect(columns.map((column) => column.getAttribute('data-label'))).not.toContain('tx-tags');
  });

  it('follows the hash with the parties and then the value as their own columns', () => {
    const { container } = render(<LatestTxsItem tx={ txMock.base }/>);

    const row = container.querySelector(`[data-latest-tx="${ txMock.base.hash }"]`) as HTMLElement;
    const columns = Array.from(row.children);
    const labels = columns.map((column) => column.getAttribute('data-label'));
    const hashIndex = columns.findIndex((column) => column.querySelector(`a[href="/tx/${ txMock.base.hash }"]`));

    expect(labels.indexOf('tx-parties')).toBe(hashIndex + 1);
    expect(labels.indexOf('tx-value')).toBe(hashIndex + 2);
  });
});
