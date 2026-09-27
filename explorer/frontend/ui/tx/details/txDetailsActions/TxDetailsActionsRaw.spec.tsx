// @vitest-environment jsdom

import React from 'react';

import type { TxAction } from 'types/api/txAction';

import { base } from 'mocks/txs/tx';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TxDetailsActionsRaw from './TxDetailsActionsRaw';

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

const mint: TxAction = {
  protocol: 'uniswap_v3',
  type: 'mint_nft',
  data: {
    name: 'Paxeer Pass',
    symbol: 'PXP',
    address: '0x21f7b20a555199fa73A238B1a91FD0f549068fEe',
    to: '0x471EcE3750Da237f93B8E339c536989b8978a438',
    ids: [ '1', '2' ],
  },
};

describe('TxDetailsActionsRaw', () => {
  beforeEach(() => {
    routerState.pathname = '/tx/[hash]';
    routerState.query = { hash: base.hash };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('gives the decoded actions their own labelled card', () => {
    const { container } = render(<TxDetailsActionsRaw actions={ [ swap ] } isLoading={ false }/>);

    const card = container.querySelector('[data-tx-action-card]');

    expect(card).not.toBeNull();
    expect(card?.getAttribute('id')).toBe('tx-actions');
    expect(card?.querySelector('[data-label]')?.textContent).toBe('Transaction action');
  });

  it('lists one entry per decoded action in the card body', () => {
    const { container } = render(<TxDetailsActionsRaw actions={ [ swap, mint ] } isLoading={ false }/>);

    const body = container.querySelector('[data-tx-action-card] [data-content]');

    expect(body?.children).toHaveLength(2);
    expect(body?.textContent).toContain('PAX');
    expect(body?.textContent).toContain('Paxeer Pass');
  });

  it('keeps the card and its label while the transaction is still loading', () => {
    const { container } = render(<TxDetailsActionsRaw actions={ [ swap ] } isLoading={ true }/>);

    const card = container.querySelector('[data-tx-action-card]');

    expect(card?.querySelector('[data-label]')?.textContent).toBe('Transaction action');
    expect(card?.querySelector('[data-content]')?.children).toHaveLength(1);
  });
});
