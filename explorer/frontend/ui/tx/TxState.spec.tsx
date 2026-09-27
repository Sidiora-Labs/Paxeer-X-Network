// @vitest-environment jsdom

import { waitFor } from '@testing-library/react';
import React from 'react';

import * as stateMock from 'mocks/txs/state';
import * as txMock from 'mocks/txs/tx';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TxState from './TxState';
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

describe('TxState', () => {
  beforeEach(() => {
    routerState.pathname = '/tx/[hash]';
    routerState.query = { hash: txMock.base.hash };
    fetchMock.resetMocks();
    fetchMock.mockResponse(
      JSON.stringify(stateMock.baseResponse),
      { headers: { 'Content-Type': 'application/json' } },
    );
  });

  it('explains the state changes above the shared table card', async() => {
    const { container } = render(<TxState txQuery={ buildTxQuery(txMock.base) }/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent).toBe('State changes');
    });

    const card = container.querySelector('[data-scan-table-card]');

    expect(card?.previousElementSibling?.textContent)
      .toContain('A set of information that represents the current state is updated when a transaction takes place on the network.');
  });

  it('counts the changes the node still has more of', async() => {
    const { container } = render(<TxState txQuery={ buildTxQuery(txMock.base) }/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent)
        .toBe('More than 6 state changes found');
    });

    expect(container.querySelector('[data-scan-table-card] [data-actions] [data-scan-pagination]')).not.toBeNull();
    expect(container.querySelector('[data-scan-table-card] [data-footer-pagination] [data-scan-pagination]')).not.toBeNull();
  });

  it('counts the changes as a total once the node has no further page', async() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(
      JSON.stringify({ ...stateMock.baseResponse, next_page_params: null }),
      { headers: { 'Content-Type': 'application/json' } },
    );

    const { container } = render(<TxState txQuery={ buildTxQuery(txMock.base) }/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent)
        .toBe('A total of 6 state changes found');
    });

    expect(container.querySelector('[data-scan-table-card] [data-footer-pagination]')).toBeNull();
  });

  it('keeps the card and says so when the transaction changed no state', async() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(
      JSON.stringify({ items: [], next_page_params: null }),
      { headers: { 'Content-Type': 'application/json' } },
    );

    const { container } = render(<TxState txQuery={ buildTxQuery(txMock.base) }/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-body]')?.textContent)
        .toBe('There are no state changes for this transaction.');
    });
  });

  it('waits for a pending transaction instead of asking for its state changes', () => {
    const { container } = render(<TxState txQuery={ buildTxQuery(txMock.pending) }/>);

    expect(container.textContent).toContain('This transaction is pending confirmation.');
    expect(container.querySelector('[data-scan-table-card]')).toBeNull();
  });

  it('reports a failed transaction request inside the card body', () => {
    const { container } = render(<TxState txQuery={ buildTxQuery(undefined, { isError: true }) }/>);

    expect(container.querySelector('[data-scan-table-card] [data-body]')?.textContent)
      .toContain('Something went wrong');
  });
});
