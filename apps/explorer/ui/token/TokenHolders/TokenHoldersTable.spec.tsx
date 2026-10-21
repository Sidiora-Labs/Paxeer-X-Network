// @vitest-environment jsdom

import React from 'react';

import { tokenHoldersERC1155, tokenHoldersERC20 } from 'mocks/tokens/tokenHolders';
import { tokenInfo, tokenInfoERC1155a } from 'mocks/tokens/tokenInfo';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TokenHoldersTable from './TokenHoldersTable';

Object.defineProperty(document, 'fonts', {
  configurable: true,
  value: { load: () => Promise.resolve([ {} ]) },
});

const headers = (container: HTMLElement) =>
  Array.from(container.querySelectorAll('th')).map((header) => header.textContent ?? '');

describe('TokenHoldersTable', () => {
  beforeEach(() => {
    routerState.pathname = '/token/[hash]';
    routerState.query = { hash: tokenInfo.address_hash, tab: 'holders' };
    fetchMock.resetMocks();
  });

  it('carries the scan table look', () => {
    const { container } = render(<TokenHoldersTable data={ tokenHoldersERC20.items } token={ tokenInfo } top={ 0 }/>);

    expect(container.querySelector('[data-token-holders-table]')?.tagName).toBe('TABLE');
  });

  it('heads a fungible holders table with the holder, the quantity and the percentage', () => {
    const { container } = render(<TokenHoldersTable data={ tokenHoldersERC20.items } token={ tokenInfo } top={ 0 }/>);

    expect(headers(container)).toEqual([ 'Holder', 'Quantity', 'Percentage' ]);
  });

  it('adds the token id column for a multi token', () => {
    const { container } = render(
      <TokenHoldersTable data={ tokenHoldersERC1155.items } token={ tokenInfoERC1155a } top={ 0 }/>,
    );

    expect(headers(container)).toContain('ID#');
  });

  it('renders one row per holder', () => {
    const { container } = render(<TokenHoldersTable data={ tokenHoldersERC20.items } token={ tokenInfo } top={ 0 }/>);

    expect(container.querySelectorAll('[data-token-holders-row]')).toHaveLength(2);
    expect(container.querySelectorAll('[data-token-holders-row] [data-entity-kind="address"]')).toHaveLength(2);
  });
});
