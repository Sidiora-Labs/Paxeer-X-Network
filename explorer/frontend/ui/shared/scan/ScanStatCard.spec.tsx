// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import ScanStatCard from './ScanStatCard';

// jsdom implements no CSS media queries, and the Chakra provider reads window.matchMedia on mount
const createMediaQueryList = (query: string): MediaQueryList => ({
  matches: false,
  media: query,
  onchange: null,
  addListener: () => undefined,
  removeListener: () => undefined,
  addEventListener: () => undefined,
  removeEventListener: () => undefined,
  dispatchEvent: () => false,
});

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', { writable: true, value: createMediaQueryList });
});

// the suite runs without vitest globals, so the testing library cannot register its own teardown
afterEach(cleanup);

describe('ScanStatCard', () => {
  it('puts the label above the value', () => {
    const { container } = render(
      <Provider>
        <ScanStatCard label="Transactions" value="1,234,567"/>
      </Provider>,
    );

    const card = container.querySelector('[data-scan-stat="Transactions"]');

    expect(card).not.toBeNull();
    expect(card?.querySelector('[data-label]')?.textContent).toBe('Transactions');
    expect(card?.querySelector('[data-value]')?.textContent).toBe('1,234,567');
  });

  it('shows the secondary figure and the delta in parentheses after the value', () => {
    const { container } = render(
      <Provider>
        <ScanStatCard
          label="Transactions"
          value="1,234,567"
          secondary="12.5 TPS"
          delta={{ value: '3.2%', direction: 'up' }}
        />
      </Provider>,
    );

    expect(container.querySelector('[data-secondary]')?.textContent).toBe('(12.5 TPS)');
    expect(container.querySelector('[data-delta="up"]')?.textContent).toBe('(3.2%)');
  });

  it('marks a falling delta apart from a rising one', () => {
    const { container } = render(
      <Provider>
        <ScanStatCard label="Median gas price" value="0.1 PAX" delta={{ value: '1.4%', direction: 'down' }}/>
      </Provider>,
    );

    expect(container.querySelector('[data-delta="down"]')).not.toBeNull();
    expect(container.querySelector('[data-delta="up"]')).toBeNull();
  });

  it('leaves out the secondary text and the delta when neither is given', () => {
    const { container } = render(
      <Provider>
        <ScanStatCard label="Total blocks" value="42"/>
      </Provider>,
    );

    expect(container.querySelector('[data-secondary]')).toBeNull();
    expect(container.querySelector('[data-delta]')).toBeNull();
  });
});
