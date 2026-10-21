// @vitest-environment jsdom

import React from 'react';

import * as tokenTransferMock from 'mocks/tokens/tokenTransfer';
import { TableBody, TableRoot } from 'toolkit/chakra/table';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TokenTransferTableItem from './TokenTransferTableItem';

Object.defineProperty(document, 'fonts', {
  configurable: true,
  value: { load: () => Promise.resolve([ {} ]) },
});

const renderRow = (item: typeof tokenTransferMock.erc20) => render(
  <TableRoot variant="scan">
    <TableBody>
      <TokenTransferTableItem { ...item }/>
    </TableBody>
  </TableRoot>,
);

describe('TokenTransferTableItem', () => {
  beforeEach(() => {
    routerState.pathname = '/token/[hash]';
    routerState.query = { hash: tokenTransferMock.erc20.token?.address_hash };
    fetchMock.resetMocks();
  });

  it('shows the transaction method on the shared method chip', () => {
    const { container } = renderRow(tokenTransferMock.erc20);

    const chip = container.querySelector('[data-scan-method]');

    expect(chip?.getAttribute('data-scan-method')).toBe('updateSmartAsset');
    expect(chip?.textContent).toBe('updateSmartAsset');
  });

  it('leaves the method cell empty when the transfer carries no method', () => {
    const { container } = renderRow({ ...tokenTransferMock.erc20, method: undefined });

    expect(container.querySelector('[data-scan-method]')).toBeNull();
  });

  it('links the transaction hash and keeps the timestamp under it', () => {
    const { container } = renderRow(tokenTransferMock.erc20);

    const row = container.querySelector('[data-token-transfer-row]') as HTMLElement;

    expect(row.querySelector('[data-entity-kind="tx"]')).not.toBeNull();
  });

  it('carries both parties of the transfer', () => {
    const { container } = renderRow(tokenTransferMock.erc20);

    const row = container.querySelector('[data-token-transfer-row]') as HTMLElement;

    expect(row.textContent).toContain('ArianeeStore');
    expect(row.querySelectorAll('[data-entity-kind="address"]').length).toBeGreaterThanOrEqual(2);
  });

  it('links the token instance of a non-fungible transfer', () => {
    const { container } = renderRow(tokenTransferMock.erc721);

    const row = container.querySelector('[data-token-transfer-row]') as HTMLElement;
    const instanceLink = row.querySelector('a[href*="/instance/875879856"]');

    expect(instanceLink).not.toBeNull();
  });
});
