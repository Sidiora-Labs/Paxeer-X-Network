// @vitest-environment jsdom

import React from 'react';

import type { Transaction } from 'types/api/transaction';
import type { TxAction } from 'types/api/txAction';

import { base } from 'mocks/txs/tx';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TxDetails from './TxDetails';
import type { TxQuery } from './useTxQuery';

const swap: TxAction = {
  protocol: 'uniswap_v3',
  type: 'swap',
  data: {
    amount0: '1',
    symbol0: 'PAX',
    address0: '0x21f7b20a555199fa73A238B1a91FD0f549068fEe',
    amount1: '3.114',
    symbol1: 'SID',
    address1: '0x471EcE3750Da237f93B8E339c536989b8978a438',
  },
};

const buildTxQuery = (data: Transaction | undefined, overrides: Partial<TxQuery> = {}): TxQuery => ({
  data,
  isError: false,
  isPending: false,
  isPlaceholderData: false,
  isFetchedAfterMount: true,
  errorUpdateCount: 0,
  socketStatus: undefined,
  setRefetchEnabled: () => undefined,
  ...overrides,
} as unknown as TxQuery);

describe('TxDetails', () => {
  beforeEach(() => {
    routerState.pathname = '/tx/[hash]';
    routerState.query = { hash: base.hash };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('stacks the decoded action card above the detail card', () => {
    const { container } = render(<TxDetails txQuery={ buildTxQuery({ ...base, actions: [ swap ] }) }/>);

    const cards = Array.from(container.querySelectorAll('[data-tx-action-card], [data-tx-info]'));

    expect(cards.map((card) => card.hasAttribute('data-tx-action-card'))).toEqual([ true, false ]);
  });

  it('names the action card and anchors it for the view all link', () => {
    const { container } = render(<TxDetails txQuery={ buildTxQuery({ ...base, actions: [ swap ] }) }/>);

    const card = container.querySelector('[data-tx-action-card]');

    expect(card?.getAttribute('id')).toBe('tx-actions');
    expect(card?.querySelector('[data-label]')?.textContent).toBe('Transaction action');
  });

  it('leaves out the action card when the transaction decodes to nothing', () => {
    const { container } = render(<TxDetails txQuery={ buildTxQuery({ ...base, actions: [] }) }/>);

    expect(container.querySelector('[data-tx-action-card]')).toBeNull();
    expect(container.querySelector('[data-tx-info]')).not.toBeNull();
  });

  it('reports a failed request instead of the cards', () => {
    const { container } = render(<TxDetails txQuery={ buildTxQuery(undefined, { isError: true }) }/>);

    expect(container.querySelector('[data-tx-details]')).toBeNull();
    expect(container.textContent).toContain('Something went wrong');
  });
});
