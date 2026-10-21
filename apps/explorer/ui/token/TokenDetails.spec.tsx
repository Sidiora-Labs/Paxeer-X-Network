// @vitest-environment jsdom

import type { UseQueryResult } from '@tanstack/react-query';
import React from 'react';

import type { TokenInfo } from 'types/api/token';

import type { ResourceError } from 'lib/api/resources';
import { tokenCounters, tokenInfoERC20b, tokenInfoERC721a } from 'mocks/tokens/tokenInfo';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TokenDetails from './TokenDetails';

Object.defineProperty(document, 'fonts', {
  configurable: true,
  value: { load: () => Promise.resolve([ {} ]) },
});

const HASH = tokenInfoERC20b.address_hash;

const query = (data: TokenInfo) => ({
  data,
  isPlaceholderData: false,
  isPending: false,
  isError: false,
}) as unknown as UseQueryResult<TokenInfo, ResourceError<unknown>>;

const cardTitles = (container: HTMLElement) =>
  Array.from(container.querySelectorAll('[data-token-card]')).map((card) => card.getAttribute('data-token-card'));

const cardRows = (container: HTMLElement, title: string) => {
  const card = container.querySelector(`[data-token-card="${ title }"]`) as HTMLElement;

  return Array.from(card.querySelectorAll('[data-scan-key]')).map((key) => key.textContent ?? '');
};

describe('TokenDetails', () => {
  beforeEach(() => {
    routerState.pathname = '/token/[hash]';
    routerState.query = { hash: HASH };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify(tokenCounters), { headers: { 'Content-Type': 'application/json' } });
  });

  it('splits the details into an overview, a market and an other-info card', () => {
    const { container } = render(<TokenDetails tokenQuery={ query(tokenInfoERC20b) }/>);

    expect(cardTitles(container)).toEqual([ 'Overview', 'Market', 'Other info' ]);
  });

  it('carries the supply, the holders and the transfers in the overview card', () => {
    const { container } = render(<TokenDetails tokenQuery={ query(tokenInfoERC20b) }/>);

    expect(cardRows(container, 'Overview')).toEqual([ 'Max total supply', 'Holders', 'Transfers' ]);
  });

  it('links the holders and the transfers to their counts once the counters answered', async() => {
    const { container } = render(<TokenDetails tokenQuery={ query(tokenInfoERC20b) }/>);

    await waitFor(() => {
      const overview = container.querySelector('[data-token-card="Overview"]') as HTMLElement;

      expect(overview.textContent).toContain('8,838,883');
      expect(overview.textContent).toContain('88,282,281');
    });
  });

  it('carries the price and both market capitalisations in the market card', () => {
    const { container } = render(<TokenDetails tokenQuery={ query(tokenInfoERC20b) }/>);

    const market = container.querySelector('[data-token-card="Market"]') as HTMLElement;

    expect(cardRows(container, 'Market')).toEqual([ 'Price', 'Onchain market cap', 'Circulating supply market cap' ]);
    expect(market.textContent).toContain('$0.9820');
    expect(market.querySelector('[data-onchain-market-cap]')?.textContent).toBe('$883,800,000,000,000,000,000.00');
    expect(market.textContent).toContain('$115,060,192.36105014');
  });

  it('names the token contract with its decimals in the other-info card', () => {
    const { container } = render(<TokenDetails tokenQuery={ query(tokenInfoERC20b) }/>);

    expect(cardRows(container, 'Other info')[0]).toBe('Token contract (with 6 decimals)');
    expect(container.querySelector('[data-token-contract] [data-entity-kind="address"]')).not.toBeNull();
  });

  it('drops the market card for a token with neither a rate nor a market cap', () => {
    const { container } = render(<TokenDetails tokenQuery={ query(tokenInfoERC721a) }/>);

    expect(cardTitles(container)).toEqual([ 'Overview', 'Other info' ]);
  });

  it('keeps the contract row without a decimals note when the token has no decimals', () => {
    const { container } = render(<TokenDetails tokenQuery={ query(tokenInfoERC721a) }/>);

    expect(cardRows(container, 'Other info')[0]).toBe('Token contract');
  });
});
