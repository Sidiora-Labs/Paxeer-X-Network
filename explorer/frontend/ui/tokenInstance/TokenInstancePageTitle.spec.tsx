// @vitest-environment jsdom

import React from 'react';

import { tokenInfoERC721a } from 'mocks/tokens/tokenInfo';
import * as tokenInstanceMock from 'mocks/tokens/tokenInstance';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TokenInstancePageTitle from './TokenInstancePageTitle';

Object.defineProperty(document, 'fonts', {
  configurable: true,
  value: { load: () => Promise.resolve([ {} ]) },
});

const HASH = tokenInfoERC721a.address_hash;

const renderTitle = (instance = tokenInstanceMock.base, token = tokenInfoERC721a) => render(
  <TokenInstancePageTitle isLoading={ false } token={ token } instance={ instance } hash={ HASH }/>,
);

describe('TokenInstancePageTitle', () => {
  beforeEach(() => {
    routerState.pathname = '/token/[hash]/instance/[id]';
    routerState.query = { hash: HASH, id: tokenInstanceMock.base.id };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({ addresses: {} }), { headers: { 'Content-Type': 'application/json' } });
  });

  it('heads the page the way the token page does', () => {
    const { container } = renderTitle();

    expect(container.querySelector('h1')?.textContent).toContain('Token instance');
  });

  it('carries the instance name beside the heading', () => {
    const { container } = renderTitle();

    expect(container.querySelector('[data-token-instance-name]')?.textContent).toBe(tokenInstanceMock.base.metadata?.name);
  });

  it('falls back to the token name and the id when the metadata carries no name', () => {
    const { container } = renderTitle({ ...tokenInstanceMock.base, metadata: null });

    expect(container.querySelector('[data-token-instance-name]')?.textContent).toBe(`HyFi Athena #${ tokenInstanceMock.base.id }`);
  });

  it('puts the standard chip and the collection link on the row beneath the title', () => {
    const { container } = renderTitle();

    const chips = container.querySelector('[data-token-chips]') as HTMLElement;

    expect(chips.querySelector('[data-scan-method]')?.getAttribute('data-scan-method')).toBe('ERC-721');
    expect(chips.querySelector('[data-entity-kind="token"]')).not.toBeNull();
  });

  it('keeps the external app link with the actions on the right', () => {
    const { container } = renderTitle();

    const actions = container.querySelector('[data-token-actions]') as HTMLElement;
    const appLink = actions.querySelector('[data-token-instance-app-link]') as HTMLAnchorElement;

    expect(appLink).not.toBeNull();
    expect(appLink.textContent).toBe('duck.nft');
  });

  it('leaves the app link out when the instance has no external app', () => {
    const { container } = renderTitle({ ...tokenInstanceMock.base, external_app_url: null });

    expect(container.querySelector('[data-token-instance-app-link]')).toBeNull();
  });
});
