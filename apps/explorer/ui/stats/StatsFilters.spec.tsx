// @vitest-environment jsdom

import React from 'react';

import * as linesMock from 'mocks/stats/lines';
import { render } from 'ui/shared/layout/testWrapper';
import { describe, expect, it, vi } from 'vitest';

import StatsFilters from './StatsFilters';

const sections = linesMock.base.sections.slice(0, 3);

const renderFilters = (props: Partial<React.ComponentProps<typeof StatsFilters>> = {}) => render(
  <StatsFilters
    sections={ sections }
    currentSection="all"
    onSectionChange={ vi.fn() }
    interval="oneMonth"
    onIntervalChange={ vi.fn() }
    onFilterInputChange={ vi.fn() }
    isLoading={ false }
    initialFilterValue=""
    { ...props }
  />,
);

describe('StatsFilters', () => {
  it('carries the section chooser, the interval chooser and the search field in one bar', () => {
    const { container } = renderFilters();

    const bar = container.querySelector('[data-stats-filters]') as HTMLElement;

    expect(bar.querySelector('[data-stats-filter-section]')).not.toBeNull();
    expect(bar.querySelector('[data-stats-filter-interval]')).not.toBeNull();
    expect(bar.querySelector('[data-stats-filter-input]')).not.toBeNull();
  });

  it('shows the interval the page is on', () => {
    const { container } = renderFilters({ interval: 'threeMonths' });

    expect(container.querySelector('[data-stats-filter-interval] [data-chart-interval-select="threeMonths"]')).not.toBeNull();
  });

  it('hands a chosen interval back to the page', () => {
    const onIntervalChange = vi.fn();
    const { container } = renderFilters({ onIntervalChange });

    (container.querySelector('[data-stats-filter-interval] [data-id="sixMonths"]') as HTMLElement).click();

    expect(onIntervalChange).toHaveBeenCalledWith('sixMonths');
  });

  it('takes a chart name to search for', () => {
    const onFilterInputChange = vi.fn();
    const { container } = renderFilters({ onFilterInputChange });

    const input = container.querySelector('[data-stats-filter-input] input') as HTMLInputElement;

    expect(input.placeholder).toBe('Find chart, metric...');
  });
});
