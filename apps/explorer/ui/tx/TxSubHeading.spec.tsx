// @vitest-environment jsdom

import React from 'react';

import type { Transaction } from 'types/api/transaction';

import { base } from 'mocks/txs/tx';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// The host runs the whole suite at once, and these trees mount real entities, so the first render of
// each of them reaches well past the default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

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

const LEFT_INSET = 'margin-(?:left|inline-start)';

// The scale resolves a zero inset either to the plain length or to the token of the same name.
const isSpace = (value: string) => !/^0[a-z%]*$/.test(value) && !/spacing-0\b/.test(value);

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

  it('keeps the inset before the two controls for the wide layout only', () => {
    const { container } = render(<TxSubHeading hash={ base.hash } hasTag={ false } txQuery={ buildTxQuery(base) }/>);

    const prevNext = container.querySelector('[data-prev-next]') as HTMLElement;
    const insets = declarationsOf(prevNext, LEFT_INSET);
    const spaced = insets.filter((rule) => isSpace(rule.value));

    expect(insets.length).toBeGreaterThan(0);
    expect(spaced.length).toBeGreaterThan(0);
    expect(spaced.every((rule) => isWide(rule.text))).toBe(true);
  });
});
