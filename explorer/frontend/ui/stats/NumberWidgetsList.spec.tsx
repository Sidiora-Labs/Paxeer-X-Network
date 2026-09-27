// @vitest-environment jsdom

import React from 'react';

import type * as stats from '@blockscout/stats-types';

import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it } from 'vitest';
import { screen, waitFor } from 'vitest/lib';

import NumberWidgetsList, { formatCounterUnits, formatCounterValue, getCounterDeltaDirection } from './NumberWidgetsList';

const counters: stats.Counters = {
  counters: [
    { id: 'totalAddresses', title: 'Total addresses', value: '1804523', description: 'Addresses that ever appeared on chain' },
    { id: 'totalTxns', title: 'Total transactions', value: '196150000', description: 'Every transaction the chain carries' },
    { id: 'averageBlockTime', title: 'Average block time', value: '0.175', units: 's', description: 'Mean time between two blocks' },
    { id: 'networkUtilization', title: 'Network utilization', value: '12.5', units: '%', description: 'Share of the gas limit in use' },
    { id: 'newAddresses24h', title: 'New addresses change', value: '-4.25', units: '%', description: 'Change over the last day' },
  ],
};

const responseInit = { headers: { 'Content-Type': 'application/json' } };

const renderList = async() => {
  const result = render(<NumberWidgetsList/>);

  await screen.findByText('Total addresses');

  return result;
};

describe('formatCounterValue', () => {
  it('shortens a large counter and keeps a small one readable', () => {
    expect(formatCounterValue('1804523')).toBe('1.805M');
    expect(formatCounterValue('0.175')).toBe('0.175');
  });
});

describe('formatCounterUnits', () => {
  it('writes seconds against the number and every other unit after a space', () => {
    expect(formatCounterUnits('s')).toBe('s');
    expect(formatCounterUnits('%')).toBe('%');
    expect(formatCounterUnits('PAX')).toBe(' PAX');
    expect(formatCounterUnits()).toBe('');
  });
});

describe('getCounterDeltaDirection', () => {
  it('reads a percentage counter as a rising or a falling change', () => {
    expect(getCounterDeltaDirection({ id: 'a', title: 'a', value: '12.5', units: '%', description: '' })).toBe('up');
    expect(getCounterDeltaDirection({ id: 'b', title: 'b', value: '-4.25', units: '%', description: '' })).toBe('down');
  });

  it('leaves an absolute counter without a change tone', () => {
    expect(getCounterDeltaDirection({ id: 'c', title: 'c', value: '42', description: '' })).toBeUndefined();
    expect(getCounterDeltaDirection({ id: 'd', title: 'd', value: 'not a number', units: '%', description: '' })).toBeUndefined();
  });
});

describe('NumberWidgetsList', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(() => Promise.resolve({ body: JSON.stringify(counters), ...responseInit }));
  });

  it('lays the counters out as a grid of stat cards', async() => {
    const { container } = await renderList();

    const grid = container.querySelector('[data-stats-number-widgets]') as HTMLElement;
    const cards = Array.from(grid.querySelectorAll('[data-scan-stat]'));

    expect(cards).toHaveLength(counters.counters.length);
    expect(cards.map((card) => card.getAttribute('data-scan-stat')))
      .toEqual(counters.counters.map((counter) => counter.title));
  });

  it('gives every card its label, its value and its information icon', async() => {
    const { container } = await renderList();

    const card = container.querySelector('[data-scan-stat="Total addresses"]') as HTMLElement;

    expect(card.querySelector('[data-label]')?.textContent).toBe('Total addresses');
    expect(card.querySelector('[data-value]')?.textContent).toBe('1.805M');
    expect(card.querySelector('[aria-label="hint"]')).not.toBeNull();
  });

  it('writes the units of a counter that carries them beside its value', async() => {
    const { container } = await renderList();

    const card = container.querySelector('[data-scan-stat="Average block time"]') as HTMLElement;

    expect(card.querySelector('[data-value]')?.textContent).toBe('0.175s');
  });

  it('tones a rising change apart from a falling one', async() => {
    const { container } = await renderList();

    await waitFor(() => {
      expect(container.querySelector('[data-scan-stat="Network utilization"] [data-delta="up"]')?.textContent).toBe('12.5%');
    });
    expect(container.querySelector('[data-scan-stat="New addresses change"] [data-delta="down"]')?.textContent).toBe('-4.3%');
  });
});
