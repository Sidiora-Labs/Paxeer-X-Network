// @vitest-environment jsdom

import React from 'react';

import * as paxeerXMock from 'mocks/paxeerX/unifiedAccount';
import { describe, expect, it } from 'vitest';
import { screen } from 'vitest/lib';

import ActivityList from './ActivityList';
import { render } from './testWrapper';

const pagination = {
  page: paxeerXMock.unifiedAccount.page_number,
  total: paxeerXMock.unifiedAccount.activity_total,
  hasNextPage: Boolean(paxeerXMock.unifiedAccount.next_page_params),
  canGoBackwards: false,
  onNextPageClick: () => window.history.forward(),
  onPrevPageClick: () => window.history.back(),
  resetPage: () => window.history.go(0),
};

describe('ActivityList', () => {
  it('shows an empty message when there is no activity', () => {
    render(<ActivityList { ...pagination } items={ [] } total={ 0 }/>);

    expect(screen.getByText('There is no Paxeer X activity for this account yet.')).toBeTruthy();
  });

  it('renders one row per activity entry', () => {
    const { container } = render(<ActivityList { ...pagination } items={ paxeerXMock.unifiedAccount.activity }/>);

    expect(container.querySelectorAll('[data-activity]')).toHaveLength(3);

    // the first cell of a row names the kind and, under it, the side of the network it happened on
    const kinds = Array.from(container.querySelectorAll('[data-activity] td:first-child')).map((cell) => cell.textContent);

    expect(kinds).toEqual([ 'Custody depositKernel', 'Token transferChain', 'TransactionChain' ]);
  });

  it('humanizes a kind outside the known vocabulary', () => {
    const item = { ...paxeerXMock.unifiedAccount.activity[0], kind: 'replay_receipt' };

    render(<ActivityList { ...pagination } items={ [ item ] } total={ 1 }/>);

    expect(screen.getByText('Replay receipt')).toBeTruthy();
  });

  it('puts every row on the status ladder', () => {
    const { container } = render(<ActivityList { ...pagination } items={ paxeerXMock.unifiedAccount.activity }/>);

    expect(container.querySelectorAll('[data-rung]')).toHaveLength(3);
    expect(container.querySelector('[data-rung="final"]')).toBeTruthy();
    expect(container.querySelector('[data-rung="sealed"]')).toBeTruthy();
    expect(container.querySelector('[data-rung="instant"]')).toBeTruthy();
  });

  it('scales amounts by the asset decimals and marks the chain and kernel sides', () => {
    render(<ActivityList { ...pagination } items={ paxeerXMock.unifiedAccount.activity }/>);

    expect(screen.getByText('1.5 HPX')).toBeTruthy();
    expect(screen.getByText('2.5 USDX')).toBeTruthy();
    expect(screen.getAllByText('Chain')).toHaveLength(2);
    expect(screen.getByText('Kernel')).toBeTruthy();
  });

  it('shows a custody amount unscaled and labelled by its kernel asset id', () => {
    render(<ActivityList { ...pagination } items={ paxeerXMock.unifiedAccount.activity }/>);

    expect(screen.getByText(`1,200 ${ paxeerXMock.custodyAsset.denom }`)).toBeTruthy();
  });
  it('heads the scan table card with the entry count and its note', () => {
    const { container } = render(<ActivityList { ...pagination } items={ paxeerXMock.unifiedAccount.activity }/>);

    const card = container.querySelector('[data-scan-table-card]');

    expect(card?.querySelector('[data-title]')?.textContent).toBe('A total of 3 activity entries found');
    expect(card?.querySelector('[data-note]')?.textContent).toBe('Chain-side and kernel-side entries in one feed, newest first');
  });

  it('names the action of every row on a method chip', () => {
    const { container } = render(<ActivityList { ...pagination } items={ paxeerXMock.unifiedAccount.activity }/>);

    const methods = Array.from(container.querySelectorAll('[data-activity] [data-scan-method]'))
      .map((chip) => chip.getAttribute('data-scan-method'));

    expect(methods).toEqual([ 'Custody deposit', 'Token transfer', 'Transaction' ]);
  });

});
