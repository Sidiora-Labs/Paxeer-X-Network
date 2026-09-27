// @vitest-environment jsdom

import { waitFor } from '@testing-library/react';
import React from 'react';

import { base } from 'mocks/txs/tx';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import Transaction from './Transaction';

const tabTitles = (container: HTMLElement) =>
  Array.from(container.querySelectorAll('[role="tab"]')).map((tab) => tab.textContent);

describe('TransactionPageContent', () => {
  beforeEach(() => {
    routerState.pathname = '/tx/[hash]';
    routerState.query = { hash: base.hash };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify(base), { headers: { 'Content-Type': 'application/json' } });
  });

  it('titles the page and keeps the transaction hash out of the heading', async() => {
    const { container } = render(<Transaction/>);

    await waitFor(() => expect(container.querySelector('h1')?.textContent).toBe('Transaction details'));
  });

  it('opens on the overview section and offers the logs section beside it', async() => {
    const { container } = render(<Transaction/>);

    await waitFor(() => expect(tabTitles(container)).toContain('Overview'));

    const titles = tabTitles(container);

    expect(titles.indexOf('Overview')).toBe(0);
    expect(titles).toContain('Logs');
  });

  it('keeps the API entry point beside the sections', async() => {
    const { container } = render(<Transaction/>);

    await waitFor(() => expect(container.querySelector('[data-tx-api-entry]')).not.toBeNull());

    expect(container.querySelector('[data-tx-api-entry]')?.getAttribute('href')).toBe('/api-docs');
  });

  it('shows the overview detail card under the sections', async() => {
    const { container } = render(<Transaction/>);

    await waitFor(() => expect(container.querySelector('[data-tx-details]')).not.toBeNull());

    expect(container.querySelector('[data-tx-details] [data-tx-info]')).not.toBeNull();
  });
});
