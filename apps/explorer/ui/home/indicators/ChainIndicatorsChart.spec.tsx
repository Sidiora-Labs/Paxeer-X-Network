// @vitest-environment jsdom

import React from 'react';

import type { TimeChartData } from 'toolkit/components/charts/types';

import { render } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import ChainIndicatorsChart from './ChainIndicatorsChart';

const buildData = (days: number): TimeChartData => ([ {
  id: 'daily_txs',
  name: 'Tx/day',
  charts: [ { type: 'area', gradient: { startColor: 'blue.100', stopColor: 'blue.500' } } ],
  items: Array.from({ length: days }, (item, index) => ({
    date: new Date(Date.UTC(2022, 10, index + 1)),
    value: 1000 + index,
  })),
} ]);

// jsdom implements <path> as SVGElement and leaves the geometry methods of SVGGeometryElement out,
// so the chart line animation of the toolkit has nothing to measure without this shim.
if (!('getTotalLength' in SVGElement.prototype)) {
  Object.defineProperty(SVGElement.prototype, 'getTotalLength', { configurable: true, value: () => 0 });
}

describe('ChainIndicatorsChart', () => {
  it('titles the sparkline and draws it', () => {
    const { container } = render(
      <ChainIndicatorsChart
        isLoading={ false }
        title="Blockscout transaction history in 14 days"
        chartQuery={{ isError: false, isPending: false, data: buildData(30) }}
      />,
    );

    const chart = container.querySelector('[data-label="chain-indicator-chart"]') as HTMLElement;

    expect(chart.querySelector('[data-title]')?.textContent).toBe('Blockscout transaction history in 14 days');
    expect(chart.querySelector('[data-label="sparkline"]')).not.toBeNull();
  });

  it('trims the series to the last days it is asked for', () => {
    const data = buildData(30);
    const { container } = render(
      <ChainIndicatorsChart
        isLoading={ false }
        title="Blockscout transaction history in 14 days"
        chartQuery={{ isError: false, isPending: false, data }}
        days={ 14 }
      />,
    );

    expect(container.querySelector('[data-label="chain-indicator-chart"]')?.getAttribute('data-points')).toBe('14');
    expect(data[0].items).toHaveLength(30);
  });

  it('falls back to a notice when the series cannot be fetched', () => {
    const { container } = render(
      <ChainIndicatorsChart
        isLoading={ false }
        title="Blockscout transaction history in 14 days"
        chartQuery={{ isError: true, isPending: false, data: [] }}
      />,
    );

    expect(container.querySelector('[data-label="sparkline"]')).toBeNull();
  });
});
