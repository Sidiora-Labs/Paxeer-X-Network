// @vitest-environment jsdom

import { waitFor } from '@testing-library/react';
import React from 'react';

import type { Log, LogsResponseTx } from 'types/api/log';

import * as addressMock from 'mocks/address/address';
import * as decodedInputMock from 'mocks/txs/decodedInputData';
import * as txMock from 'mocks/txs/tx';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TxLogs from './TxLogs';
import type { TxQuery } from './useTxQuery';

vi.setConfig({ testTimeout: 60_000 });

const transferLog: Log = {
  address: addressMock.withName,
  topics: [
    '0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef',
    '0x000000000000000000000000d789a607ceac2f0e14867de4eb15b15c9ffb5859',
    '0x0000000000000000000000007d20a8d54f955b4483a66ab335635ab66e151c51',
    null,
  ],
  data: '0x0000000000000000000000000000000000000000000000000000000000000001',
  index: 0,
  decoded: decodedInputMock.withIndexedFields,
  transaction_hash: txMock.base.hash,
  block_timestamp: txMock.base.timestamp,
};

const spendLog: Log = {
  ...transferLog,
  index: 1,
  decoded: decodedInputMock.withoutIndexedFields,
};

const response = (items: Array<Log>): LogsResponseTx => ({ items, next_page_params: null });

const rejectEveryLog = () => false;

const buildTxQuery = (data: typeof txMock.base | undefined, overrides: Partial<TxQuery> = {}): TxQuery => ({
  data,
  isError: false,
  isPending: false,
  isPlaceholderData: false,
  socketStatus: undefined,
  setRefetchEnabled: () => undefined,
  ...overrides,
} as unknown as TxQuery);

describe('TxLogs', () => {
  beforeEach(() => {
    routerState.pathname = '/tx/[hash]';
    routerState.query = { hash: txMock.base.hash };
    fetchMock.resetMocks();
    fetchMock.mockResponse(
      JSON.stringify(response([ transferLog, spendLog ])),
      { headers: { 'Content-Type': 'application/json' } },
    );
  });

  it('holds the logs in the shared table card under its own count line', async() => {
    const { container } = render(<TxLogs txQuery={ buildTxQuery(txMock.base) }/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent)
        .toBe('A total of 2 logs found');
    }, { timeout: 10_000 });

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe('Transaction receipt event logs');
  });

  it('counts a single log in the singular', async() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify(response([ transferLog ])), { headers: { 'Content-Type': 'application/json' } });

    const { container } = render(<TxLogs txQuery={ buildTxQuery(txMock.base) }/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent)
        .toBe('A total of 1 log found');
    });
  });

  it('renders every log it holds inside the card body', async() => {
    const { container } = render(<TxLogs txQuery={ buildTxQuery(txMock.base) }/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent)
        .toBe('A total of 2 logs found');
    });

    const body = container.querySelector('[data-scan-table-card] [data-body]');

    expect(body?.textContent).toContain('Transfer');
    expect(body?.textContent).toContain('CreditSpended');
  });

  it('says so when the filter leaves no log to show', async() => {
    const { container } = render(<TxLogs txQuery={ buildTxQuery(txMock.base) } logsFilter={ rejectEveryLog }/>);

    await waitFor(() => {
      const emptyText = Array.from(container.querySelectorAll('span'))
        .find((node) => node.textContent === 'There are no logs for this transaction.');

      expect(emptyText).toBeTruthy();
    }, { timeout: 10_000 });

    expect(container.querySelector('[data-scan-table-card]')).toBeNull();
  });

  it('waits for a pending transaction instead of asking for its logs', () => {
    const { container } = render(<TxLogs txQuery={ buildTxQuery(txMock.pending) }/>);

    expect(container.textContent).toContain('This transaction is pending confirmation.');
    expect(container.querySelector('[data-scan-table-card]')).toBeNull();
  });

  it('reports a failed transaction request instead of the card', () => {
    const { container } = render(<TxLogs txQuery={ buildTxQuery(undefined, { isError: true }) }/>);

    expect(container.textContent).toContain('Something went wrong');
    expect(container.querySelector('[data-scan-table-card]')).toBeNull();
  });
});
