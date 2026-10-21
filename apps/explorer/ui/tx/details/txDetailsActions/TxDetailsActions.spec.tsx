// @vitest-environment jsdom

import React from 'react';

import type { TxAction } from 'types/api/txAction';

import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TxDetailsActions from './TxDetailsActions';

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

describe('TxDetailsActions', () => {
  beforeEach(() => {
    routerState.pathname = '/tx/[hash]';
    routerState.query = { hash: '0x62d597ebcf3e8d60096dd0363bc2f0f5e2df27ba1dacd696c51aa7c9409f3196' };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('renders the decoded actions as their own card above the details', () => {
    const { container } = render(<TxDetailsActions actions={ [ swap ] } isTxDataLoading={ false }/>);

    const card = container.querySelector('[data-tx-action-card]');

    expect(card).not.toBeNull();
    expect(card?.getAttribute('id')).toBe('tx-actions');
    expect(card?.querySelector('[data-label]')?.textContent).toBe('Transaction action');
  });

  it('lists every decoded action inside the card body', () => {
    const { container } = render(<TxDetailsActions actions={ [ swap, mint ] } isTxDataLoading={ false }/>);

    const body = container.querySelector('[data-tx-action-card] [data-content]');

    expect(body?.children).toHaveLength(2);
    expect(body?.textContent).toContain('PAX');
    expect(body?.textContent).toContain('Paxeer Pass');
  });

  it('renders nothing when the transaction decodes to no action', () => {
    const { container } = render(<TxDetailsActions actions={ [] } isTxDataLoading={ false }/>);

    expect(container.querySelector('[data-tx-action-card]')).toBeNull();
    expect(container.textContent).not.toContain('Transaction action');
  });

  it('renders nothing when the transaction carries no actions at all', () => {
    const { container } = render(<TxDetailsActions isTxDataLoading={ false }/>);

    expect(container.querySelector('[data-tx-action-card]')).toBeNull();
    expect(container.textContent).not.toContain('Transaction action');
  });
});
