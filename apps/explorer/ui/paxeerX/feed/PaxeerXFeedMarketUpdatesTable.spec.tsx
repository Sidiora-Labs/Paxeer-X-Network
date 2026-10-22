// @vitest-environment jsdom

import React from 'react';

import * as feedMock from 'mocks/paxeerX/feed';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import PaxeerXFeedMarketUpdatesTable from './PaxeerXFeedMarketUpdatesTable';

const renderTable = () => render(<PaxeerXFeedMarketUpdatesTable items={ feedMock.marketUpdates }/>);

describe('PaxeerXFeedMarketUpdatesTable', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('heads the table with the history columns in order', () => {
    const { container } = renderTable();

    expect(Array.from(container.querySelectorAll('thead th')).map((cell) => cell.textContent))
      .toEqual([ 'Update', 'Block', 'Age', 'Index price', 'Change', 'Mark price', 'Sequence' ]);
  });

  it('renders one row per update linked to its details page', () => {
    const { container } = renderTable();

    expect(Array.from(container.querySelectorAll('[data-update]')).map((row) => row.getAttribute('data-update')))
      .toEqual(feedMock.marketUpdates.map((item) => item.update_hash));
    expect(container.querySelector(`[data-update="${ feedMock.updateHash }"] a[href="/feed/update/${ feedMock.updateHash }"]`))
      .toBeTruthy();
  });

  it('shows each price at 18 decimals and its change against the previous update', () => {
    const { container } = renderTable();

    const rows = Array.from(container.querySelectorAll('[data-update]'));

    expect(rows.map((row) => row.querySelectorAll('td')[3]?.textContent)).toEqual([ '67,012.5', '67,100', '67,000' ]);
    expect(rows.map((row) => row.querySelector('[data-change]')?.textContent)).toEqual([ '-0.13%', '+0.15%', '—' ]);
    expect(rows.map((row) => row.querySelectorAll('td')[6]?.textContent)).toEqual([ '9002', '9001', '9000' ]);
  });
});
