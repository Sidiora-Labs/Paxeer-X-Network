// @vitest-environment jsdom

import React from 'react';

import type { TimeChartData } from 'toolkit/components/charts/types';

import { render } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import ChainIndicatorChartContent from './ChainIndicatorChartContent';

const data: TimeChartData = [ {
  id: 'daily_txs',
  name: 'Tx/day',
  charts: [ { type: 'area', gradient: { startColor: 'blue.100', stopColor: 'blue.500' } } ],
  items: Array.from({ length: 14 }, (item, index) => ({
    date: new Date(Date.UTC(2022, 10, index + 1)),
    value: 1000 + index * 10,
  })),
} ];

// jsdom implements <path> as SVGElement and leaves the geometry methods of SVGGeometryElement out,
// so the chart line animation of the toolkit has nothing to measure without this shim.
if (!('getTotalLength' in SVGElement.prototype)) {
  Object.defineProperty(SVGElement.prototype, 'getTotalLength', { configurable: true, value: () => 0 });
}

describe('ChainIndicatorChartContent', () => {
  it('draws the series as a sparkline', () => {
    const { container } = render(<ChainIndicatorChartContent data={ data }/>);

    const svg = container.querySelector('[data-label="sparkline"]') as SVGSVGElement;

    expect(svg).not.toBeNull();
    expect(svg.querySelector('path')).not.toBeNull();
  });

  it('labels the sparkline on both axes', () => {
    const { container } = render(<ChainIndicatorChartContent data={ data }/>);

    expect(container.querySelector('[data-label="sparkline-y-axis"]')).not.toBeNull();
    expect(container.querySelector('[data-label="sparkline-x-axis"]')).not.toBeNull();
  });

  it('dates the ends of the series along the bottom axis', () => {
    const { container } = render(<ChainIndicatorChartContent data={ data }/>);

    const xAxis = container.querySelector('[data-label="sparkline-x-axis"]') as SVGGElement;

    expect(xAxis.querySelectorAll('text').length).toBeGreaterThan(0);
  });
});
