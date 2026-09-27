// @vitest-environment jsdom

import React from 'react';

import * as lineMock from 'mocks/stats/line';
import * as linesMock from 'mocks/stats/lines';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { STATS_OVERVIEW_SECTION } from 'ui/stats/constants';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import Stats from './Stats';

const responseInit = { headers: { 'Content-Type': 'application/json' } };

const sections = linesMock.base.sections;

const counters = {
  counters: [
    { id: 'totalAddresses', title: 'Total addresses', value: '1804523', description: 'Addresses that ever appeared on chain' },
    { id: 'totalTxns', title: 'Total transactions', value: '196150000', description: 'Every transaction the chain carries' },
  ],
};

const scrollIntoView = vi.fn();

const renderPage = async() => {
  const result = render(<Stats/>);

  await waitFor(() => {
    expect(result.container.querySelectorAll('[data-stats-section-nav] [data-section-id]').length)
      .toBe(sections.length + 1);
  });

  return result;
};

describe('the statistics page', () => {
  beforeEach(() => {
    routerState.pathname = '/stats';
    routerState.query = {};
    scrollIntoView.mockClear();
    Object.defineProperty(Element.prototype, 'scrollIntoView', { writable: true, value: scrollIntoView });
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => {
      if (request.url.includes('/api/v1/counters')) {
        return Promise.resolve({ body: JSON.stringify(counters), ...responseInit });
      }
      if (request.url.includes('/api/v1/lines/')) {
        return Promise.resolve({
          body: JSON.stringify({ info: sections[0].charts[0], chart: lineMock.averageGasPrice.chart }),
          ...responseInit,
        });
      }
      if (request.url.includes('/api/v1/lines')) {
        return Promise.resolve({ body: JSON.stringify(linesMock.base), ...responseInit });
      }
      return Promise.resolve({ body: JSON.stringify({}), ...responseInit });
    });
  });

  it('heads the page and carries the filters above the content', async() => {
    const { container } = await renderPage();

    expect(container.querySelector('h1')).not.toBeNull();
    expect(container.querySelector('[data-stats-filters]')).not.toBeNull();
  });

  it('lists the overview first and then every chart section in the side navigation', async() => {
    const { container } = await renderPage();

    const entries = Array.from(container.querySelectorAll('[data-stats-section-nav] [data-section-id]'));

    expect(entries.map((entry) => entry.getAttribute('data-section-id')))
      .toEqual([ STATS_OVERVIEW_SECTION.id, ...sections.map((section) => section.id) ]);
    expect(entries.map((entry) => entry.textContent))
      .toEqual([ STATS_OVERVIEW_SECTION.title, ...sections.map((section) => section.title) ]);
  });

  it('opens with the overview marked', async() => {
    const { container } = await renderPage();

    const marked = container.querySelectorAll('[data-stats-section-nav] [data-section-active]');

    expect(marked).toHaveLength(1);
    expect(marked[0].getAttribute('data-section-id')).toBe(STATS_OVERVIEW_SECTION.id);
  });

  it('scrolls to a section and marks it when its entry is chosen', async() => {
    const { container } = await renderPage();

    (container.querySelector(`[data-stats-section-nav] [data-section-id="${ sections[1].id }"]`) as HTMLElement).click();

    await waitFor(() => {
      const marked = container.querySelector('[data-stats-section-nav] [data-section-active]');
      expect(marked?.getAttribute('data-section-id')).toBe(sections[1].id);
    });
    expect(scrollIntoView).toHaveBeenCalled();
  });

  it('puts the counters grid in the overview section', async() => {
    const { container } = await renderPage();

    const overview = container.querySelector(`[data-stats-section="${ STATS_OVERVIEW_SECTION.id }"]`) as HTMLElement;

    expect(overview.querySelector(`h2#${ STATS_OVERVIEW_SECTION.id }`)?.textContent).toBe(STATS_OVERVIEW_SECTION.title);
    await waitFor(() => {
      expect(overview.querySelectorAll('[data-stats-number-widgets] [data-scan-stat]')).toHaveLength(counters.counters.length);
    });
  });

  it('puts the chart sections under the counters, each anchored on its own id', async() => {
    const { container } = await renderPage();

    await waitFor(() => {
      const blocks = Array.from(container.querySelectorAll('[data-stats-section]'));
      expect(blocks.map((block) => block.getAttribute('data-stats-section')))
        .toEqual([ STATS_OVERVIEW_SECTION.id, ...sections.map((section) => section.id) ]);
    });
  });
});
