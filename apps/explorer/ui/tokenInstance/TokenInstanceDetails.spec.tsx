// @vitest-environment jsdom

import React from 'react';

import * as addressMock from 'mocks/address/address';
import { tokenInfoERC721a } from 'mocks/tokens/tokenInfo';
import * as tokenInstanceMock from 'mocks/tokens/tokenInstance';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import { MetadataUpdateProvider } from './contexts/metadataUpdate';
import TokenInstanceDetails from './TokenInstanceDetails';

Object.defineProperty(document, 'fonts', {
  configurable: true,
  value: { load: () => Promise.resolve([ {} ]) },
});

const HASH = tokenInfoERC721a.address_hash;

const json = (payload: unknown) => ({
  body: JSON.stringify(payload),
  headers: { 'Content-Type': 'application/json' },
});

const renderDetails = (data = tokenInstanceMock.unique) => render(
  <MetadataUpdateProvider>
    <TokenInstanceDetails data={ data } token={ tokenInfoERC721a }/>
  </MetadataUpdateProvider>,
);

const cardTitles = (container: HTMLElement) =>
  Array.from(container.querySelectorAll('[data-token-card]')).map((card) => card.getAttribute('data-token-card'));

describe('TokenInstanceDetails', () => {
  beforeEach(() => {
    routerState.pathname = '/token/[hash]/instance/[id]';
    routerState.query = { hash: HASH, id: tokenInstanceMock.unique.id };
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => {
      const { pathname } = new URL(request.url);

      if (pathname === `/api/v2/addresses/${ HASH }`) {
        return Promise.resolve(json(addressMock.contract));
      }

      if (pathname.endsWith('/transfers-count')) {
        return Promise.resolve(json({ transfers_count: 42 }));
      }

      return Promise.resolve(json({ items: [], next_page_params: null }));
    });
  });

  it('splits the instance details into the media, overview, other-info and metadata cards', () => {
    const { container } = renderDetails();

    expect(cardTitles(container)).toEqual([ 'Media', 'Overview', 'Other info', 'Metadata' ]);
  });

  it('keeps the owner and the token id in the overview card', () => {
    const { container } = renderDetails();

    const overview = container.querySelector('[data-token-card="Overview"]') as HTMLElement;
    const labels = Array.from(overview.querySelectorAll('[data-scan-key]')).map((key) => key.textContent ?? '');

    expect(labels).toContain('Owner');
    expect(labels).toContain('Token ID');
    expect(overview.querySelector('[data-token-instance-id]')).not.toBeNull();
  });

  it('drops the owner row for an instance that is not unique', () => {
    const { container } = renderDetails(tokenInstanceMock.base);

    const overview = container.querySelector('[data-token-card="Overview"]') as HTMLElement;
    const labels = Array.from(overview.querySelectorAll('[data-scan-key]')).map((key) => key.textContent ?? '');

    expect(labels).not.toContain('Owner');
  });

  it('adds the creator and the transfer count to the overview card once the api answered', async() => {
    const { container } = renderDetails();

    await waitFor(() => {
      const overview = container.querySelector('[data-token-card="Overview"]') as HTMLElement;
      const labels = Array.from(overview.querySelectorAll('[data-detailed-info-label]')).map((key) => key.textContent ?? '');

      expect(labels).toContain('Creator');
      expect(labels).toContain('Transfers');
      expect(overview.textContent).toContain('42');
    });
  });

  it('puts the metadata card below the card row', () => {
    const { container } = renderDetails();

    const grid = container.querySelector('[data-token-card="Overview"]')?.parentElement as HTMLElement;
    const metadata = container.querySelector('[data-token-card="Metadata"]') as HTMLElement;

    expect(grid.contains(metadata)).toBe(false);
    expect(grid.compareDocumentPosition(metadata) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('renders nothing without an instance', () => {
    const { container } = render(
      <MetadataUpdateProvider>
        <TokenInstanceDetails token={ tokenInfoERC721a }/>
      </MetadataUpdateProvider>,
    );

    expect(container.querySelector('[data-token-instance-details]')).toBeNull();
  });
});
