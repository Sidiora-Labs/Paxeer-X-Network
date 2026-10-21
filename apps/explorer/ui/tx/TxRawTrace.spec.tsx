// @vitest-environment jsdom

import React from 'react';

import * as txMock from 'mocks/txs/tx';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TxRawTrace from './TxRawTrace';
import type { TxQuery } from './useTxQuery';

const buildTxQuery = (data: typeof txMock.base | undefined, overrides: Partial<TxQuery> = {}): TxQuery => ({
  data,
  isError: false,
  isPending: false,
  isPlaceholderData: false,
  socketStatus: undefined,
  setRefetchEnabled: () => undefined,
  ...overrides,
} as unknown as TxQuery);

describe('TxRawTrace', () => {
  beforeEach(() => {
    routerState.pathname = '/tx/[hash]';
    routerState.query = { hash: txMock.base.hash };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify([]), { headers: { 'Content-Type': 'application/json' } });
  });

  it('heads the shared table card with the raw trace title', () => {
    const { container } = render(<TxRawTrace txQuery={ buildTxQuery(txMock.base) }/>);

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent).toBe('Raw trace');
  });

  it('renders the trace snippet inside the card body', () => {
    const { container } = render(<TxRawTrace txQuery={ buildTxQuery(txMock.base) }/>);

    const body = container.querySelector('[data-scan-table-card] [data-body]');

    expect(body?.querySelector('section')).not.toBeNull();
    expect(body?.textContent).toContain('[]');
  });

  it('waits for a pending transaction instead of asking for its trace', () => {
    const { container } = render(<TxRawTrace txQuery={ buildTxQuery(txMock.pending) }/>);

    expect(container.textContent).toContain('This transaction is pending confirmation.');
    expect(container.querySelector('[data-scan-table-card]')).toBeNull();
  });

  it('reports a failed transaction request instead of the card', () => {
    const { container } = render(<TxRawTrace txQuery={ buildTxQuery(undefined, { isError: true }) }/>);

    expect(container.textContent).toContain('Something went wrong');
    expect(container.querySelector('[data-scan-table-card]')).toBeNull();
  });
});
