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

// The host runs the whole suite at once, and these trees mount real entities, so the first render of
// each of them reaches well past the default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

import TxsStats from './TxsStats';

const json = (payload: unknown) => JSON.stringify(payload);

// A responsive style prop reaches the document as one rule per breakpoint, each of them inserted
// into a style element of its own, and jsdom performs no layout, so the widths a declaration belongs
// to are read from the rules that name the element's own class.
const ruleTexts = () => {
  const inline = Array.from(document.querySelectorAll('style')).map((node) => node.textContent ?? '');
  const parsed = Array.from(document.styleSheets).flatMap((sheet) => {
    try {
      return Array.from(sheet.cssRules).map((rule) => rule.cssText);
    } catch {
      return [];
    }
  });

  return [ ...inline, ...parsed ].filter(Boolean);
};

const declarationsOf = (element: Element, property: string) => ruleTexts()
  .filter((text) => Array.from(element.classList).some((name) => new RegExp(`\\.${ name }(?![\\w-])`).test(text)))
  .map((text) => ({ text, value: new RegExp(`(?:^|[;{\\s])${ property }\\s*:\\s*([^;}]+)`).exec(text)?.[1]?.trim() }))
  .filter((rule): rule is { text: string; value: string } => rule.value !== undefined);

// Each rule is inserted on its own, so a declaration belongs to the wide layout when the rule that
// carries it is a breakpoint rule and not the base rule, which declares its own minimum width.
const isWide = (text: string) => /^\s*@media[^{]*\(min-width/.test(text);

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

  it('lets every stat card shrink inside its own track', async() => {
    const { container } = render(<TxsStats/>);

    await waitFor(() => {
      expect(container.querySelectorAll('[data-scan-stat]').length).toBeGreaterThan(0);
    });

    const row = container.querySelector('[data-scan-stat-row]') as HTMLElement;
    const tracks = declarationsOf(row, 'grid-template-columns');

    expect(tracks.length).toBeGreaterThan(0);
    expect(tracks.every((rule) => /minmax\(\s*0/.test(rule.value))).toBe(true);
    expect(tracks.some((rule) => isWide(rule.text) && rule.value.includes('repeat('))).toBe(true);
  });
});
