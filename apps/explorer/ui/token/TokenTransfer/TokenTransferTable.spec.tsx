// @vitest-environment jsdom

import React from 'react';

import { tokenInfoERC1155a, tokenInfoERC20a, tokenInfoERC721a } from 'mocks/tokens/tokenInfo';
import * as tokenTransferMock from 'mocks/tokens/tokenTransfer';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TokenTransferTable from './TokenTransferTable';

Object.defineProperty(document, 'fonts', {
  configurable: true,
  value: { load: () => Promise.resolve([ {} ]) },
});

const headers = (container: HTMLElement) =>
  Array.from(container.querySelectorAll('th')).map((header) => header.textContent ?? '');

describe('TokenTransferTable', () => {
  beforeEach(() => {
    routerState.pathname = '/token/[hash]';
    routerState.query = { hash: tokenInfoERC20a.address_hash };
    fetchMock.resetMocks();
  });

  it('carries the scan table look', () => {
    const { container } = render(
      <TokenTransferTable data={ [ tokenTransferMock.erc20 ] } token={ tokenInfoERC20a } top={ 0 } showSocketInfo={ false }/>,
    );

    const table = container.querySelector('[data-token-transfer-table]') as HTMLElement;

    expect(table).not.toBeNull();
    expect(table.tagName).toBe('TABLE');
  });

  it('heads a fungible transfer table with the hash, the method, the parties and the value', () => {
    const { container } = render(
      <TokenTransferTable data={ [ tokenTransferMock.erc20 ] } token={ tokenInfoERC20a } top={ 0 } showSocketInfo={ false }/>,
    );

    const titles = headers(container);

    expect(titles[0]).toContain('Txn hash');
    expect(titles[1]).toBe('Method');
    expect(titles[2]).toBe('From/To');
    expect(titles[3]).toContain('HyFi');
    expect(titles).toHaveLength(4);
  });

  it('adds the token id column for a non-fungible token', () => {
    const { container } = render(
      <TokenTransferTable data={ [ tokenTransferMock.erc721 ] } token={ tokenInfoERC721a } top={ 0 } showSocketInfo={ false }/>,
    );

    expect(headers(container)).toContain('Token ID');
  });

  it('keeps both the token id and the value columns for a multi token', () => {
    const { container } = render(
      <TokenTransferTable data={ [ tokenTransferMock.erc1155A ] } token={ tokenInfoERC1155a } top={ 0 } showSocketInfo={ false }/>,
    );

    const titles = headers(container);

    expect(titles).toContain('Token ID');
    expect(titles[titles.length - 1]).toContain('Value');
  });

  it('renders one row per transfer', () => {
    const { container } = render(
      <TokenTransferTable
        data={ [ tokenTransferMock.erc20, tokenTransferMock.erc20 ] }
        token={ tokenInfoERC20a }
        top={ 0 }
        showSocketInfo={ false }
      />,
    );

    expect(container.querySelectorAll('[data-token-transfer-row]')).toHaveLength(2);
  });

  it('shows the socket notice above the rows on the first page', () => {
    const { container } = render(
      <TokenTransferTable data={ [ tokenTransferMock.erc20 ] } token={ tokenInfoERC20a } top={ 0 } showSocketInfo socketInfoNum={ 3 }/>,
    );

    expect(container.querySelector('tbody')?.textContent).toContain('3 ');
  });
});
