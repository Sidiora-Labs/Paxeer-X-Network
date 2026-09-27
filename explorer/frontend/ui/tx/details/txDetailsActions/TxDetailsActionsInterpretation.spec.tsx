// @vitest-environment jsdom

import { waitFor } from '@testing-library/react';
import React from 'react';

import type { TxInterpretationResponse, TxInterpretationSummary } from 'types/api/txInterpretation';

import { base } from 'mocks/txs/tx';
import { txInterpretation } from 'mocks/txs/txInterpretation';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TxDetailsActionsInterpretation from './TxDetailsActionsInterpretation';

vi.setConfig({ testTimeout: 60_000 });

const [ transfer ] = txInterpretation.data.summaries;

const approve: TxInterpretationSummary = {
  ...transfer,
  summary_template_variables: {
    ...transfer.summary_template_variables,
    action_type: { type: 'string', value: 'Approve' },
  },
};

const summaries = (items: Array<TxInterpretationSummary>): TxInterpretationResponse => ({ data: { summaries: items } });

const respondWith = (payload: TxInterpretationResponse) => {
  fetchMock.resetMocks();
  fetchMock.mockResponse(JSON.stringify(payload), { headers: { 'Content-Type': 'application/json' } });
};

describe('TxDetailsActionsInterpretation', () => {
  beforeEach(() => {
    routerState.pathname = '/tx/[hash]';
    routerState.query = { hash: base.hash };
    respondWith(summaries([ transfer, approve ]));
  });

  it('gives the interpreted action its own card above the details', async() => {
    const { container } = render(<TxDetailsActionsInterpretation hash={ base.hash } isTxDataLoading={ false }/>);

    await waitFor(() => {
      expect(container.querySelector('[data-tx-action-card] [data-content]')?.children).toHaveLength(2);
    });

    const card = container.querySelector('[data-tx-action-card]');

    expect(card?.getAttribute('id')).toBe('tx-actions');
    expect(card?.querySelector('[data-label]')?.textContent).toBe('Transaction action');
  });

  it('renders every summary the interpreter returns', async() => {
    const { container } = render(<TxDetailsActionsInterpretation hash={ base.hash } isTxDataLoading={ false }/>);

    await waitFor(() => {
      const body = container.querySelector('[data-tx-action-card] [data-content]');

      expect(body?.children).toHaveLength(2);
      expect(body?.textContent).toContain('Transfer');
      expect(body?.textContent).toContain('Approve');
    }, { timeout: 10_000 });
  });

  it('leaves the card out when the interpreter returns a single summary', async() => {
    respondWith(summaries([ transfer ]));

    const { container } = render(<TxDetailsActionsInterpretation hash={ base.hash } isTxDataLoading={ false }/>);

    await waitFor(() => {
      expect(container.querySelector('[data-tx-action-card]')).toBeNull();
    });
  });

  it('leaves the card out when the interpreter returns nothing to say', async() => {
    respondWith(summaries([]));

    const { container } = render(<TxDetailsActionsInterpretation hash={ base.hash } isTxDataLoading={ false }/>);

    await waitFor(() => {
      expect(container.querySelector('[data-tx-action-card]')).toBeNull();
    });
  });
});
