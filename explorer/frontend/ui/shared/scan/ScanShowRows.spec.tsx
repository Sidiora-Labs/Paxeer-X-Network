// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import ScanShowRows, { SCAN_ROWS_PER_PAGE } from './ScanShowRows';

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

const noop = vi.fn();

describe('ScanShowRows', () => {
  it('offers the three page sizes the tables share', () => {
    expect(SCAN_ROWS_PER_PAGE).toEqual([ 25, 50, 100 ]);
  });

  it('labels the select and shows the size in force', () => {
    const { container } = render(
      <Provider>
        <ScanShowRows value={ 50 } onValueChange={ noop }/>
      </Provider>,
    );

    const row = container.querySelector('[data-scan-show-rows]');

    expect(row).not.toBeNull();
    expect(row?.querySelector('[data-label]')?.textContent).toBe('Show rows:');
    expect(row?.textContent).toContain('50');
  });

  it('takes a caller label and a trailing note', () => {
    const { container } = render(
      <Provider>
        <ScanShowRows value={ 25 } onValueChange={ noop } label="Rows per page:" suffix="of 1,000"/>
      </Provider>,
    );

    expect(container.querySelector('[data-label]')?.textContent).toBe('Rows per page:');
    expect(container.querySelector('[data-suffix]')?.textContent).toBe('of 1,000');
  });

  it('takes the page sizes a caller prefers', () => {
    const { container } = render(
      <Provider>
        <ScanShowRows value={ 10 } onValueChange={ noop } options={ [ 10, 20 ] }/>
      </Provider>,
    );

    expect(container.querySelector('[data-scan-show-rows]')?.textContent).toContain('10');
  });
});
