// @vitest-environment jsdom

import React from 'react';

import * as blockMock from 'mocks/blocks/block';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import BlocksListItem from './BlocksListItem';

const renderItem = (data = blockMock.base) => render(<BlocksListItem data={ data }/>);

describe('BlocksListItem', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('keeps the mobile record in the same order as the table row', () => {
    const { container } = renderItem();

    const text = container.textContent ?? '';

    expect(text.indexOf('Size')).toBeGreaterThan(-1);
    expect(text.indexOf('Validator')).toBeGreaterThan(text.indexOf('Size'));
    expect(text.indexOf('Txn')).toBeGreaterThan(text.indexOf('Validator'));
    expect(text.indexOf('Gas used')).toBeGreaterThan(text.indexOf('Txn'));
    expect(text.indexOf('Reward')).toBeGreaterThan(text.indexOf('Gas used'));
    expect(text.indexOf('Burnt fees')).toBeGreaterThan(text.indexOf('Reward'));
    expect(text.indexOf('Base fee')).toBeGreaterThan(text.indexOf('Burnt fees'));
  });

  it('links the height, the validator and the transactions of the block', () => {
    const { container } = renderItem();

    expect(container.querySelector('a[href^="/block/"]')?.getAttribute('href')).toBe(`/block/${ blockMock.base.height }`);
    expect(container.querySelector('a[href^="/address/"]')?.getAttribute('href')).toBe(`/address/${ blockMock.base.miner.hash }`);
    expect(container.querySelector(`a[href="/block/${ blockMock.base.height }?tab=txs"]`)).toBeTruthy();
  });

  it('reads the size of the block in bytes', () => {
    const { container } = renderItem();

    expect(container.textContent).toContain(`${ blockMock.base.size?.toLocaleString() } bytes`);
  });

  it('carries the burnt-fee flame beside the burnt fees', () => {
    const { container } = renderItem(blockMock.base2);

    expect(container.textContent).toContain('Burnt fees');
    expect(container.querySelectorAll('svg').length).toBeGreaterThan(0);
  });
});
