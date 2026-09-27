// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import ScanMethodChip from './ScanMethodChip';

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

describe('ScanMethodChip', () => {
  it('carries the method name as its text and as its hook', () => {
    const { container } = render(
      <Provider>
        <ScanMethodChip method="transfer"/>
      </Provider>,
    );

    const chip = container.querySelector('[data-scan-method="transfer"]');

    expect(chip).not.toBeNull();
    expect(chip?.textContent).toBe('transfer');
  });

  it('renders a bare selector the same way', () => {
    const { container } = render(
      <Provider>
        <ScanMethodChip method="0xa9059cbb"/>
      </Provider>,
    );

    expect(container.querySelector('[data-scan-method="0xa9059cbb"]')?.textContent).toBe('0xa9059cbb');
  });
});
