// @vitest-environment jsdom

import React from 'react';

import * as lineMock from 'mocks/stats/line';
import * as linesMock from 'mocks/stats/lines';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it } from 'vitest';
import { screen } from 'vitest/lib';

import ChartsWidgetsList from './ChartsWidgetsList';

const responseInit = { headers: { 'Content-Type': 'application/json' } };

const sections = linesMock.base.sections.slice(0, 2);

const renderList = async(props: Partial<React.ComponentProps<typeof ChartsWidgetsList>> = {}) => {
  const result = render(
    <ChartsWidgetsList
      initialFilterQuery=""
      isError={ false }
      isPlaceholderData={ false }
      charts={ sections }
      interval="oneMonth"
      sections={ sections }
      selectedSectionId="all"
      { ...props }
    />,
  );

  await screen.findByText(sections[0].title);

  return result;
};

describe('ChartsWidgetsList', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(() => Promise.resolve({
      body: JSON.stringify({ info: sections[0].charts[0], chart: lineMock.averageGasPrice.chart }),
      ...responseInit,
    }));
  });

  it('gives every section its own block, anchored on the section id', async() => {
    const { container } = await renderList();

    const blocks = Array.from(container.querySelectorAll('[data-stats-section]'));

    expect(blocks.map((block) => block.getAttribute('data-stats-section')))
      .toEqual(sections.map((section) => section.id));
    sections.forEach((section) => {
      expect(container.querySelector(`h2#${ section.id }`)).not.toBeNull();
    });
  });

  it('titles each section with the name the statistics service gives it', async() => {
    const { container } = await renderList();

    const titles = Array.from(container.querySelectorAll('[data-section-title]')).map((title) => title.textContent);

    expect(titles).toEqual(sections.map((section) => section.title));
  });

  it('lays the charts of a section out in a grid of chart cards', async() => {
    const { container } = await renderList();

    const block = container.querySelector(`[data-stats-section="${ sections[0].id }"]`) as HTMLElement;
    const grid = block.querySelector('[data-stats-section-charts]') as HTMLElement;
    const cards = Array.from(grid.querySelectorAll('[data-chart-card]'));

    expect(cards.map((card) => card.getAttribute('data-chart-card')))
      .toEqual(sections[0].charts.map((chart) => chart.id));
  });

  it('sends every card to the chart page of its own chart', async() => {
    const { container } = await renderList();

    const firstChart = sections[0].charts[0];
    const card = container.querySelector(`[data-chart-card="${ firstChart.id }"]`) as HTMLElement;

    expect(card.querySelector('[data-chart-view]')?.getAttribute('href')).toBe(`/stats/${ firstChart.id }`);
  });

  it('reports the failure instead of the grid when the sections did not load', async() => {
    const { container } = render(
      <ChartsWidgetsList
        initialFilterQuery=""
        isError={ true }
        isPlaceholderData={ false }
        charts={ sections }
        interval="oneMonth"
        sections={ sections }
        selectedSectionId="all"
      />,
    );

    expect(container.querySelector('[data-charts-error]')).not.toBeNull();
    expect(container.querySelector('[data-stats-section]')).toBeNull();
  });

  it('shows an empty state when the filter leaves no chart standing', async() => {
    const { container } = render(
      <ChartsWidgetsList
        initialFilterQuery="nothing matches this"
        isError={ false }
        isPlaceholderData={ false }
        charts={ [] }
        interval="oneMonth"
        sections={ sections }
        selectedSectionId="all"
      />,
    );

    expect(container.querySelector('[data-stats-section-charts]')).toBeNull();
  });
});
