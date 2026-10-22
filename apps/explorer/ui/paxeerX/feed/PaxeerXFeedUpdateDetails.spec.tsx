// @vitest-environment jsdom

import React from 'react';

import * as feedMock from 'mocks/paxeerX/feed';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import PaxeerXFeedUpdateDetails from './PaxeerXFeedUpdateDetails';

describe('PaxeerXFeedUpdateDetails', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('shows the hash, block, feeder, gas and market count of the update', () => {
    const { container } = render(<PaxeerXFeedUpdateDetails data={ feedMock.priceUpdate }/>);

    expect(container.querySelector('[data-field="hash"]')?.textContent).toBe(feedMock.updateHash);
    expect(container.querySelector('a[href="/block/34800123"]')?.textContent).toBe('34800123');
    expect(container.querySelector(`a[href="/address/${ feedMock.feederHash }"]`)).toBeTruthy();
    expect(container.querySelector('[data-field="feeder_bech32"]')?.textContent).toBe(feedMock.feederBech32);
    expect(container.querySelector('[data-field="gas"]')?.textContent).toBe('84,211 of 200,000');
    expect(container.querySelector('[data-field="observation_count"]')?.textContent).toBe('2');
    expect(screen.getByText('Success')).toBeTruthy();
  });

  it('marks a failed submission', () => {
    render(<PaxeerXFeedUpdateDetails data={ feedMock.failedPriceUpdate }/>);

    expect(screen.getByText('Failed')).toBeTruthy();
  });

  it('lists the observations by market id with prices at 18 decimals', () => {
    const { container } = render(<PaxeerXFeedUpdateDetails data={ feedMock.priceUpdate }/>);

    const rows = Array.from(container.querySelectorAll('[data-observation]'));

    expect(rows.map((row) => row.getAttribute('data-observation'))).toEqual([ '0', '1' ]);
    expect(rows.map((row) => Array.from(row.querySelectorAll('td')).map((cell) => cell.textContent)).map((cells) => [
      cells[0], cells[1], cells[2], cells[4],
    ])).toEqual([
      [ 'BTC-USD', '67,012.5', '67,020.25', '9002' ],
      [ 'ETH-USD', '3,456.78901235', '3,457', '9001' ],
    ]);
    expect(rows[0]?.querySelector('a[href="/feed/market/0"]')).toBeTruthy();
  });
});
