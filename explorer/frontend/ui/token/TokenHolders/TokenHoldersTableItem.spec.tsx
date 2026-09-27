// @vitest-environment jsdom

import React from 'react';

import { tokenHoldersERC1155, tokenHoldersERC20 } from 'mocks/tokens/tokenHolders';
import { tokenInfo, tokenInfoERC1155a } from 'mocks/tokens/tokenInfo';
import { TableBody, TableRoot } from 'toolkit/chakra/table';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TokenHoldersTableItem from './TokenHoldersTableItem';

Object.defineProperty(document, 'fonts', {
  configurable: true,
  value: { load: () => Promise.resolve([ {} ]) },
});

const renderRow = (holder: typeof tokenHoldersERC20.items[number], token = tokenInfo) => render(
  <TableRoot variant="scan">
    <TableBody>
      <TokenHoldersTableItem holder={ holder } token={ token }/>
    </TableBody>
  </TableRoot>,
);

describe('TokenHoldersTableItem', () => {
  beforeEach(() => {
    routerState.pathname = '/token/[hash]';
    routerState.query = { hash: tokenInfo.address_hash, tab: 'holders' };
    fetchMock.resetMocks();
  });

  it('marks the row so the scan table can style it', () => {
    const { container } = renderRow(tokenHoldersERC20.items[0]);

    expect(container.querySelector('[data-token-holders-row]')).not.toBeNull();
  });

  it('names the holder through the shared address entity', () => {
    const { container } = renderRow(tokenHoldersERC20.items[0]);

    const row = container.querySelector('[data-token-holders-row]') as HTMLElement;

    expect(row.querySelector('[data-entity-kind="address"]')).not.toBeNull();
    expect(row.textContent).toContain('ArianeeStore');
  });

  it('shows the held quantity', () => {
    const { container } = renderRow(tokenHoldersERC20.items[0]);

    expect(container.querySelector('[data-token-holders-row]')?.textContent).toContain('107.01');
  });

  it('carries the token id cell for a multi token holder', () => {
    const { container } = renderRow(tokenHoldersERC1155.items[0], tokenInfoERC1155a);

    expect(container.querySelector('[data-token-holders-row]')?.textContent).toContain('12345');
  });
});
