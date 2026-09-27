// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import ActionBar from './ActionBar';

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

describe('ActionBar', () => {
  it('keeps the children it is given in the order they arrive', () => {
    const { container } = render(
      <Provider>
        <ActionBar>
          <span>Download Page Data</span>
          <span>Page 1 of 9</span>
        </ActionBar>
      </Provider>,
    );

    const bar = container.querySelector('[data-action-bar]');

    expect(bar).not.toBeNull();
    expect(Array.from(bar?.children ?? []).map((child) => child.textContent)).toEqual([ 'Download Page Data', 'Page 1 of 9' ]);
  });

  it('renders nothing when every child is empty, so a page keeps no blank strip', () => {
    const { container } = render(
      <Provider>
        <ActionBar>
          { null }
          { false }
        </ActionBar>
      </Provider>,
    );

    expect(container.querySelector('[data-action-bar]')).toBeNull();
  });
});
