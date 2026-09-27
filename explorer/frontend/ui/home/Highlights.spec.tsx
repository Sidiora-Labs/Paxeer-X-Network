// @vitest-environment jsdom

import React from 'react';

import { render } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import Highlights from './Highlights';

describe('Highlights', () => {
  it('stacks the items in the order they are given', () => {
    const { container } = render(
      <Highlights
        items={ [
          { id: 'coin_price', label: 'ETH price', value: '$1.00' },
          { id: 'market_cap', label: 'ETH market cap', value: '$2.00' },
        ] }
      />,
    );

    expect(Array.from(container.querySelectorAll('[data-highlight]')).map((item) => item.getAttribute('data-highlight')))
      .toEqual([ 'coin_price', 'market_cap' ]);
  });

  it('divides every item but the first from the one above it', () => {
    const { container } = render(
      <Highlights
        items={ [
          { id: 'coin_price', label: 'ETH price', value: '$1.00' },
          { id: 'market_cap', label: 'ETH market cap', value: '$2.00' },
        ] }
      />,
    );

    const column = container.querySelector('[data-label="home-highlights"]') as HTMLElement;

    expect(Array.from(column.children).map((item) => item.hasAttribute('data-divided'))).toEqual([ false, true ]);
  });

  it('renders nothing without items', () => {
    const { container } = render(<Highlights/>);

    expect(container.querySelector('[data-label="home-highlights"]')).toBeNull();
  });
});
