// @vitest-environment jsdom

import type { UseQueryResult } from '@tanstack/react-query';
import React from 'react';

import type { TokenVerifiedInfo as TTokenVerifiedInfo } from 'types/api/token';

import type { ResourceError } from 'lib/api/resources';
import { tokenInfo } from 'mocks/tokens/tokenInfo';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import TokenVerifiedInfo from './TokenVerifiedInfo';

Object.defineProperty(document, 'fonts', {
  configurable: true,
  value: { load: () => Promise.resolve([ {} ]) },
});

const verifiedInfo: TTokenVerifiedInfo = {
  iconUrl: 'http://localhost:3000/token-icon.png',
  projectEmail: 'team@arianee.org',
  projectName: 'Arianee',
  projectSector: 'DeFi',
  projectWebsite: 'https://arianee.org/tokens',
  projectDescription: 'The Arianee protocol',
  requesterEmail: 'team@arianee.org',
  requesterName: 'Arianee',
  tokenAddress: tokenInfo.address_hash,
  updatedAt: '2023-05-01T00:00:00.000Z',
};

const query = (overrides?: Partial<UseQueryResult<TTokenVerifiedInfo, ResourceError<unknown>>>) => ({
  data: verifiedInfo,
  isPending: false,
  isError: false,
  ...overrides,
}) as unknown as UseQueryResult<TTokenVerifiedInfo, ResourceError<unknown>>;

describe('TokenVerifiedInfo', () => {
  beforeEach(() => {
    routerState.pathname = '/token/[hash]';
    routerState.query = { hash: tokenInfo.address_hash };
    fetchMock.resetMocks();
  });

  it('shows the project host on the chip row rather than the whole address', () => {
    const { container } = render(<TokenVerifiedInfo verifiedInfoQuery={ query() }/>);

    const website = container.querySelector('[data-token-website]') as HTMLAnchorElement;

    expect(website).not.toBeNull();
    expect(website.textContent).toBe('arianee.org');
    expect(website.getAttribute('href')).toBe(verifiedInfo.projectWebsite);
  });

  it('keeps the project info button beside the link', () => {
    const { container } = render(<TokenVerifiedInfo verifiedInfoQuery={ query() }/>);

    const row = container.querySelector('[data-token-project]') as HTMLElement;

    expect(row).not.toBeNull();
    expect(row.querySelectorAll('button').length).toBeGreaterThan(0);
  });

  it('holds two chip-sized placeholders while the service is answering', () => {
    const { container } = render(<TokenVerifiedInfo verifiedInfoQuery={ query({ data: undefined, isPending: true }) }/>);

    expect(container.querySelectorAll('[data-token-project] > *')).toHaveLength(2);
  });

  it('renders nothing when the service failed', () => {
    const { container } = render(<TokenVerifiedInfo verifiedInfoQuery={ query({ data: undefined, isError: true }) }/>);

    expect(container.querySelector('[data-token-project]')).toBeNull();
  });

  it('drops the link when the project website is not a URL', () => {
    const { container } = render(
      <TokenVerifiedInfo verifiedInfoQuery={ query({ data: { ...verifiedInfo, projectWebsite: 'arianee' } }) }/>,
    );

    expect(container.querySelector('[data-token-website]')).toBeNull();
  });
});
