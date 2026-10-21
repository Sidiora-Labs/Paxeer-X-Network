// @vitest-environment jsdom

import React from 'react';

import { render } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import HighlightsItem from './HighlightsItem';

describe('HighlightsItem', () => {
  it('shows the label, the value, the secondary figure and the delta of one statistic', () => {
    const { container } = render(
      <HighlightsItem
        id="total_txs"
        label="Transactions"
        value="82.26M"
        secondary="0.3 TPS"
        delta={{ value: '3.2%', direction: 'up' }}
      />,
    );

    const item = container.querySelector('[data-highlight="total_txs"]') as HTMLElement;

    expect(item.querySelector('[data-label]')?.textContent).toBe('Transactions');
    expect(item.querySelector('[data-value]')?.textContent).toBe('82.26M');
    expect(item.querySelector('[data-secondary]')?.textContent).toBe('(0.3 TPS)');
    expect(item.querySelector('[data-delta="up"]')?.textContent).toBe('(3.2%)');
  });

  it('links the statistic to its page when it has one', () => {
    const { container } = render(
      <HighlightsItem id="total_blocks" label="Latest block" value="30,146,364" href={{ pathname: '/blocks' }}/>,
    );

    expect(container.querySelector('[data-highlight="total_blocks"] a')?.getAttribute('href')).toBe('/blocks');
  });

  it('holds the link back while the statistic is loading', () => {
    const { container } = render(
      <HighlightsItem id="total_blocks" label="Latest block" value="30,146,364" href={{ pathname: '/blocks' }} isLoading/>,
    );

    expect(container.querySelector('[data-highlight="total_blocks"] a')).toBeNull();
  });
});
