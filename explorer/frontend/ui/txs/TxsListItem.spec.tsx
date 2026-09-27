// @vitest-environment jsdom

import React from 'react';

import * as addressMock from 'mocks/address/address';
import * as txMock from 'mocks/txs/tx';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TxsListItem from './TxsListItem';

const renderItem = (tx = txMock.base) => render(<TxsListItem tx={ tx } showBlockInfo/>);

describe('TxsListItem', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('keeps the mobile record in the same order as the table row', () => {
    const { container } = renderItem();

    const text = container.textContent ?? '';

    expect(text.indexOf('Method')).toBeGreaterThan(-1);
    expect(text.indexOf('Block')).toBeGreaterThan(text.indexOf('Method'));
    expect(text.indexOf('Value')).toBeGreaterThan(text.indexOf('Block'));
    expect(text.indexOf('Fee')).toBeGreaterThan(text.indexOf('Value'));
  });

  it('reads the method through the shared scan chip', () => {
    const { container } = renderItem();

    expect(container.querySelector('[data-scan-method]')?.getAttribute('data-scan-method')).toBe(txMock.base.method);
  });

  it('falls back to the raw selector when the method is not decoded', () => {
    const { container } = renderItem(txMock.pending);

    expect(container.querySelector('[data-scan-method]')?.getAttribute('data-scan-method')).toBe(txMock.base.raw_input.slice(0, 10));
  });

  it('marks the direction of a record the current address received', () => {
    const { container } = render(<TxsListItem tx={ txMock.base } showBlockInfo currentAddress={ addressMock.hash }/>);

    expect(container.querySelector('[data-direction]')?.getAttribute('data-direction')).toBe('in');
  });

  it('carries no direction on a record of the chain-wide list', () => {
    const { container } = renderItem();

    expect(container.querySelector('[data-direction]')).toBeNull();
  });

  it('links the transaction, its block and both of its addresses', () => {
    const { container } = renderItem();

    expect(container.querySelector('a[href^="/tx/"]')?.getAttribute('href')).toBe(`/tx/${ txMock.base.hash }`);
    expect(container.querySelector('a[href^="/block/"]')?.getAttribute('href')).toBe(`/block/${ txMock.base.block_number }`);
    expect(container.querySelectorAll('a[href^="/address/"]')).toHaveLength(2);
  });
});
