// @vitest-environment jsdom

import { waitFor } from '@testing-library/react';
import React from 'react';

import * as internalTxsMock from 'mocks/txs/internalTxs';
import * as txMock from 'mocks/txs/tx';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TxInternals from './TxInternals';
import type { TxQuery } from './useTxQuery';

vi.setConfig({ testTimeout: 60_000 });

const buildTxQuery = (data: typeof txMock.base | undefined, overrides: Partial<TxQuery> = {}): TxQuery => ({
  data,
  isError: false,
  isPending: false,
  isPlaceholderData: false,
  socketStatus: undefined,
  setRefetchEnabled: () => undefined,
  ...overrides,
} as unknown as TxQuery);

describe('TxInternals', () => {
  beforeEach(() => {
    routerState.pathname = '/tx/[hash]';
    routerState.query = { hash: txMock.base.hash };
    fetchMock.resetMocks();
    fetchMock.mockResponse(
      JSON.stringify(internalTxsMock.baseResponse),
      { headers: { 'Content-Type': 'application/json' } },
    );
  });

  it('holds the internal transactions in the shared table card under its own count line', async() => {
    const { container } = render(<TxInternals txQuery={ buildTxQuery(txMock.base) }/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent)
        .toBe('A total of 3 internal transactions found');
    });

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe('Internal transactions');
  });

  it('renders the rows of the list inside the card body', async() => {
    const { container } = render(<TxInternals txQuery={ buildTxQuery(txMock.base) }/>);

    await waitFor(() => {
      const body = container.querySelector('[data-scan-table-card] [data-body]');

      expect(body?.textContent).toContain('ArianeeStore');
      expect(body?.textContent).toContain('ArianeeCreditHistory');
    }, { timeout: 10_000 });
  });

  it('keeps the card and says so when the transaction made no internal call', async() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(
      JSON.stringify({ items: [], next_page_params: null }),
      { headers: { 'Content-Type': 'application/json' } },
    );

    const { container } = render(<TxInternals txQuery={ buildTxQuery(txMock.base) }/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-body]')?.textContent)
        .toBe('There are no internal transactions for this transaction.');
    });

    expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent)
      .toBe('A total of 0 internal transactions found');
  });

  it('waits for a pending transaction instead of asking for its internal calls', () => {
    const { container } = render(<TxInternals txQuery={ buildTxQuery(txMock.pending) }/>);

    expect(container.textContent).toContain('This transaction is pending confirmation.');
    expect(container.querySelector('[data-scan-table-card]')).toBeNull();
  });

  it('reports a failed transaction request inside the card body', () => {
    const { container } = render(<TxInternals txQuery={ buildTxQuery(undefined, { isError: true }) }/>);

    expect(container.querySelector('[data-scan-table-card] [data-body]')?.textContent)
      .toContain('Something went wrong');
  });
});
