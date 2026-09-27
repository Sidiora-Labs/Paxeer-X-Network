// @vitest-environment jsdom

import React from 'react';

import type { Transaction } from 'types/api/transaction';

import { base } from 'mocks/txs/tx';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TxSubHeading, { TxApiEntry } from './TxSubHeading';
import type { TxQuery } from './useTxQuery';

const buildTxQuery = (data: Transaction, overrides: Partial<TxQuery> = {}): TxQuery => ({
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

describe('TxSubHeading', () => {
  beforeEach(() => {
    routerState.pathname = '/tx/[hash]';
    routerState.query = { hash: base.hash };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('titles the page and keeps the interpretation in the second row', () => {
    const { container } = render(<TxSubHeading hash={ base.hash } hasTag={ false } txQuery={ buildTxQuery(base) }/>);

    const heading = container.querySelector('h1');

    expect(heading?.textContent).toBe('Transaction details');
    expect(container.querySelector('[data-tx-sub-heading]')).not.toBeNull();
  });

  it('offers the previous and next controls beside the title', () => {
    const { container } = render(<TxSubHeading hash={ base.hash } hasTag={ false } txQuery={ buildTxQuery(base) }/>);

    const prevNext = container.querySelector('[data-prev-next]');

    expect(prevNext).not.toBeNull();
    expect(prevNext?.querySelectorAll('button')).toHaveLength(2);
  });

  it('disables both controls while the transaction is still a placeholder', () => {
    const { container } = render(
      <TxSubHeading hash={ base.hash } hasTag={ false } txQuery={ buildTxQuery(base, { isPlaceholderData: true }) }/>,
    );

    expect(container.querySelector('[data-prev-next]')).toBeNull();
  });

  it('disables the previous control on the first transaction of the block', () => {
    const { container } = render(
      <TxSubHeading hash={ base.hash } hasTag={ false } txQuery={ buildTxQuery({ ...base, position: 0 }) }/>,
    );

    const buttons = Array.from(container.querySelectorAll('[data-prev-next] button'));

    expect(buttons[0].hasAttribute('disabled')).toBe(true);
  });

  it('places the tags given to it after the title', () => {
    const { container } = render(
      <TxSubHeading
        hash={ base.hash }
        hasTag={ false }
        txQuery={ buildTxQuery(base) }
        titleContentAfter={ <span data-testid="tx-tags">Coin bridge</span> }
      />,
    );

    expect(container.querySelector('[data-testid="tx-tags"]')?.textContent).toBe('Coin bridge');
  });

  it('links the API documentation from the entry point', () => {
    const { container } = render(<TxApiEntry/>);

    const link = container.querySelector('[data-tx-api-entry]');

    expect(link?.getAttribute('href')).toBe('/api-docs');
    expect(link?.textContent).toContain('API');
  });
});
