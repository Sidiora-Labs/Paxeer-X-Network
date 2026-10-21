// @vitest-environment jsdom

import React from 'react';

import { tokenInfoERC721a } from 'mocks/tokens/tokenInfo';
import * as tokenInstanceMock from 'mocks/tokens/tokenInstance';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TokenInventoryItem from './TokenInventoryItem';

Object.defineProperty(document, 'fonts', {
  configurable: true,
  value: { load: () => Promise.resolve([ {} ]) },
});

describe('TokenInventoryItem', () => {
  beforeEach(() => {
    routerState.pathname = '/token/[hash]';
    routerState.query = { hash: tokenInfoERC721a.address_hash, tab: 'inventory' };
    fetchMock.resetMocks();
  });

  it('carries the card surface the scan pages use', () => {
    const { container } = render(
      <TokenInventoryItem item={ tokenInstanceMock.base } token={ tokenInfoERC721a } isLoading={ false }/>,
    );

    const card = container.querySelector('[data-inventory-item]') as HTMLElement;

    expect(card).not.toBeNull();
    expect(card.getAttribute('data-inventory-item')).toBe(tokenInstanceMock.base.id);
  });

  it('links the instance by its id', () => {
    const { container } = render(
      <TokenInventoryItem item={ tokenInstanceMock.base } token={ tokenInfoERC721a } isLoading={ false }/>,
    );

    const link = container.querySelector(`a[href*="/instance/${ tokenInstanceMock.base.id }"]`);

    expect(link).not.toBeNull();
    expect(container.textContent).toContain('ID#');
  });

  it('names the owner when the instance carries one', () => {
    const { container } = render(
      <TokenInventoryItem item={ tokenInstanceMock.unique } token={ tokenInfoERC721a } isLoading={ false }/>,
    );

    expect(container.textContent).toContain('Owner');
    expect(container.querySelector('[data-entity-kind="address"]')).not.toBeNull();
  });

  it('leaves the owner row out when the instance has none', () => {
    const { container } = render(
      <TokenInventoryItem item={{ ...tokenInstanceMock.base, owner: null }} token={ tokenInfoERC721a } isLoading={ false }/>,
    );

    expect(container.textContent).not.toContain('Owner');
  });
});
