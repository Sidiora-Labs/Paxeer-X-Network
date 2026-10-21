// @vitest-environment jsdom

import type { UseQueryResult } from '@tanstack/react-query';
import React from 'react';

import type { Address } from 'types/api/address';
import type { TokenInfo, TokenVerifiedInfo } from 'types/api/token';

import type { ResourceError } from 'lib/api/resources';
import * as addressMock from 'mocks/address/address';
import { tokenInfo } from 'mocks/tokens/tokenInfo';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TokenPageTitle from './TokenPageTitle';

Object.defineProperty(document, 'fonts', {
  configurable: true,
  value: { load: () => Promise.resolve([ {} ]) },
});

const HASH = tokenInfo.address_hash;

const verifiedInfo: TokenVerifiedInfo = {
  iconUrl: 'http://localhost:3000/token-icon.png',
  projectEmail: 'team@arianee.org',
  projectName: 'Arianee',
  projectSector: 'DeFi',
  projectWebsite: 'https://arianee.org/tokens',
  projectDescription: 'The Arianee protocol',
  requesterEmail: 'team@arianee.org',
  requesterName: 'Arianee',
  tokenAddress: HASH,
  updatedAt: '2023-05-01T00:00:00.000Z',
};

const tokenQuery = (overrides?: Partial<UseQueryResult<TokenInfo, ResourceError<unknown>>>) => ({
  data: tokenInfo,
  isPlaceholderData: false,
  isPending: false,
  isError: false,
  ...overrides,
}) as unknown as UseQueryResult<TokenInfo, ResourceError<unknown>>;

const addressQuery = (data: Address = addressMock.contract) => ({
  data,
  isPlaceholderData: false,
  isPending: false,
  isError: false,
}) as unknown as UseQueryResult<Address, ResourceError<unknown>>;

const verifiedInfoQuery = (data?: TokenVerifiedInfo) => ({
  data,
  isPlaceholderData: false,
  isPending: false,
  isError: !data,
}) as unknown as UseQueryResult<TokenVerifiedInfo, ResourceError<unknown>>;

const renderTitle = (
  token = tokenQuery(),
  address = addressQuery(),
  verified = verifiedInfoQuery(verifiedInfo),
) => render(
  <TokenPageTitle tokenQuery={ token } addressQuery={ address } verifiedInfoQuery={ verified } hash={ HASH }/>,
);

describe('TokenPageTitle', () => {
  beforeEach(() => {
    routerState.pathname = '/token/[hash]';
    routerState.query = { hash: HASH };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({ addresses: {} }), { headers: { 'Content-Type': 'application/json' } });
  });

  it('heads the page with the word Token and carries the name and symbol beside it', () => {
    const { container } = renderTitle();

    expect(container.querySelector('h1')?.textContent).toContain('Token');
    expect(container.querySelector('[data-token-name]')?.textContent).toBe('ARIANEE (ARIA)');
  });

  it('marks the token as verified when the contract info service answered for it', () => {
    const { container } = renderTitle();

    expect(container.querySelector('[data-token-verified]')).not.toBeNull();
  });

  it('leaves the verified mark off when no verified info was found', () => {
    const { container } = renderTitle(tokenQuery(), addressQuery(), verifiedInfoQuery());

    expect(container.querySelector('[data-token-verified]')).toBeNull();
  });

  it('puts the standard and the source-code chips on the row beneath the title', () => {
    const { container } = renderTitle();

    const chips = container.querySelector('[data-token-chips]') as HTMLElement;
    const methods = Array.from(chips.querySelectorAll('[data-scan-method]')).map((chip) => chip.getAttribute('data-scan-method'));

    expect(methods).toContain('ERC-20');
    expect(methods).toContain('Source Code (Proxy)');
  });

  it('names the implementation behind the proxy on its own chip', () => {
    const { container } = renderTitle();

    const implementation = container.querySelector('[data-token-implementation]') as HTMLElement;

    expect(implementation).not.toBeNull();
    expect(implementation.textContent).toContain('Implementation');
    expect(implementation.querySelector('[data-entity-kind="address"]')).not.toBeNull();
  });

  it('says Source Code without the proxy note when the contract has no implementation', () => {
    const { container } = renderTitle(tokenQuery(), addressQuery({ ...addressMock.contract, implementations: [] }));

    const methods = Array.from(container.querySelectorAll('[data-scan-method]')).map((chip) => chip.getAttribute('data-scan-method'));

    expect(methods).toContain('Source Code');
    expect(methods).not.toContain('Source Code (Proxy)');
    expect(container.querySelector('[data-token-implementation]')).toBeNull();
  });

  it('keeps the project link, the API entry and the menu on the right of the chip row', () => {
    const { container } = renderTitle();

    const actions = container.querySelector('[data-token-actions]') as HTMLElement;

    expect(actions.querySelector('[data-token-project]')).not.toBeNull();
    expect(actions.querySelector('[data-token-api-link]')).not.toBeNull();
    expect(container.querySelector('[data-token-chip-row]')?.contains(actions)).toBe(true);
  });
});
