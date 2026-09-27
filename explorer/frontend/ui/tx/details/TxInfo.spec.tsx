// @vitest-environment jsdom

import { act } from '@testing-library/react';
import React from 'react';

import { base } from 'mocks/txs/tx';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TxInfo from './TxInfo';

const labels = (container: HTMLElement, scope: string) =>
  Array.from(container.querySelectorAll(`${ scope } [data-detailed-info-label]`)).map((item) => item.textContent);

describe('TxInfo', () => {
  beforeEach(() => {
    routerState.pathname = '/tx/[hash]';
    routerState.query = { hash: base.hash };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('puts the detail card before the more details expander', () => {
    const { container } = render(<TxInfo data={ base } isLoading={ false }/>);

    const blocks = Array.from(container.querySelectorAll('[data-tx-info] > [data-detailed-info], [data-tx-info] > [data-scan-expander]'));

    expect(blocks).toHaveLength(2);
    expect(blocks[0].hasAttribute('data-detailed-info')).toBe(true);
    expect(blocks[1].hasAttribute('data-scan-expander')).toBe(true);
  });

  it('orders the overview rows from the hash down to the gas price', () => {
    const { container } = render(<TxInfo data={ base } isLoading={ false }/>);

    const expected = [ 'Transaction hash', 'Status and method', 'Block', 'Timestamp', 'From', 'To', 'Value', 'Transaction fee', 'Gas price' ];

    expect(labels(container, '[data-tx-info] > [data-detailed-info]').filter((label) => expected.includes(label ?? ''))).toEqual(expected);
  });

  it('carries the hash with its copy control and the method beside the status', () => {
    const { container } = render(<TxInfo data={ base } isLoading={ false }/>);

    const rows = container.querySelectorAll('[data-tx-info] > [data-detailed-info] [data-scan-value]');

    expect(rows[0].querySelector('[aria-label="copy"]')).not.toBeNull();
    expect(rows[1].textContent).toContain(base.method);
  });

  it('shows the confirmations as a chip on the block row', () => {
    const { container } = render(<TxInfo data={ base } isLoading={ false }/>);

    expect(container.querySelector('[data-block-confirmations]')?.textContent)
      .toBe(`${ base.confirmations } Block confirmations`);
  });

  it('offers the timestamp together with its zone control', () => {
    const { container } = render(<TxInfo data={ base } isLoading={ false }/>);

    const timestampRow = Array.from(container.querySelectorAll('[data-tx-info] > [data-detailed-info] [data-scan-value]'))[3];

    expect(timestampRow.querySelector('button')).not.toBeNull();
  });

  it('marks a verified recipient contract', () => {
    const verified = { ...base, to: { ...base.to!, is_contract: true, is_verified: true } };
    const { container } = render(<TxInfo data={ verified } isLoading={ false }/>);

    expect(container.querySelector('[data-verified-mark]')).not.toBeNull();
  });

  it('leaves the mark off an unverified recipient contract', () => {
    const unverified = { ...base, to: { ...base.to!, is_contract: true, is_verified: false } };
    const { container } = render(<TxInfo data={ unverified } isLoading={ false }/>);

    expect(container.querySelector('[data-verified-mark]')).toBeNull();
  });

  it('keeps the gas usage, nonce and raw input out of the overview until the expander is opened', () => {
    const { container } = render(<TxInfo data={ base } isLoading={ false }/>);

    const expander = container.querySelector('[data-scan-expander]') as HTMLElement;

    expect(expander.getAttribute('data-open')).toBe('false');
    expect(expander.querySelector('[data-content]')).toBeNull();

    act(() => {
      (expander.querySelector('[data-toggle]') as HTMLElement).click();
    });

    expect(expander.getAttribute('data-open')).toBe('true');
    expect(labels(container, '[data-scan-expander]')).toEqual(expect.arrayContaining([ 'Gas usage & limit by txn', 'Other', 'Raw input' ]));
  });
});
