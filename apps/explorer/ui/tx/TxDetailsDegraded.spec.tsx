// @vitest-environment jsdom

import React from 'react';

import { base } from 'mocks/txs/tx';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TxDetailsDegraded from './TxDetailsDegraded';
import type { TxQuery } from './useTxQuery';

// The degraded page reads the transaction from the node, and the node query ships the RPC
// placeholders it renders until the node answers. The spec leaves the query unfetched, which is the
// state the page is in while the indexer is behind, and asserts what the page puts on the screen.
const buildTxQuery = (status?: number): TxQuery => ({
  data: undefined,
  error: status === undefined ? null : { status, payload: undefined },
  isError: status !== undefined,
  isPending: false,
  isPlaceholderData: false,
  isFetchedAfterMount: false,
  socketStatus: undefined,
  setRefetchEnabled: () => undefined,
} as unknown as TxQuery);

describe('TxDetailsDegraded', () => {
  beforeEach(() => {
    routerState.pathname = '/tx/[hash]';
    routerState.query = { hash: base.hash };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('renders the overview card from the node data', () => {
    const { container } = render(<TxDetailsDegraded hash={ base.hash } txQuery={ buildTxQuery(500) }/>);

    expect(container.querySelector('[data-tx-info]')).not.toBeNull();
  });

  it('puts the sync warning in the band above the overview card', () => {
    const { container } = render(<TxDetailsDegraded hash={ base.hash } txQuery={ buildTxQuery(500) }/>);

    const band = container.querySelector('[data-tx-info]')?.previousElementSibling;

    expect(band?.textContent).toContain('Data sync in progress');
  });

  it('keeps the testnet notice in the same band', () => {
    const { container } = render(<TxDetailsDegraded hash={ base.hash } txQuery={ buildTxQuery(500) }/>);

    const band = container.querySelector('[data-tx-info]')?.previousElementSibling;

    expect(band?.textContent).toContain('This is a testnet transaction only');
  });

  it('drops the sync warning when the indexer simply has no such transaction', () => {
    const { container } = render(<TxDetailsDegraded hash={ base.hash } txQuery={ buildTxQuery(404) }/>);

    const band = container.querySelector('[data-tx-info]')?.previousElementSibling;

    expect(band?.textContent).not.toContain('Data sync in progress');
    expect(container.querySelector('[data-tx-info]')).not.toBeNull();
  });
});
