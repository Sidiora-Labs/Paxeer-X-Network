// @vitest-environment jsdom

import { waitFor } from '@testing-library/react';
import React from 'react';

import * as txMock from 'mocks/txs/tx';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const replace = vi.hoisted(() => vi.fn(() => Promise.resolve(true)));

vi.mock('next/router', async() => {
  const { nextRouterModule } = await import('ui/shared/layout/testWrapper');
  const { useRouter } = nextRouterModule();

  return { useRouter: () => ({ ...useRouter(), replace }) };
});

import Transaction from './Transaction';

const respondWith = (body: unknown) => {
  fetchMock.resetMocks();
  fetchMock.mockResponse(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });
};

describe('TransactionPageContent for a price update', () => {
  beforeEach(() => {
    replace.mockClear();
  });

  it('sends a price update hash to its feed details page', async() => {
    routerState.pathname = '/tx/[hash]';
    routerState.query = { hash: txMock.priceUpdate.hash };
    respondWith(txMock.priceUpdate);

    render(<Transaction/>);

    await waitFor(() => expect(replace).toHaveBeenCalledWith({ pathname: '/feed/update/[hash]', query: { hash: txMock.priceUpdate.hash } }));
  });

  it('keeps an EVM transaction on the transaction page', async() => {
    routerState.pathname = '/tx/[hash]';
    routerState.query = { hash: txMock.base.hash };
    respondWith(txMock.base);

    const { container } = render(<Transaction/>);

    await waitFor(() => expect(container.querySelector('[data-tx-details]')).not.toBeNull());
    expect(replace).not.toHaveBeenCalled();
  });
});
