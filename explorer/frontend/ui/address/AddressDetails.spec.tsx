// @vitest-environment jsdom

import React from 'react';

import * as addressMock from 'mocks/address/address';
import * as countersMock from 'mocks/address/counters';
import * as paxeerXMock from 'mocks/paxeerX/unifiedAccount';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { waitFor } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_PAXEER_X_ENABLED: 'true',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// The address lists render fifty placeholder rows of real table items, which jsdom lays out well past the
// default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

import AddressDetails from './AddressDetails';
import useAddressCountersQuery from './utils/useAddressCountersQuery';
import useAddressQuery from './utils/useAddressQuery';

const HASH = addressMock.hash;

const json = (payload: unknown) => ({
  body: JSON.stringify(payload),
  headers: { 'Content-Type': 'application/json' },
});

const answer = (address: unknown, withUnifiedAccount: boolean) => {
  fetchMock.mockResponse((request) => {
    const { pathname } = new URL(request.url);

    if (pathname === `/api/v2/addresses/${ HASH }`) {
      return Promise.resolve(json(address));
    }

    if (pathname === `/api/v2/addresses/${ HASH }/counters`) {
      return Promise.resolve(json(countersMock.forAddress));
    }

    if (pathname === `/api/v2/addresses/${ HASH }/unified`) {
      return withUnifiedAccount ?
        Promise.resolve(json(paxeerXMock.unifiedAccount)) :
        Promise.resolve({ ...json({ message: 'Not found' }), status: 404 });
    }

    return Promise.resolve(json({ items: [], next_page_params: null }));
  });
};

// The page owns both queries and hands them down, so the spec drives the real hooks the page uses.
const Harness = () => {
  const addressQuery = useAddressQuery({ hash: HASH });
  const countersQuery = useAddressCountersQuery({ hash: HASH, isLoading: addressQuery.isPlaceholderData });

  return <AddressDetails addressQuery={ addressQuery } countersQuery={ countersQuery } isLoading={ addressQuery.isPlaceholderData }/>;
};

const cardTitles = (container: HTMLElement) =>
  Array.from(container.querySelectorAll('[data-address-card] [data-card-title]')).map((item) => item.textContent);

describe('AddressDetails', () => {
  beforeEach(() => {
    routerState.pathname = '/address/[hash]';
    routerState.query = { hash: HASH };
    fetchMock.resetMocks();
  });

  it('lays the details out as three cards in one grid', async() => {
    answer(addressMock.validator, false);

    const { container } = render(<Harness/>);

    await waitFor(() => {
      expect(cardTitles(container)).toEqual([ 'Overview', 'More info', 'Other info' ]);
    });

    expect(container.querySelectorAll('[data-address-details]')).toHaveLength(1);
  });

  it('heads the third card with the contract information when the address has no kernel account', async() => {
    answer(addressMock.contract, false);

    const { container } = render(<Harness/>);

    await waitFor(() => {
      expect(cardTitles(container)).toEqual([ 'Overview', 'More info', 'Contract info' ]);
    });

    expect(container.querySelector('[data-kernel-account-link]')).toBeNull();
  });

  it('counts the transactions and the transfers in the more-info card', async() => {
    answer(addressMock.eoa, false);

    const { container } = render(<Harness/>);

    await waitFor(() => {
      expect(cardTitles(container)[1]).toBe('More info');
    });

    const moreInfo = container.querySelector('[data-address-card="More info"]') as HTMLElement;

    await waitFor(() => {
      expect(moreInfo.textContent).toContain('Transactions');
    });

    expect(moreInfo.textContent).toContain('Funded by');
  });

  it('names the creator instead of the funding source for a contract', async() => {
    answer(addressMock.contract, false);

    const { container } = render(<Harness/>);

    await waitFor(() => {
      const moreInfo = container.querySelector('[data-address-card="More info"]') as HTMLElement;
      expect(moreInfo.textContent).toContain('Creator');
    });
  });

  it('replaces the third card with the kernel account when the address has one', async() => {
    answer(addressMock.validator, true);

    const { container } = render(<Harness/>);

    await waitFor(() => {
      expect(cardTitles(container)).toEqual([ 'Overview', 'More info', 'Paxeer X account' ]);
    });

    const kernelCard = container.querySelector('[data-address-card="Paxeer X account"]') as HTMLElement;

    expect(Array.from(kernelCard.querySelectorAll('[data-identity]')).map((item) => item.getAttribute('data-identity')))
      .toEqual([ 'evm', 'pax', 'did', 'kernel_account' ]);
    expect(kernelCard.querySelector('[data-kernel-account-link]')?.getAttribute('href'))
      .toBe(`/paxeer-x/account/${ HASH }`);
  });

  it('offers the token-holdings selector only for an address that holds tokens', async() => {
    answer(addressMock.eoa, false);

    const { container } = render(<Harness/>);

    await waitFor(() => {
      const overview = container.querySelector('[data-address-card="Overview"]') as HTMLElement;
      expect(overview.textContent).toContain('Token holdings');
    });
  });
});
