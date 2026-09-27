// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import ScanDirectionBadge from './ScanDirectionBadge';

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

describe('ScanDirectionBadge', () => {
  it('reads IN for an incoming transfer', () => {
    const { container } = render(
      <Provider>
        <ScanDirectionBadge direction="in"/>
      </Provider>,
    );

    const badge = container.querySelector('[data-direction="in"]');

    expect(badge).not.toBeNull();
    expect(badge?.textContent).toBe('IN');
  });

  it('reads OUT for an outgoing transfer', () => {
    const { container } = render(
      <Provider>
        <ScanDirectionBadge direction="out"/>
      </Provider>,
    );

    const badge = container.querySelector('[data-direction="out"]');

    expect(badge).not.toBeNull();
    expect(badge?.textContent).toBe('OUT');
  });
});
