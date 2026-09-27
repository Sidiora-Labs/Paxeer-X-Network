// @vitest-environment jsdom

import React from 'react';

import * as statsMock from 'mocks/stats/index';
import * as txsStatsMock from 'mocks/txs/stats';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { waitFor } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_STATS_API_HOST: '',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TxsStats from './TxsStats';

const json = (payload: unknown) => JSON.stringify(payload);

describe('TxsStats', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => {
      if (request.url.includes('/api/v2/transactions/stats')) {
        return { body: json(txsStatsMock.base), headers: { 'Content-Type': 'application/json' } };
      }

      if (request.url.includes('/api/v2/stats')) {
        return { body: json(statsMock.base), headers: { 'Content-Type': 'application/json' } };
      }

      return { body: json({}), headers: { 'Content-Type': 'application/json' } };
    });
  });

  it('opens the page with the four stat cards in the scan order', async() => {
    const { container } = render(<TxsStats/>);

    await waitFor(() => {
      expect(Array.from(container.querySelectorAll('[data-scan-stat]')).map((card) => card.getAttribute('data-scan-stat')))
        .toEqual([
          'Transactions (24h)',
          'Pending transactions (last 1h)',
          'Total transaction fee (24h)',
          'Avg. transaction fee (24h)',
        ]);
    });
  });

  it('reads the counts and the fees the transaction statistics carry', async() => {
    const { container } = render(<TxsStats/>);

    await waitFor(() => {
      const values = Array.from(container.querySelectorAll('[data-scan-stat]'))
        .map((card) => card.querySelector('[data-value]')?.textContent);

      expect(values[0]).toBe('992,890');
      expect(values[1]).toBe('4,200');
      expect(values[2]).toContain('22.18');
      expect(values[2]).toContain('ETH');
      expect(values[3]?.startsWith('$')).toBe(true);
    });
  });

  it('carries a delta on every card, derived from the statistics it reads', async() => {
    const { container } = render(<TxsStats/>);

    await waitFor(() => {
      const deltas = Array.from(container.querySelectorAll('[data-scan-stat]'))
        .map((card) => {
          const delta = card.querySelector('[data-delta]');

          return delta ? [ delta.getAttribute('data-delta'), delta.textContent ] : null;
        });

      expect(deltas).toEqual([
        [ 'up', '(1.21%)' ],
        [ 'down', '(0.42%)' ],
        [ 'down', '(7.42%)' ],
        [ 'down', '(7.42%)' ],
      ]);
    });
  });
});
