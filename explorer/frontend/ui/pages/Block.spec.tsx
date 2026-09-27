// @vitest-environment jsdom

import React from 'react';

import * as blockMock from 'mocks/blocks/block';
import * as internalTxsMock from 'mocks/txs/internalTxs';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from 'vitest/lib';

const routerSpy = vi.hoisted(() => ({ push: vi.fn(() => Promise.resolve(true)) }));

vi.mock('next/router', async() => {
  const base = (await import('ui/shared/layout/testWrapper')).nextRouterModule();

  return {
    ...base,
    useRouter: () => ({ ...base.useRouter(), push: routerSpy.push }),
  };
});

import Block from './Block';

const responseInit = {
  headers: {
    'Content-Type': 'application/json',
  },
};

const height = String(blockMock.base.height);

const renderPage = async() => {
  const result = render(<Block/>);

  await screen.findByText(blockMock.base.hash);

  return result;
};

describe('BlockPageContent', () => {
  beforeEach(() => {
    routerState.pathname = '/block/[height_or_hash]';
    routerState.query = { height_or_hash: height };
    routerSpy.push.mockClear();
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => Promise.resolve({
      body: JSON.stringify(
        request.url.includes('/internal-transactions') ? internalTxsMock.baseResponse : blockMock.base,
      ),
      ...responseInit,
    }));
  });

  it('heads the page with the block title and its number', async() => {
    const { container } = await renderPage();

    expect(screen.getByRole('heading', { level: 1 }).textContent).toContain('Block');
    expect(container.querySelector('[data-block-number]')?.textContent).toBe(`#${ height }`);
  });

  it('lists the sections the block carries as pills', async() => {
    const { container } = await renderPage();

    const tabs = container.querySelector('[data-scan-section-tabs]') as HTMLElement;
    const titles = Array.from(tabs.querySelectorAll('[data-tab]')).map((tab) => tab.textContent);

    expect(titles).toEqual([ 'Overview', 'Transactions', 'Internal txns' ]);
  });

  it('puts the api entry beside the pills', async() => {
    const { container } = await renderPage();

    const rightSlot = container.querySelector('[data-scan-section-tabs] [data-right-slot]') as HTMLElement;

    expect(rightSlot.querySelector('[data-api-entry]')?.textContent).toBe('API');
  });

  it('opens on the overview, which is the details card', async() => {
    const { container } = await renderPage();

    expect(container.querySelector('[data-block-details-card]')).not.toBeNull();
    expect(container.querySelector('[data-scan-table-card]')).toBeNull();
  });

  it('routes to the section a pill names, keeping the block out of the query string', async() => {
    const { container } = await renderPage();

    (container.querySelector('[data-tab="txs"]') as HTMLElement).click();

    await waitFor(() => {
      expect(routerSpy.push).toHaveBeenCalledWith(
        { pathname: '/block/[height_or_hash]', query: { height_or_hash: height, tab: 'txs' } },
        undefined,
        { shallow: true },
      );
    });
  });

  it('shows the internal transactions card when the route names that section', async() => {
    routerState.query = { height_or_hash: height, tab: 'internal_txs' };

    const { container } = render(<Block/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
        .toBe(`A total of ${ blockMock.base.internal_transactions_count } internal transactions found`);
    });
    expect(container.querySelector('[data-block-details-card]')).toBeNull();
  });
});
