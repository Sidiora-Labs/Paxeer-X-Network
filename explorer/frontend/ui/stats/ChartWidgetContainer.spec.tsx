// @vitest-environment jsdom

import React from 'react';

import type * as stats from '@blockscout/stats-types';

import * as lineMock from 'mocks/stats/line';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from 'vitest/lib';

import ChartWidgetContainer, { formatChartValue } from './ChartWidgetContainer';

const responseInit = { headers: { 'Content-Type': 'application/json' } };

const info: stats.LineChartInfo = {
  id: 'averageGasPrice',
  title: 'Average gas price',
  description: 'The mean gas price of the period',
  units: 'Gwei',
  resolutions: [ 'DAY', 'MONTH' ],
};

const chart = lineMock.averageGasPrice.chart;
const lastValue = Number(chart[chart.length - 1].value);

const mockLine = (payload: stats.LineChart) => {
  fetchMock.resetMocks();
  fetchMock.mockResponse(() => Promise.resolve({ body: JSON.stringify(payload), ...responseInit }));
};

const renderCard = async(props: Partial<React.ComponentProps<typeof ChartWidgetContainer>> = {}) => {
  const result = render(
    <ChartWidgetContainer
      id="averageGasPrice"
      title="Average gas price"
      description="The mean gas price of the period"
      interval="oneMonth"
      isPlaceholderData={ false }
      onLoadingError={ vi.fn() }
      href={{ pathname: '/stats/[id]', query: { id: 'averageGasPrice' } }}
      { ...props }
    />,
  );

  await screen.findByText(/Average gas price/);

  return result;
};

describe('formatChartValue', () => {
  it('writes the units after the figure and leaves a unitless chart bare', () => {
    expect(formatChartValue(1234.5678, 'Gwei')).toBe('1,234.5678 Gwei');
    expect(formatChartValue(42)).toBe('42');
  });
});

describe('ChartWidgetContainer', () => {
  beforeEach(() => {
    mockLine({ info, chart });
  });

  it('heads the card with the chart title and the interval it is showing', async() => {
    const { container } = await renderCard();

    expect(container.querySelector('[data-chart-card="averageGasPrice"]')).not.toBeNull();
    expect(container.querySelector('[data-chart-title]')?.textContent).toBe('Average gas price (1M)');
  });

  it('puts an information icon beside the title', async() => {
    const { container } = await renderCard();

    const header = container.querySelector('[data-chart-card-header]') as HTMLElement;

    expect(header.querySelector('[aria-label="hint"]')).not.toBeNull();
  });

  it('puts the view link on the right of the header', async() => {
    const { container } = await renderCard();

    const link = container.querySelector('[data-chart-view]') as HTMLAnchorElement;

    expect(link.textContent).toContain('View');
    expect(link.getAttribute('href')).toBe('/stats/averageGasPrice');
  });

  it('leaves the view link out when the card leads nowhere', async() => {
    const { container } = await renderCard({ href: undefined });

    expect(container.querySelector('[data-chart-view]')).toBeNull();
  });

  it('writes the current value beneath the title', async() => {
    const { container } = await renderCard();

    await waitFor(() => {
      expect(container.querySelector('[data-chart-value]')?.textContent).toBe(formatChartValue(lastValue, 'Gwei'));
    });
  });

  it('draws the chart on its own surface under the header', async() => {
    const { container } = await renderCard();

    const surface = container.querySelector('[data-chart-surface]') as HTMLElement;

    await waitFor(() => {
      expect(surface.querySelector('svg')).not.toBeNull();
    });
  });

  it('says so when the chart carries no data', async() => {
    mockLine({ info, chart: [] });

    const { container } = await renderCard();

    await waitFor(() => {
      expect(container.querySelector('[data-chart-surface]')?.textContent).toBe('No data');
    });
    expect(container.querySelector('[data-chart-value]')).toBeNull();
  });
});
